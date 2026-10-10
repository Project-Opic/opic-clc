// clc_readonly.go —— 只读代理与前置预检的控制器
// (FUNC-47 / FUNC-48 / FUNC-49 / FUNC-50 / FUNC-51;m5 权限码 PERM-FUNC-47 / 48 / 49 / 50 / 51)。
//
// 五条都是用户侧 QUERY:走 OPIC 登录组信封(common.OKJSON/FailJSON);门面组由对端承担,
// 本域不出现 flat JSON,也不新挂 middleware.FacadeRequired。控制器只做入参搬运 + 权限挂载,
// 语义 / 不可达分档 / 留痕全部在服务层。
package business

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
)

// ClcNodeLoginIdentities GET /api/clc/cluster/:id/node-login-identities (FUNC-47,REP-16)。
// 「复制登录标识」的读侧:返回登录用户@节点地址拼接串,零凭据出口(INV-08);
// 复制动作 = 前端剪贴板写入,后端不产生业务写入、不发事件、不留痕。
// 权限:对全部只读角色开放(含 CLC-ROLE-04);PERM-FUNC-47。
func ClcNodeLoginIdentities(c *gin.Context) {
	out, err := service.ClusterNodeLoginIdentities(common.PathID(c, "id"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "登录标识已就绪(REF-18 展示口径:登录用户@节点地址;复制动作 = 前端剪贴板写入,后端不留痕)")
}

// ClcIgnDeliveryConfig GET /api/clc/cluster/:id/ign-config (FUNC-48,REP-17)。
// 经既有交付门面代理读 O-IGN 字段级视图:同一令牌、不前端直连、不留副本、不落对象(R-25);
// 展示对端结论,本域不重算、不合并四层;对端不可达时 200 + peer_unreachable 显式标识 + 跳转入口(REF-12)。
// query:file_face / param_template_ref / version_ref 三个都可选,留空由对端按自己口径回落。
func ClcIgnDeliveryConfig(c *gin.Context) {
	out, err := service.IgnDeliveryConfigView(
		common.PathID(c, "id"),
		c.Query("file_face"),
		c.Query("param_template_ref"),
		c.Query("version_ref"),
	)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "交付配置字段级视图(由对端承载,本域只代理读、不重算四层、不留副本;不可达时保留跳转入口不缓存冒充)")
}

// ClcIgnDeliveryConfigRaw GET /api/clc/cluster/:id/ign-config-raw?file_face=HOSTS|CONFIG
// (FUNC-49,REP-18)。原文只读、一次一面、无编辑态;凭据形态的行已由对端整行掩码(INV-08),
// 本域不二次解析、不据原文做判定。逐次留痕:谁 / 何时 / 哪个集群 / 哪一面原文(审计,不写本域业务对象)。
// 权限:独立权限码 PERM-FUNC-49 —— 不随 PERM-FUNC-48 附带;持 48 不持 49 由 RequirePermission 拒 403。
func ClcIgnDeliveryConfigRaw(c *gin.Context) {
	out, err := service.IgnDeliveryConfigRawView(
		common.PathID(c, "id"),
		c.Query("file_face"),
		common.UserOf(c),
	)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "交付配置原文只读视图(一次一面;逐次留痕已写审计。要改字段请跳对端字段级覆写口)")
}

// ClcDeliveryReadinessCheck GET /api/clc/cluster/:id/delivery-readiness (FUNC-50;queryReportRef: null,不建报表)。
// 交付前置完备性即时预检:只读本域事实与既有降级结论,不新增跨中心同步探活;
// 结论是即时提示、不拦截提交(拦截仍由各 INV / R 在写入瞬间执行);不写本域对象、不产生事件。
func ClcDeliveryReadinessCheck(c *gin.Context) {
	out, err := service.DeliveryReadinessCheck(common.PathID(c, "id"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "交付前置完备性预检结论(即时提示、不拦截提交;各 INV / R 在写入瞬间执行拦截)")
}

// ClcRemoteExecLog GET /api/clc/exec-log/:ref?since_seq=&limit= (FUNC-51,REP-19)。
// 交付执行日志代理查看:按远端任务引用(:ref = 任务头 remoteTaskRef)代理直展 O-IGN 的
// ezctl 逐行运行原文 —— 游标增量、响应即弃、不落 clc_task_log(R-25 日志腿 / REF-12);
// 对端写前已掩码,本域不二次脱敏也不据行文本做判定(INV-08 / R-17)。
// 逐次留痕与不可达分档都在服务层,界面拿到的 peer_unreachable 决定「显式说明 + 跳转兜底」。
// 权限:独立权限码 PERM-FUNC-51(与 PERM-FUNC-49 同档同判据,不与回执类可见性合并)。
func ClcRemoteExecLog(c *gin.Context) {
	sinceSeq, _ := strconv.ParseInt(c.Query("since_seq"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	out, err := service.RemoteExecLogView(c.Param("ref"), sinceSeq, limit, common.UserOf(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, out, "远程执行日志代理视图(对端逐行原文,游标增量、不留副本;不可达时显式说明并保留跳转入口)")
}
