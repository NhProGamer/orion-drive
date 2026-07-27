package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/pkg/crontab"
	"github.com/NhProGamer/orion-drive/pkg/sftpserver"
	"github.com/NhProGamer/orion-drive/routers"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run the OrionDrive HTTP server",
	RunE: func(*cobra.Command, []string) error {
		dep, err := bootstrap.Init(cfg)
		if err != nil {
			return err
		}
		if err := bootstrap.Migrate(dep.DB, cfg); err != nil {
			return err
		}

		engine, err := routers.New(dep)
		if err != nil {
			return err
		}

		srv := &http.Server{Addr: cfg.System.Listen, Handler: engine}

		// Background maintenance: purge expired trash and clean stale upload temp.
		sched := crontab.New(dep.Logger)
		retention := time.Duration(cfg.System.TrashRetentionDays) * 24 * time.Hour
		sched.Add("trash-purge", 6*time.Hour, func(ctx context.Context) (string, error) {
			n, err := dep.Files.PurgeExpiredTrash(ctx, retention)
			return fmt.Sprintf("%d file(s) purged", n), err
		})
		sched.Add("upload-cleanup", 6*time.Hour, func(context.Context) (string, error) {
			n, err := dep.Files.CleanupUploadTemp()
			return fmt.Sprintf("%d stale upload file(s) removed", n), err
		})
		schedCtx, schedCancel := context.WithCancel(context.Background())
		sched.Start(schedCtx)

		go func() {
			dep.Logger.Info("OrionDrive listening", "addr", cfg.System.Listen, "oidc", dep.Auth.Enabled())
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				dep.Logger.Error("server error", "error", err)
				os.Exit(1)
			}
		}()

		// Optional SFTP front-end (same dedicated credentials as WebDAV).
		var sftpSrv *sftpserver.Server
		if cfg.SFTP.Enable {
			ss, err := sftpserver.New(dep.Files, dep.Repo, dep.Logger, cfg.SFTP.Listen, cfg.SFTP.HostKeyPath)
			if err != nil {
				dep.Logger.Error("sftp init failed", "error", err)
			} else {
				sftpSrv = ss
				go func() {
					dep.Logger.Info("SFTP listening", "addr", cfg.SFTP.Listen)
					if err := ss.ListenAndServe(); err != nil {
						dep.Logger.Error("sftp server error", "error", err)
					}
				}()
			}
		}

		// Graceful shutdown on SIGINT/SIGTERM.
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop

		schedCancel()
		if sftpSrv != nil {
			_ = sftpSrv.Close()
		}
		dep.Tasks.Close() // cancel in-flight background jobs and stop the workers
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dep.Logger.Info("shutting down")
		return srv.Shutdown(ctx)
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
