// Package bootstrap builds the dependency container shared across the app.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NhProGamer/orion-drive/conf"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/auth"
	"github.com/NhProGamer/orion-drive/pkg/board"
	"github.com/NhProGamer/orion-drive/pkg/cache"
	"github.com/NhProGamer/orion-drive/pkg/filemanager"
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/local"  // register the local storage backend
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/remote" // register the remote (slave) storage backend
	_ "github.com/NhProGamer/orion-drive/pkg/filemanager/driver/s3"     // register the S3 storage backend
	"github.com/NhProGamer/orion-drive/pkg/filemanager/encrypt"
	"github.com/NhProGamer/orion-drive/pkg/livedoc"
	"github.com/NhProGamer/orion-drive/pkg/queue"
	"github.com/NhProGamer/orion-drive/pkg/thumb"
	"github.com/NhProGamer/orion-drive/pkg/wopi"
	"github.com/NhProGamer/orion-drive/repository"
	"github.com/NhProGamer/orion-drive/service/share"
	"gorm.io/gorm"
)

// Dependency is the application's injected dependency container.
type Dependency struct {
	Config   *conf.Config
	Logger   *slog.Logger
	DB       *gorm.DB
	Cache    cache.Store
	Repo     *repository.Repository
	Files    *filemanager.Manager
	Shares   *share.Service
	Tasks    *queue.Queue
	Auth     *auth.Authenticator
	Signer   *auth.Signer
	WOPI     *wopi.Token
	WOPIDisc *wopi.Discovery
	Boards   *board.Hub
	Docs     *livedoc.Hub
}

// Init opens the database and assembles the dependency container. It does not
// run migrations (see Migrate) so the `migrate` command can own that step.
func Init(cfg *conf.Config) (*Dependency, error) {
	logger := newLogger(cfg)

	// Fail closed on a weak session secret (it keys session and WOPI tokens).
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.SessionSecretWeak() {
		logger.Warn("running with a weak/default SessionSecret; acceptable only in debug mode — never expose this instance")
	}

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
	thumb.Configure(thumb.Options{
		Disable:         cfg.Thumbnail.Disable,
		MaxDim:          cfg.Thumbnail.MaxDim,
		Quality:         cfg.Thumbnail.Quality,
		DisableVideo:    cfg.Thumbnail.DisableVideo,
		DisableAudio:    cfg.Thumbnail.DisableAudio,
		DisableVips:     cfg.Thumbnail.DisableVips,
		DisableRaw:      cfg.Thumbnail.DisableRaw,
		DisablePDF:      cfg.Thumbnail.DisablePDF,
		DisableDocument: cfg.Thumbnail.DisableDocument,
		DisableEbook:    cfg.Thumbnail.DisableEbook,
		FFmpegPath:      cfg.Thumbnail.FFmpegPath,
		VipsPath:        cfg.Thumbnail.VipsPath,
		PopplerPath:     cfg.Thumbnail.PopplerPath,
		LibreOfficePath: cfg.Thumbnail.LibreOfficePath,
		LibRawPath:      cfg.Thumbnail.LibRawPath,
		// Reuse the configured WOPI document server (Collabora) to render
		// document thumbnails when LibreOffice is not installed locally.
		DocServerURL: cfg.WOPI.ServerURL,
	})

	tasks := queue.New(4)
	files := filemanager.NewManager(repo, c, tmpDir, cipher, tasks)
	files.SetDedup(cfg.Storage.Dedup)
	files.SetMaxVersions(cfg.Storage.MaxVersions)
	files.SetArchiveLimits(filemanager.ArchiveLimits{
		MaxEntries:      cfg.Archive.MaxEntries,
		MaxUncompressed: cfg.Archive.MaxSizeMB << 20,
		MaxRatio:        cfg.Archive.MaxRatio,
		RatioFloor:      cfg.Archive.RatioFloorMB << 20,
		Timeout:         time.Duration(cfg.Archive.TimeoutSeconds) * time.Second,
	})

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
		Config:   cfg,
		Logger:   logger,
		DB:       db,
		Cache:    c,
		Repo:     repo,
		Files:    files,
		Shares:   share.New(repo, files),
		Tasks:    tasks,
		Auth:     authn,
		Signer:   auth.NewSigner(cfg.System.SessionSecret),
		WOPI:     wopi.NewToken(cfg.WOPI.TokenSecret(cfg.System.SessionSecret)),
		WOPIDisc: wopi.NewDiscovery(cfg.WOPI.Discovery(), time.Hour),
	}

	if cfg.LiveDoc.Enabled() {
		dep.Docs = livedoc.NewHub(files.Snapshots(), livedoc.Options{
			MaxFrameBytes: cfg.LiveDoc.MaxFrameBytes(),
			MaxPeers:      cfg.LiveDoc.MaxPeers,
			SaveInterval:  cfg.LiveDoc.SaveInterval(),
		}, logger)
	}

	if cfg.Board.Enabled() {
		dep.Boards = board.NewHub(files.Snapshots(), board.Options{
			MaxSceneBytes: cfg.Board.MaxSceneBytes(),
			MaxElements:   cfg.Board.MaxElements,
			MaxPeers:      cfg.Board.MaxPeers,
			SaveInterval:  cfg.Board.SaveInterval(),
		}, logger)
	}

	// Render document thumbnails via the configured WOPI document server when
	// LibreOffice is not installed locally (Collabora upload, or OnlyOffice
	// fetch-by-URL). Only used by the KindDocument thumbnail path.
	if cfg.WOPI.ServerURL != "" {
		files.SetDocThumbnailer(docThumbnailer(cfg, dep.WOPI))
	}
	return dep, nil
}

// docThumbnailer builds the document-server thumbnail fallback. With no
// ConvertSecret it uploads the file to Collabora's convert-to API; with a
// secret it drives OnlyOffice's conversion API, which fetches the file itself
// from a short-lived tokenised WOPI URL (so System.SiteURL must be reachable
// from the document server).
func docThumbnailer(cfg *conf.Config, signer *wopi.Token) filemanager.DocThumbFunc {
	return func(ctx context.Context, f *model.File, path string) ([]byte, error) {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Name), "."))
		if cfg.WOPI.ConvertSecret == "" {
			return thumb.CollaboraConvert(ctx, cfg.WOPI.ServerURL, path)
		}
		site := strings.TrimRight(cfg.System.SiteURL, "/")
		if site == "" {
			return nil, fmt.Errorf("thumbnail: OnlyOffice conversion needs System.SiteURL")
		}
		tok, err := signer.Sign(f.ID, f.OwnerID, false, "",
			strconv.FormatUint(uint64(f.OwnerID), 10), "thumbnail", 5*time.Minute)
		if err != nil {
			return nil, err
		}
		fileURL := fmt.Sprintf("%s/wopi/files/%d/contents?access_token=%s", site, f.ID, url.QueryEscape(tok))
		var ver uint
		if f.PrimaryEntityID != nil {
			ver = *f.PrimaryEntityID
		}
		key := fmt.Sprintf("odthumb-%d-%d", f.ID, ver)
		return thumb.OnlyOfficeConvert(ctx, cfg.WOPI.ServerURL, cfg.WOPI.ConvertSecret, fileURL, ext, key)
	}
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
	l := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(l) // so package-level helpers (e.g. controllers.fail) log too
	return l
}
