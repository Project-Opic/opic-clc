// Package v1 —— 路由注册(hertz-admin router/v1 风格:公开组 + 登录组 + 权限中间件按路由挂载)。
package v1

import (
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"

	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/apiserver/controller"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/pkg/middleware"
)

// InitApi 挂载 /api 路由。
func InitApi(h *route.Engine) {
	api := h.Group("/api")

	// ---- 公开路由(认证入口) ----
	pub := api.Group("")
	{
		pub.POST("/auth/login", controller.Login)
		pub.GET("/auth/mode", controller.AuthMode)
		pub.GET("/auth/login-url", controller.ZitadelLoginURL)
		pub.GET("/auth/callback", controller.ZitadelCallback)
	}

	// ---- 登录路由组 ----
	authed := api.Group("", middleware.LoginRequired())
	{
		// 认证
		authed.POST("/auth/logout", controller.Logout)
		authed.GET("/auth/info", controller.Info)

		// 元数据(登录即可)
		authed.GET("/meta/dictionaries", controller.MetaDictionaries)
		authed.GET("/meta/customer-status", controller.MetaCustomerStatus)
		authed.GET("/meta/rules", controller.MetaRules)

		// 客户申请(登录即可;业务校验在服务层)
		authed.GET("/customer", controller.CustomerList)
		authed.GET("/customer/:id", controller.CustomerGet)
		authed.POST("/customer/draft", controller.CustomerCreateDraft)
		authed.PUT("/customer/:id/draft", controller.CustomerUpdateDraft)
		authed.POST("/customer/:id/submit", controller.CustomerSubmit)
		authed.POST("/customer/:id/withdraw", controller.CustomerWithdraw)

		// 审批中心(登录即可;归属校验在服务层)
		authed.GET("/workbench/todo", controller.WorkbenchTodo)
		authed.GET("/workbench/done", controller.WorkbenchDone)
		authed.GET("/workbench/requested", controller.WorkbenchRequested)
		authed.POST("/workbench/todo/:id/approve", controller.WorkbenchApprove)
		authed.POST("/workbench/todo/:id/reject", controller.WorkbenchReject)
		authed.POST("/workbench/todo/:id/return", controller.WorkbenchReturn)

		// 流程查询(登录即可)
		authed.GET("/flow/definitions", controller.FlowDefinitionList)
		authed.GET("/flow/definitions/:id", controller.FlowDefinitionGet)
		authed.GET("/flow/definitions/:id/graph", controller.FlowDefinitionGraph)
		authed.GET("/flow/instances", controller.FlowInstanceList)
		authed.GET("/flow/instances/:id", controller.FlowInstanceGet)
		authed.GET("/flow/tasks", controller.FlowTaskList)

		// 系统查询(登录即可)
		authed.GET("/users", controller.UserList)
		authed.GET("/users/options", controller.UserOptions)
		authed.GET("/roles", controller.RoleList)
		authed.GET("/permissions", controller.PermissionList)
		authed.GET("/permissions/all", controller.PermissionAll)
		authed.GET("/resources", controller.ResourceList)
		authed.GET("/resources/tree", controller.ResourceTree)

		// ---- 写操作:按权限码挂载中间件 ----
		perm := func(code string, handler app.HandlerFunc) []app.HandlerFunc {
			return []app.HandlerFunc{middleware.RequirePermission(code), handler}
		}

		// 用户管理
		authed.POST("/users", perm("system:user:add", controller.UserCreate)...)
		authed.PUT("/users/:id", perm("system:user:edit", controller.UserUpdate)...)
		authed.DELETE("/users/:id", perm("system:user:delete", controller.UserDelete)...)
		authed.PUT("/users/:id/roles", perm("system:user:assign-role", controller.UserAssignRoles)...)
		authed.PUT("/users/:id/reset-pwd", perm("system:user:reset-pwd", controller.UserResetPwd)...)

		// 角色管理
		authed.POST("/roles", perm("system:role:add", controller.RoleCreate)...)
		authed.PUT("/roles/:id", perm("system:role:edit", controller.RoleUpdate)...)
		authed.DELETE("/roles/:id", perm("system:role:delete", controller.RoleDelete)...)
		authed.PUT("/roles/:id/permissions", perm("system:role:assign", controller.RoleAssignPermissions)...)
		authed.PUT("/roles/:id/resources", perm("system:role:assign", controller.RoleAssignResources)...)

		// 权限管理
		authed.POST("/permissions", perm("system:permission:add", controller.PermissionCreate)...)
		authed.PUT("/permissions/:id", perm("system:permission:edit", controller.PermissionUpdate)...)
		authed.DELETE("/permissions/:id", perm("system:permission:delete", controller.PermissionDelete)...)

		// 资源管理
		authed.POST("/resources", perm("system:resource:add", controller.ResourceCreate)...)
		authed.PUT("/resources/:id", perm("system:resource:edit", controller.ResourceUpdate)...)
		authed.DELETE("/resources/:id", perm("system:resource:delete", controller.ResourceDelete)...)

		// 流程管理写操作
		authed.POST("/flow/definitions", perm("flow:definition:add", controller.FlowDefinitionCreate)...)
		authed.PUT("/flow/definitions/:id", perm("flow:definition:edit", controller.FlowDefinitionUpdate)...)
		authed.POST("/flow/definitions/:id/publish", perm("flow:definition:publish", controller.FlowDefinitionPublish)...)
		authed.PUT("/flow/instances/:id/terminate", perm("flow:instance:terminate", controller.FlowInstanceTerminate)...)
		authed.PUT("/flow/tasks/:id/transfer", perm("flow:task:transfer", controller.FlowTaskTransfer)...)
		authed.PUT("/flow/tasks/:id/urge", perm("flow:task:urge", controller.FlowTaskUrge)...)
	}
}
