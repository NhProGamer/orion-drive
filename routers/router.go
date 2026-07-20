// Package routers builds the Gin engine: middleware, API routes and the SPA.
package routers

import (
	"net/http"
	"strings"

	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/application/constants"
	"github.com/NhProGamer/orion-drive/application/statics"
	"github.com/NhProGamer/orion-drive/middleware"
	"github.com/NhProGamer/orion-drive/model"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver"
	"github.com/NhProGamer/orion-drive/pkg/filemanager/driver/remote"
	"github.com/NhProGamer/orion-drive/pkg/webdav"
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

	// WebDAV is served under /dav for clients authenticating with dedicated
	// credentials. It is intercepted before CORS (whose preflight handler would
	// otherwise answer OPTIONS itself, hiding the DAV capability header) and
	// before Gin's method router, so it can handle the non-standard WebDAV
	// methods — PROPFIND, MKCOL, MOVE, ... — that Gin would not route.
	if dep.Config.WebDAV.Enable {
		davHandler := webdav.Handler(dep.Files, dep.Repo)
		r.Use(func(c *gin.Context) {
			p := c.Request.URL.Path
			if p == webdav.Prefix || strings.HasPrefix(p, webdav.Prefix+"/") {
				// Reset the status Gin pre-set for the unmatched (NoRoute) path to
				// 200; the WebDAV handler overrides it when it writes its own
				// status (207, 201, ...), and OPTIONS — which writes none — stays 200.
				c.Status(http.StatusOK)
				davHandler.ServeHTTP(c.Writer, c.Request)
				c.Abort()
				return
			}
			c.Next()
		})
	}

	r.Use(middleware.CORS(dep.Config.System.SiteURL))
	r.Use(middleware.CurrentUser(dep.Signer, dep.Repo))

	ctl := controllers.New(dep)
	api := r.Group(constants.APIPrefix)
	registerAuthRoutes(api, ctl, dep)
	registerFileRoutes(api, ctl)
	registerShareRoutes(api, ctl, dep)
	registerAdminRoutes(api, ctl, dep)

	// Public direct-link content (no authentication).
	api.GET("/link/:token", ctl.DirectLinkContent)

	// WOPI host endpoints (called by the Office editor; authorised by token).
	wopi := r.Group("/wopi/files")
	wopi.GET("/:id", ctl.WopiCheckFileInfo)
	wopi.GET("/:id/contents", ctl.WopiGetFile)
	wopi.POST("/:id/contents", ctl.WopiPutFile)

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

	// Public: which formats online Office can edit/view (for the SPA and the
	// public share page). Empty when Office editing is not configured.
	api.GET("/office/formats", ctl.OfficeFormats)
}

func registerFileRoutes(api *gin.RouterGroup, ctl *controllers.Controller) {
	f := api.Group("")
	f.Use(middleware.RequireAuth())

	f.GET("/file", ctl.ListFiles)
	f.POST("/file/folder", ctl.CreateFolder)
	f.POST("/file/folder-path", ctl.EnsureFolderPath)
	f.POST("/file/rename", ctl.Rename)
	f.POST("/file/star", ctl.Star)
	f.POST("/file/move", ctl.Move)
	f.POST("/file/trash", ctl.Trash)
	f.POST("/file/restore", ctl.Restore)
	f.POST("/file/purge", ctl.Purge)
	f.POST("/file/trash/empty", ctl.EmptyTrash)
	f.GET("/file/content/:id", ctl.Download)
	f.GET("/file/thumb/:id", ctl.Thumbnail)
	f.PUT("/file/text", ctl.SaveText)
	f.PUT("/file/blob/:id", ctl.SaveBlob)
	f.GET("/file/office/:id", ctl.OfficeLaunch)
	f.POST("/file/office/new", ctl.OfficeNew)

	f.GET("/file/versions/:id", ctl.ListVersions)
	f.POST("/file/version/restore", ctl.RestoreVersion)
	f.POST("/file/version/delete", ctl.DeleteVersion)

	f.POST("/file/lock", ctl.Lock)
	f.POST("/file/unlock", ctl.Unlock)

	f.POST("/file/direct-link", ctl.CreateDirectLink)
	f.GET("/file/direct-links/:id", ctl.ListDirectLinks)
	f.DELETE("/file/direct-link/:token", ctl.DeleteDirectLink)

	f.GET("/file/archive", ctl.ArchiveDownload)
	f.GET("/file/archive/entries/:id", ctl.ArchiveEntries)
	f.POST("/file/archive/compress", ctl.CompressArchive)
	f.POST("/file/archive/extract", ctl.ExtractArchive)

	f.GET("/task", ctl.TaskList)
	f.GET("/task/:id", ctl.TaskStatus)

	f.POST("/upload", ctl.InitUpload)
	f.POST("/upload/:sid/chunk", ctl.PutChunk)
	f.POST("/upload/:sid/complete", ctl.CompleteUpload)
	f.DELETE("/upload/:sid", ctl.CancelUpload)

	f.GET("/user/capacity", ctl.Capacity)

	f.GET("/webdav/accounts", ctl.WebdavList)
	f.POST("/webdav/accounts", ctl.WebdavCreate)
	f.DELETE("/webdav/accounts/:id", ctl.WebdavDelete)
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

// registerAdminRoutes mounts the admin panel API, gated by admin access.
func registerAdminRoutes(api *gin.RouterGroup, ctl *controllers.Controller, dep *bootstrap.Dependency) {
	a := api.Group("/admin")
	a.Use(middleware.RequireAuth(), middleware.RequireAdmin(dep.Config.System.AdminEmailSet(), dep.Config.System.AdminGroupSet()))

	a.GET("/stats", ctl.AdminStats)
	a.POST("/maintenance", ctl.AdminRunMaintenance)

	a.GET("/users", ctl.AdminListUsers)
	a.PATCH("/users/:id", ctl.AdminUpdateUser)

	a.GET("/groups", ctl.AdminListGroups)
	a.POST("/groups", ctl.AdminCreateGroup)
	a.PATCH("/groups/:id", ctl.AdminUpdateGroup)
	a.DELETE("/groups/:id", ctl.AdminDeleteGroup)

	a.GET("/policies", ctl.AdminListPolicies)
	a.POST("/policies", ctl.AdminCreatePolicy)
	a.DELETE("/policies/:id", ctl.AdminDeletePolicy)
}

func registerShareRoutes(api *gin.RouterGroup, ctl *controllers.Controller, dep *bootstrap.Dependency) {
	// Authenticated: manage your own shares.
	api.POST("/share", middleware.RequireAuth(), ctl.CreateShare)
	api.GET("/share", middleware.RequireAuth(), ctl.ListShares)
	api.PATCH("/share/:token", middleware.RequireAuth(), ctl.UpdateShare)
	api.DELETE("/share/:token", middleware.RequireAuth(), ctl.DeleteShare)

	// Public share endpoints (no authentication) are rate-limited per client IP to
	// blunt password brute-forcing and anonymous-write abuse. The resumable-upload
	// chunk PUT is excluded — a large file legitimately sends many chunks quickly,
	// and it is already bounded by the (secret) session id and the quota.
	rl := middleware.RateLimit(dep.Cache, dep.Config.Security.SharePublicRate(), "share")

	// Public: view, browse and download a shared file or folder.
	api.GET("/share/:token", rl, ctl.ShareView)
	api.GET("/share/:token/list", rl, ctl.ShareList)
	api.GET("/share/:token/content", rl, ctl.ShareDownload)
	api.GET("/share/:token/archive", rl, ctl.ShareArchive)

	// Public: write into a write/deposit share. Each handler also enforces the
	// share's permission, password/expiry and subtree confinement.
	api.POST("/share/:token/folder", rl, ctl.ShareCreateFolder)
	api.POST("/share/:token/upload", rl, ctl.ShareInitUpload)
	api.POST("/share/:token/upload/:sid/chunk", ctl.SharePutChunk)
	api.POST("/share/:token/upload/:sid/complete", rl, ctl.ShareCompleteUpload)
	api.DELETE("/share/:token/upload/:sid", rl, ctl.ShareCancelUpload)
	api.POST("/share/:token/rename", rl, ctl.ShareRename)
	api.POST("/share/:token/move", rl, ctl.ShareMove)
	api.POST("/share/:token/delete", rl, ctl.ShareDelete)
	api.GET("/share/:token/office", rl, ctl.OfficeLaunchShare)
}
