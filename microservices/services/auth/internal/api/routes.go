// Package api wires the auth service HTTP surface. The /api/v1 layout matches
// the monolith exactly for every extracted route; /internal/verify is new and
// deliberately outside /api so it stays unreachable through the gateway's
// /api routing rules.
package api

import (
	"github.com/gin-gonic/gin"
	"github.com/sharptoolbox/opic-techbase/services/auth/internal/api/auth"
	"github.com/sharptoolbox/opic-techbase/services/auth/internal/api/common"
	sharedapi "github.com/sharptoolbox/opic-techbase/services/shared/pkg/sharedapi"
	"github.com/sharptoolbox/opic-techbase/services/auth/internal/api/verify"
	authDAO "github.com/sharptoolbox/opic-techbase/services/auth/internal/dao/auth"
	authdao "github.com/sharptoolbox/opic-techbase/services/shared/pkg/authdao"
	"github.com/sharptoolbox/opic-techbase/services/auth/internal/middleware"
	authsvc "github.com/sharptoolbox/opic-techbase/services/auth/internal/service/auth"
)

// SetupRoutes mounts the auth service API using legacy global fallbacks.
func SetupRoutes(router *gin.Engine) {
	SetupRoutesWithDeps(router, sharedapi.Dependencies{})
}

// SetupRoutesWithDeps mounts the API with injected infrastructure handles.
func SetupRoutesWithDeps(router *gin.Engine, deps sharedapi.Dependencies) {
	api := router.Group("/api/v1")

	common.RegisterPublicRoutesWithDeps(api, deps)

	public := api.Group("/")
	{
		auth.RegisterPublicRoutesWithDeps(public, deps)
	}

	protected := api.Group("/")
	protected.Use(middleware.AuthMiddleware())
	{
		auth.RegisterProtectedRoutesWithDeps(protected, deps)
	}

	router.GET("/internal/verify", newVerifyHandlerFromDeps(deps).Verify)
}

// newVerifyHandlerFromDeps assembles the forwardAuth handler from injected
// handles. Without a database handle the cookie branch reports "console login
// required", mirroring middleware.AuthMiddleware without dependencies.
func newVerifyHandlerFromDeps(deps sharedapi.Dependencies) *verify.Handler {
	if deps.DB == nil {
		return verify.NewHandler(nil, nil, nil)
	}
	sessions := authsvc.NewConsoleSessionServiceWithDB(deps.DB)
	return verify.NewHandler(&sessions, authDAO.NewUserDAO(deps.DB), authdao.NewPermissionDAO(deps.DB))
}
