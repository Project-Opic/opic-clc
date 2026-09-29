// Package api —— opicdemo 路由（gin；镜像 v1 路由面，ZITADEL SSO 端点保留——auth 包随服务移植）。
package api

import (
	"github.com/gin-gonic/gin"

	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/middleware"
)

// InitApi 挂载路由。
func InitApi(h *gin.Engine) {
	api := h.Group("/api")

	pub := api.Group("")
	{
		pub.POST("/auth/login", Login)
		pub.GET("/auth/mode", AuthMode)
		pub.GET("/auth/login-url", ZitadelLoginURL)
		pub.GET("/auth/callback", ZitadelCallback)
	}

	authed := api.Group("", middleware.LoginRequired())
	{
		authed.POST("/auth/logout", Logout)
		authed.GET("/auth/info", Info)

		authed.GET("/meta/dictionaries", MetaDictionaries)
		authed.GET("/meta/customer-status", MetaCustomerStatus)
		authed.GET("/meta/rules", MetaRules)

		authed.GET("/customer", CustomerList)
		authed.GET("/customer/:id", CustomerGet)
		authed.POST("/customer/draft", CustomerCreateDraft)
		authed.PUT("/customer/:id/draft", CustomerUpdateDraft)
		authed.POST("/customer/:id/submit", CustomerSubmit)
		authed.POST("/customer/:id/withdraw", CustomerWithdraw)

		authed.GET("/workbench/todo", WorkbenchTodo)
		authed.GET("/workbench/done", WorkbenchDone)
		authed.GET("/workbench/requested", WorkbenchRequested)
		authed.POST("/workbench/todo/:id/approve", WorkbenchApprove)
		authed.POST("/workbench/todo/:id/reject", WorkbenchReject)
		authed.POST("/workbench/todo/:id/return", WorkbenchReturn)

		authed.GET("/flow/definitions", FlowDefinitionList)
		authed.GET("/flow/definitions/:id", FlowDefinitionGet)
		authed.GET("/flow/definitions/:id/graph", FlowDefinitionGraph)
		authed.GET("/flow/instances", FlowInstanceList)
		authed.GET("/flow/instances/:id", FlowInstanceGet)
		authed.GET("/flow/tasks", FlowTaskList)

		authed.GET("/users", UserList)
		authed.GET("/users/options", UserOptions)
		authed.GET("/roles", RoleList)
		authed.GET("/permissions", PermissionList)
		authed.GET("/permissions/all", PermissionAll)
		authed.GET("/resources", ResourceList)
		authed.GET("/resources/tree", ResourceTree)

		perm := func(code string, handler gin.HandlerFunc) []gin.HandlerFunc {
			return []gin.HandlerFunc{middleware.RequirePermission(code), handler}
		}

		authed.POST("/users", perm("system:user:add", UserCreate)...)
		authed.PUT("/users/:id", perm("system:user:edit", UserUpdate)...)
		authed.DELETE("/users/:id", perm("system:user:delete", UserDelete)...)
		authed.PUT("/users/:id/roles", perm("system:user:assign-role", UserAssignRoles)...)
		authed.PUT("/users/:id/reset-pwd", perm("system:user:reset-pwd", UserResetPwd)...)
		authed.POST("/roles", perm("system:role:add", RoleCreate)...)
		authed.PUT("/roles/:id", perm("system:role:edit", RoleUpdate)...)
		authed.DELETE("/roles/:id", perm("system:role:delete", RoleDelete)...)
		authed.PUT("/roles/:id/permissions", perm("system:role:assign", RoleAssignPermissions)...)
		authed.PUT("/roles/:id/resources", perm("system:role:assign", RoleAssignResources)...)
		authed.POST("/permissions", perm("system:permission:add", PermissionCreate)...)
		authed.PUT("/permissions/:id", perm("system:permission:edit", PermissionUpdate)...)
		authed.DELETE("/permissions/:id", perm("system:permission:delete", PermissionDelete)...)
		authed.POST("/resources", perm("system:resource:add", ResourceCreate)...)
		authed.PUT("/resources/:id", perm("system:resource:edit", ResourceUpdate)...)
		authed.DELETE("/resources/:id", perm("system:resource:delete", ResourceDelete)...)
		authed.POST("/flow/definitions", perm("flow:definition:add", FlowDefinitionCreate)...)
		authed.PUT("/flow/definitions/:id", perm("flow:definition:edit", FlowDefinitionUpdate)...)
		authed.POST("/flow/definitions/:id/publish", perm("flow:definition:publish", FlowDefinitionPublish)...)
		authed.PUT("/flow/instances/:id/terminate", perm("flow:instance:terminate", FlowInstanceTerminate)...)
		authed.PUT("/flow/tasks/:id/transfer", perm("flow:task:transfer", FlowTaskTransfer)...)
		authed.PUT("/flow/tasks/:id/urge", perm("flow:task:urge", FlowTaskUrge)...)
	}
}
