// clc_template.go —— 集群模板（AGG-CLC-08，FUNC-36~43）的服务入口。
//
// 模板是「输入配方」不是「运行依赖」（R-20）：登记/编辑/启停/设默认/软删/克隆都只动
// 本聚合自己的三张表，绝不回写已建成的集群、节点与参数（INV-49/R-20 同源）。
// 对端 O-IGN 参数面只透传标识、不解析内容（R-21）；凭据形态值不得进模板（INV-08，
// 判定复用 delivery.MaskSecret —— 掩码后与原值不同即命中凭据形态，判定与掩码单点同源）。
//
// 不变量在本层的中文口径（库里唯一索引只是兜底，报错文案要能指导下一步动作）：
//   - INV-51：名称未删除范围内唯一（软删释放名称）；is_default 为真者全局至多一行且须
//     ENABLED —— 库侧 uq_template_default 只兜「至多一行」（谓词不带状态条件，与 IGN
//     uq_ign_template_default_per_face 同形态），「默认须启用 / 停用先撤默认 / 删除先撤默认」由本层校验；
//   - INV-52：两组子实体整组替换（编辑即提交新的一组行，历史行以 flag=0 保留）；
//     组名/参数键模板内唯一、同角色模板内至多一行、expected_count ≥ 1；
//   - R-22：被未销毁集群引用即拒删并列出全部出处；停用不回收任何集群引用。
package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/service/delivery"
	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// 模板状态（字典 CLC_TEMPLATE_STATE；INV-51 迁移固定 DRAFT→ENABLED⇄DISABLED）。
const (
	TemplateStatusDraft    = "DRAFT"
	TemplateStatusEnabled  = "ENABLED"
	TemplateStatusDisabled = "DISABLED"
)

// clcTemplateStateLabel 状态中文（报错文案可读）。
func clcTemplateStateLabel(code string) string {
	switch code {
	case TemplateStatusDraft:
		return "草稿"
	case TemplateStatusEnabled:
		return "启用"
	case TemplateStatusDisabled:
		return "停用"
	}
	return code
}

// clcTemplateLiveByNo 按编号取未删除模板行；不存在即 NotFound（编号只读，FUNC-38 生成）。
func clcTemplateLiveByNo(db *gorm.DB, templateNo string) (map[string]any, error) {
	row, err := store.One(db, `SELECT * FROM clc_cluster_template WHERE template_no = ? AND flag = 1`, templateNo)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NotFound("集群模板（软删除行请走列表的已删除筛选）")
	}
	return row, nil
}

// clcTemplateHasCredentialShape INV-08 判定：值经同一套凭据掩码后发生变化即命中凭据形态。
// 判定与掩码共用 delivery.MaskSecret 单点实现，两把尺子各写一套就必然漏。
func clcTemplateHasCredentialShape(v string) bool {
	return delivery.MaskSecret(v) != v
}

// ---------- 子实体行归一（FUNC-38/39/43 共用的 INV-52 行约束，保存即校验） ----------

// clcNormalizeTemplateGroups 节点组整组归一：组名模板内唯一、同角色至多一行、
// 角色 ∈ CLC_NODE_ROLE、expected_count ≥ 1；返回按 sort_no（入参序）落库的行集。
func clcNormalizeTemplateGroups(raw any) ([]map[string]any, error) {
	items, _ := raw.([]any)
	seenName := map[string]bool{}
	seenRole := map[string]bool{}
	out := make([]map[string]any, 0, len(items))
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, Fail(errcode.ErrParam, "节点组必须是对象数组（group_name + node_role + expected_count）（INV-52）")
		}
		name := strings.TrimSpace(Str(m["group_name"]))
		role := strings.ToUpper(strings.TrimSpace(Str(m["node_role"])))
		count := paramInt(m["expected_count"])
		if name == "" {
			return nil, Fail(errcode.ErrParam, "第 %d 行节点组缺少分组名称（组名在模板内唯一，INV-52）", i+1)
		}
		if !nodeRoleAllowed[role] {
			return nil, bizRuleFail("INV-11", "第 %d 行节点组角色 %q 非法：只能是 MASTER / WORKER / INFRA（CLC_NODE_ROLE 字典）", i+1, role)
		}
		if count < 1 {
			return nil, bizRuleFail("INV-52", "第 %d 行节点组期望数量须为不小于 1 的整数（配方要能算出节点位），收到「%v」", i+1, m["expected_count"])
		}
		key := strings.ToLower(name)
		if seenName[key] {
			return nil, bizRuleFail("INV-52", "分组名称「%s」在本次提交内重复（组名模板内唯一）", name)
		}
		seenName[key] = true
		if seenRole[role] {
			return nil, bizRuleFail("INV-52", "角色 %s 在本次提交内出现多行（同角色模板内至多一行，数量由 expected_count 表达）", role)
		}
		seenRole[role] = true
		out = append(out, map[string]any{
			"group_name": name, "node_role": role, "expected_count": count, "sort_no": i + 1,
		})
	}
	return out, nil
}

// clcNormalizeTemplateParams 参数行整组归一：键模板内唯一；值不得含凭据形态（INV-08，
// 拒收而非掩码入库——掩码值进配方会把「已掩码」当成默认值传染给每一次实例化）。
// 空串值是合法口径：表示该键须现场填（向导兜底），不是缺省。
func clcNormalizeTemplateParams(raw any) ([]map[string]any, error) {
	items, _ := raw.([]any)
	seenKey := map[string]bool{}
	out := make([]map[string]any, 0, len(items))
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, Fail(errcode.ErrParam, "参数行必须是对象数组（param_key + param_value），提交形态非法（INV-52）")
		}
		key := strings.TrimSpace(Str(m["param_key"]))
		value := Str(m["param_value"])
		if key == "" {
			return nil, Fail(errcode.ErrParam, "第 %d 行参数缺少参数键（键在模板内唯一，沿用集群参数键域口径）", i+1)
		}
		if len([]rune(value)) > 500 {
			return nil, Fail(errcode.ErrParam, "参数 %q 的默认值超过 500 字，收不进配方行", key)
		}
		if clcTemplateHasCredentialShape(value) {
			return nil, bizRuleFail("INV-08",
				"参数 %q 的默认值命中凭据形态（口令/令牌/私钥类）：凭据值不得进模板——要引用现场凭据请登记存放位置标识（如 vault 路径），或留空由实例化时现场填", key)
		}
		k := strings.ToLower(key)
		if seenKey[k] {
			return nil, bizRuleFail("INV-52", "参数键 %q 在本次提交内重复（同键模板内唯一，整组替换没有行级增量）", key)
		}
		seenKey[k] = true
		out = append(out, map[string]any{
			"param_key": key, "param_value": value,
			"description": strings.TrimSpace(Str(m["description"])), "sort_no": i + 1,
		})
	}
	return out, nil
}

// clcWriteTemplateChildrenTx 整组替换写侧：旧行一律 flag=0 保留（INV-52 级联为逻辑删除），
// 新行按归一结果整组插入；FUNC-38/39/43 共用同一落库口径。
func clcWriteTemplateChildrenTx(db *gorm.DB, templateNo string, groups, params []map[string]any, user UserInfo) error {
	if _, err := store.Exec(db, `UPDATE clc_template_node_group SET flag = 0, updated_by = ?, updated_at = ?
		WHERE template_ref = ? AND flag = 1`, user.ID, nowText(), templateNo); err != nil {
		return err
	}
	if _, err := store.Exec(db, `UPDATE clc_template_param SET flag = 0, updated_by = ?, updated_at = ?
		WHERE template_ref = ? AND flag = 1`, user.ID, nowText(), templateNo); err != nil {
		return err
	}
	for _, g := range groups {
		if _, err := store.Exec(db, `INSERT INTO clc_template_node_group
			(template_ref, group_name, node_role, expected_count, sort_no, created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			templateNo, g["group_name"], g["node_role"], g["expected_count"], g["sort_no"], user.ID, user.ID); err != nil {
			return err
		}
	}
	for _, p := range params {
		if _, err := store.Exec(db, `INSERT INTO clc_template_param
			(template_ref, param_key, param_value, description, sort_no, created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			templateNo, p["param_key"], p["param_value"], nullableText(Str(p["description"])), p["sort_no"], user.ID, user.ID); err != nil {
			return err
		}
	}
	return nil
}

// nullableText 空串按 NULL 落库（与「显式空值」区分，展示层不出现空串/NULL 两种空）。
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---------- 视图组装（REP-14 列表 / REP-15 详情共用形状） ----------

// clcTemplateRoleSummary 角色分布摘要（如 MASTER×3 + WORKER×5）：由节点组即时聚合，不落库字段。
func clcTemplateRoleSummary(groups []map[string]any) string {
	order := []string{NodeRoleMaster, NodeRoleWorker, NodeRoleInfra}
	sum := map[string]int64{}
	for _, g := range groups {
		sum[Str(g["node_role"])] += paramInt(g["expected_count"])
	}
	parts := []string{}
	for _, r := range order {
		if sum[r] > 0 {
			parts = append(parts, fmt.Sprintf("%s×%d", r, sum[r]))
		}
	}
	for _, g := range groups { // 字典外角色理论进不来（INV-11 已拦），兜底原样汇总
		r := Str(g["node_role"])
		if sum[r] > 0 && r != NodeRoleMaster && r != NodeRoleWorker && r != NodeRoleInfra {
			parts = append(parts, fmt.Sprintf("%s×%d", r, sum[r]))
		}
	}
	if len(parts) == 0 {
		return "（无节点组）"
	}
	return strings.Join(parts, " + ")
}

// clcTemplateUsedByClusters R-22 删除保护与 FUNC-37「被哪些集群用过」共用同一条反查：
// 未销毁集群的 templateRef 命中清单（出处即下一步动作的位置：集群列表屏按引用筛）。
func clcTemplateUsedByClusters(db *gorm.DB, templateNo string) ([]map[string]any, error) {
	return store.List(db, `SELECT cluster_no, name, status FROM clc_cluster
		WHERE flag = 1 AND status <> ? AND template_ref = ? ORDER BY cluster_no`, ClusterStatusDestroyed, templateNo)
}

// clcTemplateRowView 列表行视图：只出配方头 + 角色分布摘要，参数行不进列表页
// （默认值可能含敏感项，列表不展开——REP-14 口径注记）；默认徽标只在启用态出现（INV-51 界面侧投影）。
func clcTemplateRowView(row map[string]any, groupRows []map[string]any) map[string]any {
	v := map[string]any{}
	for k, x := range row {
		v[k] = x
	}
	live := Int(row["flag"]) == 1
	v["deleted"] = !live
	v["default_badge"] = live && Str(row["template_status"]) == TemplateStatusEnabled && truthy(row["is_default"])
	if groupRows != nil {
		v["role_summary"] = clcTemplateRoleSummary(groupRows)
	} else {
		v["role_summary"] = "（取数不可用）"
	}
	return v
}

// ---------- FUNC-36 列表查询 ----------

// ListClcTemplates FUNC-36 模板列表（REP-14）：过滤状态 / 是否默认 / 关键字（名称、说明）；
// 已软删默认可见可筛（口径 3 家族）；参数行不进列表页（REP-14 口径注记）。
func ListClcTemplates(page, size int, p map[string]any) (*Page, error) {
	var conds []string
	args := []any{}
	if v := strings.ToUpper(strings.TrimSpace(Str(p["status"]))); v != "" {
		conds = append(conds, "template_status = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(Str(p["is_default"])); v != "" {
		want := v == "1" || strings.EqualFold(v, "true")
		conds = append(conds, "is_default = ?")
		args = append(args, want)
	}
	if v := strings.TrimSpace(Str(p["deleted"])); v != "" {
		// 已软删默认可见可筛（口径 3 家族）：deleted=1 只看软删行，deleted=0 只看存活行
		deleted := v == "1" || strings.EqualFold(v, "true")
		conds = append(conds, "flag = ?")
		if deleted {
			args = append(args, 0)
		} else {
			args = append(args, 1)
		}
	}
	if kw := strings.TrimSpace(Str(p["keyword"])); kw != "" {
		conds = append(conds, "(LOWER(name) LIKE ? OR LOWER(COALESCE(description,'')) LIKE ?)")
		like := "%" + strings.ToLower(kw) + "%"
		args = append(args, like, like)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_cluster_template `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`SELECT * FROM clc_cluster_template `+where+` ORDER BY flag ASC, id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		groupRows, gerr := qList(`SELECT node_role, expected_count FROM clc_template_node_group
			WHERE template_ref = ? AND flag = 1 ORDER BY sort_no`, Str(r["template_no"]))
		if gerr != nil {
			groupRows = nil
		}
		list = append(list, clcTemplateRowView(r, groupRows))
	}
	return &Page{List: list, Total: total, PageV: page, Size: size}, nil
}

// ---------- FUNC-37 详情查询 ----------

// GetClcTemplate FUNC-37 模板详情（REP-15 主从）：模板头 + 节点组 + 参数行全量 +
// sourceClusterRef 来源；「被哪些集群用过」按需反查（与 R-22 删除保护同一条查询）。
// 参数值列过 INV-08 同一套掩码兜底（登记时已拒凭据形态，这里防库外直写的脏值流出）。
func GetClcTemplate(templateNo string) (map[string]any, error) {
	templateNo = strings.TrimSpace(templateNo)
	row, err := qOne(`SELECT * FROM clc_cluster_template WHERE template_no = ?`, templateNo)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NotFound("集群模板")
	}
	groups, err := qList(`SELECT group_name, node_role, expected_count, sort_no FROM clc_template_node_group
		WHERE template_ref = ? AND flag = 1 ORDER BY sort_no`, templateNo)
	if err != nil {
		return nil, err
	}
	params, err := qList(`SELECT param_key, param_value, description, sort_no FROM clc_template_param
		WHERE template_ref = ? AND flag = 1 ORDER BY sort_no`, templateNo)
	if err != nil {
		return nil, err
	}
	for _, p := range params {
		masked := delivery.MaskSecret(Str(p["param_value"]))
		p["param_value_masked"] = masked
		p["masked_hit"] = masked != Str(p["param_value"])
		if Str(p["param_value"]) == "" {
			p["display_note"] = "须现场填（空串是合法配方值，不是缺失）"
		}
	}
	usedBy, err := clcTemplateUsedByClusters(store.DB, templateNo)
	if err != nil {
		usedBy = []map[string]any{}
	}
	out := clcTemplateRowView(row, groups)
	out["node_groups"] = groups
	out["params"] = params
	out["used_by_clusters"] = usedBy
	return out, nil
}

// ---------- FUNC-38 登记 ----------

// CreateClcTemplate FUNC-38 模板登记：名称未删除范围内唯一（INV-51，软删释放名称）；
// 编号 TPL-YYYYMMDD-NNNN 生成后只读；状态置草稿、默认位为假；节点组与参数行同事务整组写入。
// 交付版本引用只校形态（REF-05）；O-IGN 参数面标识只透传、不解析不校验内容（R-21）。
func CreateClcTemplate(d map[string]any, user UserInfo) (map[string]any, error) {
	name := strings.TrimSpace(Str(d["name"]))
	if name == "" {
		return nil, Fail(errcode.ErrParam, "模板名称必填（重名在未删除范围内即拒绝；要腾出名称请对占用行执行软删除，FUNC-42）")
	}
	if len([]rune(name)) > 100 {
		return nil, Fail(errcode.ErrParam, "模板名称不得超过 100 字")
	}
	desc := strings.TrimSpace(Str(d["description"]))
	if len([]rune(desc)) > 500 {
		return nil, Fail(errcode.ErrParam, "模板说明不得超过 500 字")
	}
	versionRef := strings.TrimSpace(Str(d["version_ref"]))
	if err := checkVersionRef(versionRef); err != nil {
		return nil, err
	}
	paramTemplateRef := strings.TrimSpace(Str(d["param_template_ref"]))
	if len([]rune(paramTemplateRef)) > 100 {
		return nil, Fail(errcode.ErrParam, "O-IGN 参数面引用标识不得超过 100 字（本域只透传标识、不解析内容，R-21）")
	}
	groups, err := clcNormalizeTemplateGroups(d["node_groups"])
	if err != nil {
		return nil, err
	}
	params, err := clcNormalizeTemplateParams(d["params"])
	if err != nil {
		return nil, err
	}
	var no string
	err = tx(func(db *gorm.DB) error {
		dup, err := store.One(db, `SELECT id FROM clc_cluster_template WHERE flag = 1 AND LOWER(name) = LOWER(?)`, name)
		if err != nil {
			return err
		}
		if dup != nil {
			return bizRuleFail("INV-51", "模板名称「%s」在未删除范围内已存在（重名即拒绝；软删除释放名称）", name)
		}
		no, err = genBizNo(db, NoPrefixTemplate, "clc_cluster_template", "template_no")
		if err != nil {
			return err
		}
		if _, err := store.Exec(db, `INSERT INTO clc_cluster_template
			(template_no, name, description, version_ref, param_template_ref, template_status, is_default, created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, FALSE, ?, ?)`,
			no, name, nullableText(desc), nullableText(versionRef), nullableText(paramTemplateRef),
			TemplateStatusDraft, user.ID, user.ID); err != nil {
			return err
		}
		return clcWriteTemplateChildrenTx(db, no, groups, params, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-38", fmt.Sprintf(
		"登记集群模板 %s（%s）：节点组 %d 行、参数行 %d 行、版本引用 %q、参数面引用 %q",
		no, name, len(groups), len(params), versionRef, paramTemplateRef))
	return GetClcTemplate(no)
}

// ---------- FUNC-39 整组编辑 ----------

// SaveClcTemplateGroups FUNC-39 整组编辑：三态均可编辑、停用态编辑后仍是停用态（不回 DRAFT）；
// 提交的是「一整组新行」，旧行 flag=0 保留（INV-52 整组替换，没有行级增量改写）；
// 模板头字段就地更新；不触碰任何已建成集群（R-20 建成后不回写、不跟随）。
func SaveClcTemplateGroups(templateNo string, d map[string]any, user UserInfo) (map[string]any, error) {
	reason := strings.TrimSpace(Str(d["reason"]))
	if reason == "" {
		return nil, Fail(errcode.ErrParam, "模板整组编辑必须携带事由（配方是跨集群复用资产，改了什么要可回放——与 IGN 侧字段行整组替换同纪律）")
	}
	groups, err := clcNormalizeTemplateGroups(d["node_groups"])
	if err != nil {
		return nil, err
	}
	params, err := clcNormalizeTemplateParams(d["params"])
	if err != nil {
		return nil, err
	}
	err = tx(func(db *gorm.DB) error {
		tpl, e := clcTemplateLiveByNo(db, templateNo)
		if e != nil {
			return e
		}
		sets := []string{"updated_by = ?", "updated_at = ?"}
		args := []any{user.ID, nowText()}
		if name := strings.TrimSpace(Str(d["name"])); name != "" && name != Str(tpl["name"]) {
			dup, e := store.One(db, `SELECT template_no FROM clc_cluster_template
				WHERE flag = 1 AND LOWER(name) = LOWER(?) AND template_no <> ?`, name, templateNo)
			if e != nil {
				return e
			}
			if dup != nil {
				return bizRuleFail("INV-51", "模板名称「%s」在未删除范围内已被模板 %s 占用（重名即拒绝；软删除释放名称）", name, Str(dup["template_no"]))
			}
			sets = append(sets, "name = ?")
			args = append(args, name)
		}
		if _, has := d["description"]; has {
			sets = append(sets, "description = ?")
			args = append(args, nullableText(strings.TrimSpace(Str(d["description"]))))
		}
		if _, has := d["version_ref"]; has {
			vr := strings.TrimSpace(Str(d["version_ref"]))
			if e := checkVersionRef(vr); e != nil {
				return e
			}
			sets = append(sets, "version_ref = ?")
			args = append(args, nullableText(vr))
		}
		if _, has := d["param_template_ref"]; has {
			pr := strings.TrimSpace(Str(d["param_template_ref"]))
			if len([]rune(pr)) > 100 {
				return Fail(errcode.ErrParam, "O-IGN 参数面引用标识不得超过 100 字（只透传标识、不解析内容，R-21）")
			}
			sets = append(sets, "param_template_ref = ?")
			args = append(args, nullableText(pr))
		}
		if _, e := store.Exec(db, fmt.Sprintf("UPDATE clc_cluster_template SET %s WHERE template_no = ?",
			strings.Join(sets, ", ")), append(args, templateNo)...); e != nil {
			return e
		}
		return clcWriteTemplateChildrenTx(db, templateNo, groups, params, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-39", fmt.Sprintf(
		"集群模板 %s 整组编辑：节点组 %d 行、参数行 %d 行（旧行 flag=0 保留）：事由 %s",
		templateNo, len(groups), len(params), reason))
	return GetClcTemplate(templateNo)
}

// ---------- FUNC-40 启用 / 停用 ----------

// SetClcTemplateState FUNC-40 启用/停用：迁移固定 DRAFT→ENABLED⇄DISABLED（INV-51），
// 其余迁移即拒；启用前置 = 节点组非空且至少一个 MASTER（配方至少要能描述「谁是主控」）；
// 停用默认项即拒（先撤默认再停用）；停用不回收任何集群引用（R-22）、不改动任何集群（INV-49）。
func SetClcTemplateState(templateNo, action, reason string, user UserInfo) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, Fail(errcode.ErrParam, "启用 / 停用是治理动作，必须携带事由（与整组编辑 / 设默认 / 软删除同口径）")
	}
	if action != "enable" && action != "disable" {
		return nil, Fail(errcode.ErrParam, "未知的模板状态动作 %q（只支持 enable / disable）", action)
	}
	var tpl map[string]any
	err := tx(func(db *gorm.DB) error {
		var e error
		tpl, e = clcTemplateLiveByNo(db, templateNo)
		if e != nil {
			return e
		}
		status := Str(tpl["template_status"])
		switch action {
		case "enable":
			if status == TemplateStatusEnabled {
				return Fail(errcode.ErrState, "模板已处于启用态（合法迁移只有 草稿→启用 与 停用→启用）")
			}
			groups, e := store.List(db, `SELECT node_role, expected_count FROM clc_template_node_group
				WHERE template_ref = ? AND flag = 1`, templateNo)
			if e != nil {
				return e
			}
			hasMaster := false
			for _, g := range groups {
				if Str(g["node_role"]) == NodeRoleMaster {
					hasMaster = true
				}
			}
			if len(groups) == 0 || !hasMaster {
				return Fail(errcode.ErrState,
					"启用前置不满足：节点组须非空且至少一个 MASTER 组（配方至少要能描述「谁是主控」）——请先用整组编辑（FUNC-39）补齐角色拓扑")
			}
			if _, e := store.Exec(db, `UPDATE clc_cluster_template
				SET template_status = 'ENABLED', updated_by = ?, updated_at = ? WHERE template_no = ?`,
				user.ID, nowText(), templateNo); e != nil {
				return e
			}
		case "disable":
			if status != TemplateStatusEnabled {
				return Fail(errcode.ErrState, "只有启用态模板可以停用（当前「%s」；草稿不涉及停用，重复停用无意义）", clcTemplateStateLabel(status))
			}
			if truthy(tpl["is_default"]) {
				return Fail(errcode.ErrState,
					"默认模板不可直接停用（INV-51：默认行必须处于启用态）——请先对另一张启用态模板执行「设为默认」（FUNC-41 会自动摘除本张默认位），再停用本张")
			}
			if _, e := store.Exec(db, `UPDATE clc_cluster_template
				SET template_status = 'DISABLED', updated_by = ?, updated_at = ? WHERE template_no = ?`,
				user.ID, nowText(), templateNo); e != nil {
				return e
			}
		}
		var e2 error
		tpl, e2 = store.One(db, `SELECT * FROM clc_cluster_template WHERE template_no = ?`, templateNo)
		return e2
	})
	if err != nil {
		return nil, err
	}
	verb := "启用"
	if action == "disable" {
		verb = "停用"
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-40", fmt.Sprintf(
		"集群模板 %s %s：事由 %s（停用不回收任何集群引用，R-22/INV-49）", templateNo, verb, reason))
	return GetClcTemplate(templateNo)
}

// ---------- FUNC-41 设为默认 ----------

// SetClcTemplateDefault FUNC-41 设为默认：目标须处于 ENABLED（INV-51，「默认须启用」这条
// 库侧谓词不管、由本层把关——uq_template_default 只兜「全局至多一行默认」）；
// 事务内「撤销原默认 + 置新默认」原子完成；生效面只是创建向导第二步的预选，
// 不改动任何既有集群（与 IGN 侧「改默认不会让存量参数漂移」同纪律）。
func SetClcTemplateDefault(templateNo, reason string, user UserInfo) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, Fail(errcode.ErrParam, "设为默认必须携带事由（影响面是「后续新建集群缺省挂哪张」，跨集群动作，与配方维护分档）")
	}
	var tpl map[string]any
	err := tx(func(db *gorm.DB) error {
		var e error
		tpl, e = clcTemplateLiveByNo(db, templateNo)
		if e != nil {
			return e
		}
		if Str(tpl["template_status"]) != TemplateStatusEnabled {
			return Fail(errcode.ErrState, "设为默认前置不满足：目标模板须处于启用态（当前「%s」；INV-51：默认行必须处于启用态）",
				clcTemplateStateLabel(Str(tpl["template_status"])))
		}
		if _, e := store.Exec(db, `UPDATE clc_cluster_template
			SET is_default = FALSE, updated_by = ?, updated_at = ?
			WHERE flag = 1 AND is_default = TRUE AND template_no <> ?`,
			user.ID, nowText(), templateNo); e != nil {
			return e
		}
		if _, e := store.Exec(db, `UPDATE clc_cluster_template
			SET is_default = TRUE, updated_by = ?, updated_at = ? WHERE template_no = ?`,
			user.ID, nowText(), templateNo); e != nil {
			return e
		}
		var e2 error
		tpl, e2 = store.One(db, `SELECT * FROM clc_cluster_template WHERE template_no = ?`, templateNo)
		return e2
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-41", fmt.Sprintf(
		"集群模板 %s 设为默认（原默认行已同事务撤销）：事由 %s", templateNo, reason))
	return GetClcTemplate(templateNo)
}

// ---------- FUNC-42 软删除 ----------

// DeleteClcTemplate FUNC-42 模板软删除：前置反查未销毁集群引用（R-22），命中即拒并列出
// 全部出处（出处指到下一步动作的位置）；默认项须先撤默认（INV-51）；软删为终态、行保留，
// 子实体随聚合级联置 flag=0；不改动任何集群的 templateRef（INV-49 只读语义）。
func DeleteClcTemplate(templateNo, reason string, user UserInfo) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, Fail(errcode.ErrParam, "模板软删除必须携带事由（配方资产的去留要可回放，与治理动作同口径）")
	}
	tpl, err := qOne(`SELECT * FROM clc_cluster_template WHERE template_no = ? AND flag = 1`, templateNo)
	if err != nil {
		return nil, err
	}
	if tpl == nil {
		return nil, NotFound("集群模板")
	}
	usedBy, err := clcTemplateUsedByClusters(store.DB, templateNo)
	if err != nil {
		return nil, err
	}
	if len(usedBy) > 0 {
		parts := make([]string, 0, len(usedBy))
		for _, c := range usedBy {
			parts = append(parts, fmt.Sprintf("%s（%s，状态 %s）", Str(c["cluster_no"]), Str(c["name"]), Str(c["status"])))
		}
		return nil, Fail(errcode.ErrForbidden,
			"模板 %s 正被 %d 个未销毁集群引用，不可删除（R-22 删除保护）。出处：%s —— 出处即下一步动作的位置：集群列表屏可按来源模板筛出这些行；引用本身不因模板软删失效（INV-49），确认配方不再复用后再删",
			templateNo, len(usedBy), strings.Join(parts, "；"))
	}
	if truthy(tpl["is_default"]) {
		return nil, Fail(errcode.ErrState,
			"默认模板须先撤销默认位才可删除（INV-51：默认行必须处于启用态；删除会连带摘除默认位，先让向导第二步明确挂哪张）")
	}
	err = tx(func(db *gorm.DB) error {
		cur, e := clcTemplateLiveByNo(db, templateNo)
		if e != nil {
			return e
		}
		if truthy(cur["is_default"]) {
			return Fail(errcode.ErrState, "默认模板须先撤销默认位才可删除（前置校验与写入同事务复查，INV-51）")
		}
		if _, e := store.Exec(db, `UPDATE clc_template_node_group SET flag = 0, updated_by = ?, updated_at = ?
			WHERE template_ref = ? AND flag = 1`, user.ID, nowText(), templateNo); e != nil {
			return e
		}
		if _, e := store.Exec(db, `UPDATE clc_template_param SET flag = 0, updated_by = ?, updated_at = ?
			WHERE template_ref = ? AND flag = 1`, user.ID, nowText(), templateNo); e != nil {
			return e
		}
		_, e = store.Exec(db, `UPDATE clc_cluster_template
			SET flag = 0, is_default = FALSE, updated_by = ?, updated_at = ? WHERE template_no = ?`,
			user.ID, nowText(), templateNo)
		return e
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-42", fmt.Sprintf(
		"集群模板 %s 软删除（子实体随聚合级联 flag=0，名称随删除释放；既有集群引用不失效，INV-49）：事由 %s",
		templateNo, reason))
	return GetClcTemplate(templateNo)
}

// ---------- FUNC-43 克隆 ----------

// CloneClcTemplate FUNC-43 模板克隆，两个来源：
//  1. 既有模板（source_template_no）：改名复制头字段与两组子实体；
//  2. 既有集群（source_cluster_no）：版本引用取集群行、拓扑取在册节点（PENDING+ADDED）的角色分布聚合、
//     参数取集群当前参数行；命中凭据形态的值一律剔除为「须现场填」的空值并留注记
//     （INV-08：凭据值不进配方，与登记侧同一把尺子）。
//
// 克隆产物一律新登记一份草稿（不复用来源状态）；不接收外部文本上传（前置条件口径）。
func CloneClcTemplate(d map[string]any, user UserInfo) (map[string]any, error) {
	name := strings.TrimSpace(Str(d["name"]))
	srcTpl := strings.TrimSpace(Str(d["source_template_no"]))
	srcCluster := strings.TrimSpace(Str(d["source_cluster_no"]))
	if name == "" {
		return nil, Fail(errcode.ErrParam, "克隆必须给新模板名称（新登记一份配方，不是覆写来源；名称在未删除范围内唯一，INV-51）")
	}
	if (srcTpl == "") == (srcCluster == "") {
		return nil, Fail(errcode.ErrParam,
			"克隆来源必须且只能给一个：source_template_no（从既有模板）或 source_cluster_no（从既有集群）——两者都给或都不给都是歧义请求")
	}
	groups := []map[string]any{}
	params := []map[string]any{}
	versionRef, paramTemplateRef, sourceClusterRef := "", "", ""
	if srcTpl != "" {
		src, err := qOne(`SELECT * FROM clc_cluster_template WHERE template_no = ? AND flag = 1`, srcTpl)
		if err != nil {
			return nil, err
		}
		if src == nil {
			return nil, NotFound("来源模板（软删除行不能作为克隆来源）")
		}
		versionRef, paramTemplateRef = Str(src["version_ref"]), Str(src["param_template_ref"])
		gs, err := qList(`SELECT group_name, node_role, expected_count FROM clc_template_node_group
			WHERE template_ref = ? AND flag = 1 ORDER BY sort_no`, srcTpl)
		if err != nil {
			return nil, err
		}
		for _, g := range gs {
			groups = append(groups, map[string]any{
				"group_name": Str(g["group_name"]), "node_role": Str(g["node_role"]),
				"expected_count": g["expected_count"], "sort_no": 0,
			})
		}
		ps, err := qList(`SELECT param_key, param_value, description FROM clc_template_param
			WHERE template_ref = ? AND flag = 1 ORDER BY sort_no`, srcTpl)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			params = append(params, map[string]any{
				"param_key": Str(p["param_key"]), "param_value": Str(p["param_value"]),
				"description": Str(p["description"]), "sort_no": 0,
			})
		}
	} else {
		c, err := qOne(`SELECT cluster_no, name, version_ref FROM clc_cluster WHERE flag = 1 AND cluster_no = ?`, srcCluster)
		if err != nil {
			return nil, err
		}
		if c == nil {
			return nil, NotFound("来源集群")
		}
		sourceClusterRef = srcCluster
		versionRef = Str(c["version_ref"])
		rows, err := qList(`SELECT node_role, COUNT(*) AS cnt FROM clc_cluster_node
			WHERE cluster_ref = ? AND flag = 1 AND node_status IN (?, ?)
			GROUP BY node_role ORDER BY node_role`, srcCluster, NodeStatusPending, NodeStatusAdded)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, Fail(errcode.ErrState,
				"来源集群 %s 没有在册节点（PENDING/ADDED），克隆不出角色拓扑——请先登记节点或改用从模板克隆", srcCluster)
		}
		for _, r := range rows {
			role := strings.ToUpper(Str(r["node_role"]))
			groups = append(groups, map[string]any{
				"group_name": roleLabelForRole(role) + "组（克隆自集群）", "node_role": role,
				"expected_count": r["cnt"], "sort_no": 0,
			})
		}
		prs, err := qList(`SELECT param_key, param_value FROM clc_param
			WHERE cluster_ref = ? AND flag = 1 ORDER BY param_key`, srcCluster)
		if err != nil {
			return nil, err
		}
		for _, p := range prs {
			value := Str(p["param_value"])
			desc := "克隆自集群当前参数值"
			if clcTemplateHasCredentialShape(value) {
				value = ""
				desc = "来源值命中凭据形态，已按 INV-08 剔除；实例化时现场填（或改登存放位置标识）"
			}
			params = append(params, map[string]any{
				"param_key": Str(p["param_key"]), "param_value": value,
				"description": desc, "sort_no": 0,
			})
		}
	}
	normalizedGroups, err := clcNormalizeTemplateGroups(anySlice(groups))
	if err != nil {
		return nil, err
	}
	normalizedParams, err := clcNormalizeTemplateParams(anySlice(params))
	if err != nil {
		return nil, err
	}
	var no string
	err = tx(func(db *gorm.DB) error {
		dup, e := store.One(db, `SELECT id FROM clc_cluster_template WHERE flag = 1 AND LOWER(name) = LOWER(?)`, name)
		if e != nil {
			return e
		}
		if dup != nil {
			return bizRuleFail("INV-51", "模板名称「%s」在未删除范围内已存在（克隆是新登记一份配方，重名即拒绝）", name)
		}
		no, e = genBizNo(db, NoPrefixTemplate, "clc_cluster_template", "template_no")
		if e != nil {
			return e
		}
		if _, e := store.Exec(db, `INSERT INTO clc_cluster_template
			(template_no, name, description, version_ref, param_template_ref, template_status, is_default, source_cluster_ref, created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, 'DRAFT', FALSE, ?, ?, ?)`,
			no, name, nullableText(strings.TrimSpace(Str(d["description"]))),
			nullableText(versionRef), nullableText(paramTemplateRef), nullableText(sourceClusterRef),
			user.ID, user.ID); e != nil {
			return e
		}
		return clcWriteTemplateChildrenTx(db, no, normalizedGroups, normalizedParams, user)
	})
	if err != nil {
		return nil, err
	}
	srcLabel := "模板 " + srcTpl
	if srcCluster != "" {
		srcLabel = "集群 " + srcCluster
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-43", fmt.Sprintf(
		"克隆登记集群模板 %s（%s，来源：%s）：节点组 %d 行、参数行 %d 行（克隆产物一律草稿态）",
		no, name, srcLabel, len(normalizedGroups), len(normalizedParams)))
	return GetClcTemplate(no)
}

// roleLabelForRole 角色中文名（克隆组名用）。
func roleLabelForRole(role string) string {
	switch role {
	case NodeRoleMaster:
		return "主控"
	case NodeRoleWorker:
		return "工作"
	case NodeRoleInfra:
		return "基础设施"
	}
	return role
}

// anySlice map 切片 → []any（克隆复制行直接进归一器，同一把尺子再量一遍）。
func anySlice(rows []map[string]any) []any {
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	return out
}
