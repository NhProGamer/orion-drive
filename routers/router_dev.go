//go:build dev

package routers

import (
	"github.com/NhProGamer/orion-drive/application/bootstrap"
	"github.com/NhProGamer/orion-drive/routers/controllers"
	"github.com/gin-gonic/gin"
)

// registerDevRoutes wires the unauthenticated dev-login shortcut. It is compiled
// in only with `-tags dev`, and still requires Mode=debug at runtime.
func registerDevRoutes(auth *gin.RouterGroup, ctl *controllers.Controller, dep *bootstrap.Dependency) {
	if dep.Config.System.Mode == "debug" {
		auth.GET("/dev-login", ctl.DevLogin)
	}
}
