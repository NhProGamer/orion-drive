// Package routers builds the Gin engine: middleware, API routes and the SPA.
package routers

import (
	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/application/statics"
	"github.com/NhProGamer/orion-drive/middleware"
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

	f.POST("/upload", ctl.InitUpload)
	f.POST("/upload/:sid/chunk", ctl.PutChunk)
	f.POST("/upload/:sid/complete", ctl.CompleteUpload)
	f.DELETE("/upload/:sid", ctl.CancelUpload)

	f.GET("/user/capacity", ctl.Capacity)
}
