package controller

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"

	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/apiserver/service"
)

// ---------- 用户管理 ----------

// UserList GET /api/users
func UserList(ctx context.Context, c *app.RequestContext) {
	page, size := pageOf(c)
	p, err := service.ListUsers(page, size, c.Query("keyword"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// UserOptions GET /api/users/options
func UserOptions(ctx context.Context, c *app.RequestContext) {
	rows, err := service.ListRoleOptions()
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, rows, "")
}

// UserCreate POST /api/users
func UserCreate(ctx context.Context, c *app.RequestContext) {
	id, err := service.CreateUser(bindBody(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, utils.H{"id": id}, "创建成功")
}

// UserUpdate PUT /api/users/:id
func UserUpdate(ctx context.Context, c *app.RequestContext) {
	if err := service.UpdateUser(pathID(c, "id"), bindBody(c)); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "更新成功")
}

// UserDelete DELETE /api/users/:id
func UserDelete(ctx context.Context, c *app.RequestContext) {
	if err := service.DeleteUser(pathID(c, "id")); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "删除成功")
}

// UserAssignRoles PUT /api/users/:id/roles
func UserAssignRoles(ctx context.Context, c *app.RequestContext) {
	body := bindBody(c)
	roleIDs, _ := body["role_ids"].([]any)
	if err := service.AssignRoles(pathID(c, "id"), roleIDs); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "角色已更新")
}

// UserResetPwd PUT /api/users/:id/reset-pwd
func UserResetPwd(ctx context.Context, c *app.RequestContext) {
	body := bindBody(c)
	if err := service.ResetPassword(pathID(c, "id"), service.Str(body["new_password"])); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "密码已重置")
}

// ---------- 角色管理 ----------

// RoleList GET /api/roles
func RoleList(ctx context.Context, c *app.RequestContext) {
	page, size := pageOf(c)
	p, err := service.ListRoles(page, size, c.Query("keyword"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// RoleCreate POST /api/roles
func RoleCreate(ctx context.Context, c *app.RequestContext) {
	id, err := service.CreateRole(bindBody(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, utils.H{"id": id}, "创建成功")
}

// RoleUpdate PUT /api/roles/:id
func RoleUpdate(ctx context.Context, c *app.RequestContext) {
	if err := service.UpdateRole(pathID(c, "id"), bindBody(c)); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "更新成功")
}

// RoleDelete DELETE /api/roles/:id
func RoleDelete(ctx context.Context, c *app.RequestContext) {
	if err := service.DeleteRole(pathID(c, "id")); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "删除成功")
}

// RoleAssignPermissions PUT /api/roles/:id/permissions
func RoleAssignPermissions(ctx context.Context, c *app.RequestContext) {
	body := bindBody(c)
	ids, _ := body["permission_ids"].([]any)
	if err := service.AssignPermissions(pathID(c, "id"), ids); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "权限已更新")
}

// RoleAssignResources PUT /api/roles/:id/resources
func RoleAssignResources(ctx context.Context, c *app.RequestContext) {
	body := bindBody(c)
	ids, _ := body["resource_ids"].([]any)
	if err := service.AssignResources(pathID(c, "id"), ids); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "资源已更新")
}

// ---------- 权限管理 ----------

// PermissionList GET /api/permissions
func PermissionList(ctx context.Context, c *app.RequestContext) {
	page, size := pageOf(c)
	p, err := service.ListPermissions(page, size, c.Query("keyword"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// PermissionAll GET /api/permissions/all
func PermissionAll(ctx context.Context, c *app.RequestContext) {
	rows, err := service.ListAllPermissions()
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, rows, "")
}

// PermissionCreate POST /api/permissions
func PermissionCreate(ctx context.Context, c *app.RequestContext) {
	id, err := service.CreatePermission(bindBody(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, utils.H{"id": id}, "创建成功")
}

// PermissionUpdate PUT /api/permissions/:id
func PermissionUpdate(ctx context.Context, c *app.RequestContext) {
	if err := service.UpdatePermission(pathID(c, "id"), bindBody(c)); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "更新成功")
}

// PermissionDelete DELETE /api/permissions/:id
func PermissionDelete(ctx context.Context, c *app.RequestContext) {
	if err := service.DeletePermission(pathID(c, "id")); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "删除成功")
}

// ---------- 资源管理 ----------

// ResourceTree GET /api/resources/tree
func ResourceTree(ctx context.Context, c *app.RequestContext) {
	tree, err := service.ResourceTree()
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, tree, "")
}

// ResourceList GET /api/resources
func ResourceList(ctx context.Context, c *app.RequestContext) {
	rows, err := service.ListResources()
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, rows, "")
}

// ResourceCreate POST /api/resources
func ResourceCreate(ctx context.Context, c *app.RequestContext) {
	id, err := service.CreateResource(bindBody(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, utils.H{"id": id}, "创建成功")
}

// ResourceUpdate PUT /api/resources/:id
func ResourceUpdate(ctx context.Context, c *app.RequestContext) {
	if err := service.UpdateResource(pathID(c, "id"), bindBody(c)); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "更新成功")
}

// ResourceDelete DELETE /api/resources/:id
func ResourceDelete(ctx context.Context, c *app.RequestContext) {
	if err := service.DeleteResource(pathID(c, "id")); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "删除成功")
}
