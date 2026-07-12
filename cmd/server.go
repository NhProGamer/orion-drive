package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
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

		go func() {
			dep.Logger.Info("OrionDrive listening", "addr", cfg.System.Listen, "oidc", dep.Auth.Enabled())
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				dep.Logger.Error("server error", "error", err)
				os.Exit(1)
			}
		}()

		// Graceful shutdown on SIGINT/SIGTERM.
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dep.Logger.Info("shutting down")
		return srv.Shutdown(ctx)
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
