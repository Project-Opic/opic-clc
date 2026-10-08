// clc_template.go —— 集群模板控制器（FUNC-36~43，AGG-CLC-08；m5 权限码 PERM-FUNC-36~43）。
//
// 与其余业务接口同一信封（OKJSON/FailJSON）；所有不变量的中文提示都在服务层给出，
// 本层只做入参搬运与权限挂载（读操作也按行为级权限码挂，沿用本仓 GET 走 PERM 的口径）。
package business

import (
	"strings"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
)

// ClcTemplateList GET /api/clc/template (FUNC-36;REP-14 只出配方头，参数行不进列表页)
func ClcTemplateList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListClcTemplates(page, size, map[string]any{
		"status": c.Query("status"), "is_default": c.Query("is_default"),
		"deleted": c.Query("deleted"), "keyword": c.Query("keyword"),
	})
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// ClcTemplateGet GET /api/clc/template/:no (FUNC-37;REP-15 主从详情，按模板编号读取)
func ClcTemplateGet(c *gin.Context) {
	out, err := service.GetClcTemplate(c.Param("no"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "")
}

// ClcTemplateCreate POST /api/clc/template (FUNC-38 登记：草稿态 + 同事务整组子实体)
func ClcTemplateCreate(c *gin.Context) {
	out, err := service.CreateClcTemplate(common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "模板已登记（草稿态；启用前需补齐角色拓扑且至少一个 MASTER）")
}

// ClcTemplateGroupsSave PUT /api/clc/template/:no/groups (FUNC-39 整组编辑：INV-52 整组替换)
func ClcTemplateGroupsSave(c *gin.Context) {
	out, err := service.SaveClcTemplateGroups(c.Param("no"), common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "已整组替换（未提交的行即不存在；不改动任何已建成集群，R-20）")
}

// ClcTemplateEnable POST /api/clc/template/:no/enable (FUNC-40)
func ClcTemplateEnable(c *gin.Context) {
	body := common.BindJSON(c)
	out, err := service.SetClcTemplateState(c.Param("no"), "enable",
		strings.TrimSpace(service.Str(body["reason"])), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "模板已启用")
}

// ClcTemplateDisable POST /api/clc/template/:no/disable (FUNC-40；默认项须先撤默认)
func ClcTemplateDisable(c *gin.Context) {
	body := common.BindJSON(c)
	out, err := service.SetClcTemplateState(c.Param("no"), "disable",
		strings.TrimSpace(service.Str(body["reason"])), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "模板已停用（既有集群引用不回收，R-22）")
}

// ClcTemplateSetDefault POST /api/clc/template/:no/default (FUNC-41 原子「撤原默认 + 置新默认」)
func ClcTemplateSetDefault(c *gin.Context) {
	body := common.BindJSON(c)
	out, err := service.SetClcTemplateDefault(c.Param("no"),
		strings.TrimSpace(service.Str(body["reason"])), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "已设为默认（原默认位已同事务撤销；生效面仅向导第二步预选）")
}

// ClcTemplateDelete DELETE /api/clc/template/:no (FUNC-42 软删除：引用反查 + 默认位前置)
func ClcTemplateDelete(c *gin.Context) {
	out, err := service.DeleteClcTemplate(c.Param("no"),
		strings.TrimSpace(c.Query("reason")), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "模板已软删除（行保留、名称释放；既有集群引用不失效，INV-49）")
}

// ClcTemplateClone POST /api/clc/template/clone (FUNC-43 克隆：既有模板或既有集群，二选一)
func ClcTemplateClone(c *gin.Context) {
	out, err := service.CloneClcTemplate(common.BindJSON(c), common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "克隆完成（产物为新登记一份草稿；集群来源时凭据形态值已按 INV-08 剔除）")
}
