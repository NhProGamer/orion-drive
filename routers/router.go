// Package routers builds the Gin engine: middleware, API routes and the SPA.
package routers

import (
	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/application/statics"
	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver/remote"
	"github.com/NhProGamer/orion-drive/routers/controllers"
	"github.com/gin-gonic/gin"
)

// New assembles the HTTP engine for the given dependencies.
func New(dep *bootstrap.Dependency) (*gin.Engine, error) {
	if dep.Config.System.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logging(dep.Logger))
	r.Use(middleware.CORS(dep.Config.System.SiteURL))
	r.Use(middleware.CurrentUser(dep.Signer, dep.Repo))

	ctl := controllers.New(dep)
	api := r.Group(constants.APIPrefix)
	registerAuthRoutes(api, ctl, dep)
	registerFileRoutes(api, ctl)
	registerShareRoutes(api, ctl)

	// When a slave secret is configured, this node also acts as a storage slave.
	if dep.Config.Slave.Secret != "" {
		if err := registerSlaveRoutes(r, dep); err != nil {
			return nil, err
		}
	}

	if err := statics.Register(r); err != nil {
		return nil, err
	}
	return r, nil
}

func registerAuthRoutes(api *gin.RouterGroup, ctl *controllers.Controller, dep *bootstrap.Dependency) {
	auth := api.Group("/auth")
	auth.GET("/config", ctl.AuthConfig)
	auth.GET("/oidc/login", ctl.OIDCLogin)
	auth.GET("/oidc/callback", ctl.OIDCCallback)
	auth.POST("/logout", ctl.Logout)

	// Development-only local login, available when running in debug mode.
	if dep.Config.System.Mode == "debug" {
		auth.GET("/dev-login", ctl.DevLogin)
	}

	api.GET("/user/me", ctl.Me)
}

func registerFileRoutes(api *gin.RouterGroup, ctl *controllers.Controller) {
	f := api.Group("")
	f.Use(middleware.RequireAuth())

	f.GET("/file", ctl.ListFiles)
	f.POST("/file/folder", ctl.CreateFolder)
	f.POST("/file/rename", ctl.Rename)
	f.POST("/file/star", ctl.Star)
	f.POST("/file/move", ctl.Move)
	f.POST("/file/trash", ctl.Trash)
	f.POST("/file/restore", ctl.Restore)
	f.POST("/file/purge", ctl.Purge)
	f.GET("/file/content/:id", ctl.Download)

	f.GET("/file/versions/:id", ctl.ListVersions)
	f.POST("/file/version/restore", ctl.RestoreVersion)
	f.POST("/file/version/delete", ctl.DeleteVersion)

	f.POST("/file/lock", ctl.Lock)
	f.POST("/file/unlock", ctl.Unlock)

	f.POST("/upload", ctl.InitUpload)
	f.POST("/upload/:sid/chunk", ctl.PutChunk)
	f.POST("/upload/:sid/complete", ctl.CompleteUpload)
	f.DELETE("/upload/:sid", ctl.CancelUpload)

	f.GET("/user/capacity", ctl.Capacity)
}

// registerSlaveRoutes mounts the signed slave storage API backed by a local
// directory. Every route is gated by a shared-secret signature.
func registerSlaveRoutes(r *gin.Engine, dep *bootstrap.Dependency) error {
	store, err := driver.New(&model.StoragePolicy{
		Type:     model.PolicyTypeLocal,
		BasePath: dep.Config.Slave.StoragePath,
	})
	if err != nil {
		return err
	}
	slave := controllers.NewSlave(store)

	g := r.Group(remote.APIPrefix)
	g.Use(middleware.SlaveAuth(dep.Config.Slave.Secret))
	g.POST("/upload", slave.Upload)
	g.GET("/content", slave.Content)
	g.GET("/download", slave.Download)
	g.POST("/delete", slave.Delete)

	dep.Logger.Info("slave storage enabled", "path", dep.Config.Slave.StoragePath)
	return nil
}

func registerShareRoutes(api *gin.RouterGroup, ctl *controllers.Controller) {
	// Authenticated: manage your own shares.
	api.POST("/share", middleware.RequireAuth(), ctl.CreateShare)
	api.GET("/share", middleware.RequireAuth(), ctl.ListShares)
	api.DELETE("/share/:token", middleware.RequireAuth(), ctl.DeleteShare)

	// Public: view and download a shared file (no authentication).
	api.GET("/share/:token", ctl.ShareView)
	api.GET("/share/:token/content", ctl.ShareDownload)
}
