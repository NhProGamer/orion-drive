//go:build !dev

package routers

import (
	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/routers/controllers"
	"github.com/gin-gonic/gin"
)

// registerDevRoutes is a no-op in production builds: the dev-login shortcut is
// only compiled in with `-tags dev`.
func registerDevRoutes(_ *gin.RouterGroup, _ *controllers.Controller, _ *bootstrap.Dependency) {}
