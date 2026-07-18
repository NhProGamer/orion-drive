// Package bootstrap builds the dependency container shared across the app.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/pkg/auth"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/local"  // register the local storage backend
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/remote" // register the remote (slave) storage backend
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/s3"     // register the S3 storage backend
	"github.com/NhProGamer/orion-drive/pkg/filemanager/encrypt"
	"github.com/NhProGamer/orion-drive/pkg/queue"
	"github.com/NhProGamer/orion-drive/pkg/wopi"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/NhProGamer/orion-drive/service/share"
	"gorm.io/gorm"
)

// Dependency is the application's injected dependency container.
type Dependency struct {
	Config *conf.Config
	Logger *slog.Logger
	DB     *gorm.DB
	Cache  cache.Store
	Repo   *repository.Repository
	Files  *filemanager.Manager
	Shares *share.Service
	Tasks  *queue.Queue
	Auth   *auth.Authenticator
	Signer   *auth.Signer
	WOPI     *wopi.Token
	WOPIDisc *wopi.Discovery
}

// Init opens the database and assembles the dependency container. It does not
// run migrations (see Migrate) so the `migrate` command can own that step.
func Init(cfg *conf.Config) (*Dependency, error) {
	logger := newLogger(cfg)

	db, err := OpenDatabase(cfg)
	if err != nil {
		return nil, err
	}

	c, err := openCache(cfg, logger)
	if err != nil {
		return nil, err
	}
	repo := repository.New(db)
	tmpDir := filepath.Join(filepath.Dir(cfg.Database.DBFile), "tmp", "uploads")
	var cipher *encrypt.Cipher
	if cfg.Storage.EncryptionKey != "" {
		cipher, err = encrypt.NewCipherHex(cfg.Storage.EncryptionKey)
		if err != nil {
			return nil, err
		}
	}
	tasks := queue.New(4)
	files := filemanager.NewManager(repo, c, tmpDir, cipher, tasks)

	// OIDC discovery is best-effort: if the provider is unreachable or
	// unconfigured, the server still boots (login just stays unavailable).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authn, err := auth.NewAuthenticator(ctx, cfg, c)
	if err != nil {
		logger.Warn("OIDC provider discovery failed; login disabled", "error", err)
		authn = &auth.Authenticator{}
	}

	dep := &Dependency{
		Config: cfg,
		Logger: logger,
		DB:     db,
		Cache:  c,
		Repo:   repo,
		Files:  files,
		Shares: share.New(repo, files),
		Tasks:  tasks,
		Auth:   authn,
		Signer:   auth.NewSigner(cfg.System.SessionSecret),
		WOPI:     wopi.NewToken(cfg.System.SessionSecret),
		WOPIDisc: wopi.NewDiscovery(cfg.WOPI.Discovery(), time.Hour),
	}
	return dep, nil
}

// openCache returns the Redis-backed cache when a server is configured,
// otherwise the in-process memory cache.
func openCache(cfg *conf.Config, logger *slog.Logger) (cache.Store, error) {
	if cfg.Redis.Server == "" {
		return cache.NewMemory(), nil
	}
	c, err := cache.NewRedis(cfg.Redis.Server, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		return nil, fmt.Errorf("connect to redis at %s: %w", cfg.Redis.Server, err)
	}
	logger.Info("using redis cache", "server", cfg.Redis.Server, "db", cfg.Redis.DB)
	return c, nil
}

func newLogger(cfg *conf.Config) *slog.Logger {
	level := slog.LevelInfo
	if cfg.System.Mode == "debug" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
