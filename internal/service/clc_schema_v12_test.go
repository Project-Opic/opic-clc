// v1.2 增补 DDL(migrations/000007_clc_template.sql)的库侧不变量用例:
// 只钉「表/列存在 + 唯一性与缺省」这一层,服务层语义(FUNC-36~43 等)用例另见后续文件。
package service

import (
	"fmt"
	"strings"
	"testing"

	"gitcode.com/opic-ontology/opic-techbase/internal/store"
)

func ddlHasTable(t *testing.T, table string) bool {
	t.Helper()
	row := mustOne(t, `SELECT to_regclass(?) AS reg`, table)
	return row != nil && row["reg"] != nil
}

func ddlHasColumn(t *testing.T, table, column string) bool {
	t.Helper()
	n := countRows(t, `SELECT count(*) FROM information_schema.columns WHERE table_name=? AND column_name=?`, table, column)
	return n > 0
}

func expectDDLReject(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := store.DB.Exec(sql, args...).Error; err == nil {
		t.Fatalf("库侧约束未拦截,语句却成功: %s", sql)
	} else if !strings.Contains(err.Error(), "violates") && !strings.Contains(err.Error(), "constraint") {
		t.Fatalf("预期约束类错误,实得: %v", err)
	}
}

func seedTemplate(t *testing.T, name string, status string, isDefault bool) string {
	t.Helper()
	no := fmt.Sprintf("TPL-T%05d", nextSeq())
	mustSQL(t, `
		INSERT INTO clc_cluster_template (template_no, name, template_status, is_default)
		VALUES (?, ?, ?, ?)`, no, name, status, isDefault)
	return no
}

// 三张新表与集群/任务的新列都必须由 goose 迁移建出,服务层不得自行建表。
func TestClcV12SchemaObjects(t *testing.T) {
	requireDB(t)
	for _, tbl := range []string{"clc_cluster_template", "clc_template_node_group", "clc_template_param"} {
		if !ddlHasTable(t, tbl) {
			t.Errorf("表 %s 不存在(000007 未生效)", tbl)
		}
		for _, col := range []string{"created_by", "created_at", "updated_by", "updated_at", "flag"} {
			if !ddlHasColumn(t, tbl, col) {
				t.Errorf("表 %s 缺默认字段 %s", tbl, col)
			}
		}
	}
	if !ddlHasColumn(t, "clc_cluster", "template_ref") {
		t.Error("clc_cluster 缺列 template_ref")
	}
	if !ddlHasColumn(t, "clc_task", "step_mode") {
		t.Error("clc_task 缺列 step_mode")
	}
	if !ddlHasColumn(t, "clc_task", "operation_reason") {
		t.Error("clc_task 缺列 operation_reason")
	}
}

// REF-17:步进模式缺省按创建入口取,库侧缺省必须留在 AUTO —— 否则 v1.1 已验收的连跑入口
// (编排屏 / 审批回调 / 定时看护)会被新列静默改成单步等待。
func TestClcV12TaskStepModeDefaultsToAuto(t *testing.T) {
	requireDB(t)
	clusterNo := seedCluster(t, "RUNNING")
	taskNo := seedTask(t, clusterNo, "INSTALL", "QUEUED", "LOCAL")
	row := mustOne(t, `SELECT step_mode, operation_reason FROM clc_task WHERE task_no=?`, taskNo)
	if got := Str(row["step_mode"]); got != "AUTO" {
		t.Errorf("存量/新任务缺省步进模式应为 AUTO,实得 %q", got)
	}
	if row["operation_reason"] != nil {
		t.Errorf("事由应为空(仅跳步与高危派发必填),实得 %v", row["operation_reason"])
	}
}

// INV-51:名称在未删除范围内唯一(不区分大小写,与 INV-01 同口径);默认位全局至多一行为真。
func TestClcV12TemplateNameAndDefaultBit(t *testing.T) {
	requireDB(t)
	t.Cleanup(func() { mustSQL(t, `DELETE FROM clc_cluster_template WHERE template_no LIKE 'TPL-T%'`) })
	first := seedTemplate(t, "Calico 三相配方", "ENABLED", true)
	expectDDLReject(t, `INSERT INTO clc_cluster_template (template_no, name, template_status) VALUES (?, ?, 'DRAFT')`,
		fmt.Sprintf("TPL-T%05d", nextSeq()), "Calico 三相配方")
	// 不区分大小写:唯一索引建在 LOWER(name) 上
	expectDDLReject(t, `INSERT INTO clc_cluster_template (template_no, name, template_status) VALUES (?, ?, 'DRAFT')`,
		fmt.Sprintf("TPL-T%05d", nextSeq()), "CALICO 三相配方")
	// 默认位已被占用:第二行为真必被拒
	expectDDLReject(t, `INSERT INTO clc_cluster_template (template_no, name, template_status, is_default) VALUES (?, ?, 'ENABLED', true)`,
		fmt.Sprintf("TPL-T%05d", nextSeq()), "另一配方")
	// 撤销原默认后新项可置真(FUNC-41 的「原子撤旧置新」在库侧只要求终态成立)
	mustSQL(t, `UPDATE clc_cluster_template SET is_default=false WHERE template_no=?`, first)
	mustSQL(t, `INSERT INTO clc_cluster_template (template_no, name, template_status, is_default) VALUES (?, ?, 'ENABLED', true)`,
		fmt.Sprintf("TPL-T%05d", nextSeq()), "又一配方")
	// 软删行的名称可被复用(与 INV-01 同口径:未删除范围)
	mustSQL(t, `UPDATE clc_cluster_template SET flag=0 WHERE template_no=?`, first)
	mustSQL(t, `INSERT INTO clc_cluster_template (template_no, name, template_status) VALUES (?, ?, 'DRAFT')`,
		fmt.Sprintf("TPL-T%05d", nextSeq()), "Calico 三相配方")
}

// INV-52:两组子实体整组读写 —— 组名/同角色/参数键在模板内各至多一行;期望数量 ≥1。
func TestClcV12TemplateChildRowConstraints(t *testing.T) {
	requireDB(t)
	t.Cleanup(func() {
		mustSQL(t, `DELETE FROM clc_template_param WHERE template_ref LIKE 'TPL-T%'`)
		mustSQL(t, `DELETE FROM clc_template_node_group WHERE template_ref LIKE 'TPL-T%'`)
		mustSQL(t, `DELETE FROM clc_cluster_template WHERE template_no LIKE 'TPL-T%'`)
	})
	tpl := seedTemplate(t, "节点组校验配方", "DRAFT", false)
	mustSQL(t, `INSERT INTO clc_template_node_group (template_ref, group_name, node_role, expected_count, sort_no) VALUES (?, ?, 'MASTER', 3, 10)`,
		tpl, "主控组")
	expectDDLReject(t, `INSERT INTO clc_template_node_group (template_ref, group_name, node_role, expected_count, sort_no) VALUES (?, ?, 'WORKER', 1, 20)`,
		tpl, "主控组")
	expectDDLReject(t, `INSERT INTO clc_template_node_group (template_ref, group_name, node_role, expected_count, sort_no) VALUES (?, ?, 'MASTER', 1, 30)`,
		tpl, "另一个主控组")
	expectDDLReject(t, `INSERT INTO clc_template_node_group (template_ref, group_name, node_role, expected_count, sort_no) VALUES (?, ?, 'INFRA', 0, 40)`,
		tpl, "零数量组")
	// 不同模板可同名(约束作用域是模板内)
	other := seedTemplate(t, "另一台配方", "DRAFT", false)
	mustSQL(t, `INSERT INTO clc_template_node_group (template_ref, group_name, node_role, expected_count, sort_no) VALUES (?, ?, 'MASTER', 1, 10)`,
		other, "主控组")

	mustSQL(t, `INSERT INTO clc_template_param (template_ref, param_key, param_value, sort_no) VALUES (?, 'clc.network.mode', 'calico', 10)`, tpl)
	// 空串是合法默认值(表示该项须现场填),不得被 NOT NULL 挡下
	mustSQL(t, `INSERT INTO clc_template_param (template_ref, param_key, param_value, sort_no) VALUES (?, 'clc.network.cidr', '', 20)`, tpl)
	expectDDLReject(t, `INSERT INTO clc_template_param (template_ref, param_key, param_value, sort_no) VALUES (?, 'clc.network.mode', 'cilium', 30)`, tpl)
}
