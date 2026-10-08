// Package meta —— 元数据控制器(字典/M3 规则;数据源为本体注册表)。
package meta

import (
	"strings"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// Dictionaries GET /api/meta/dictionaries 字典项(m1 data_dictionaries 全量)。
func Dictionaries(c *gin.Context) {
	reg := ontology.Load()
	c.JSON(200, errcode.OK(map[string]any{
		"CLC_CLUSTER_STATUS":     common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_CLUSTER_STATUS", "CLC_CLUSTER_STATUS")),
		"CLC_HEALTH":             common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_HEALTH", "CLC_HEALTH")),
		"CLC_DEGRADED_REASON":    common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_DEGRADED_REASON", "CLC_DEGRADED_REASON")),
		"CLC_NODE_STATUS":        common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_NODE_STATUS", "CLC_NODE_STATUS")),
		"CLC_NODE_HEALTH":        common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_NODE_HEALTH", "CLC_NODE_HEALTH")),
		"CLC_TASK_STATUS":        common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_TASK_STATUS", "CLC_TASK_STATUS")),
		"CLC_TASK_FAIL_REASON":   common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_TASK_FAIL_REASON", "CLC_TASK_FAIL_REASON")),
		"CLC_PROBE_RESULT":       common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_PROBE_RESULT", "CLC_PROBE_RESULT")),
		"CLC_RUN_MODE":           common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_RUN_MODE", "CLC_RUN_MODE")),
		"CLC_NODE_ROLE":          common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_NODE_ROLE", "CLC_NODE_ROLE")),
		"CLC_TARGET_CAPABILITY":  common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_TARGET_CAPABILITY", "CLC_TARGET_CAPABILITY")),
		"CLC_TARGET_KIND":        common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_TARGET_KIND", "CLC_TARGET_KIND")),
		"CLC_BINDING_PURPOSE":    common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_BINDING_PURPOSE", "CLC_BINDING_PURPOSE")),
		"CLC_BINDING_SOURCE":     common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_BINDING_SOURCE", "CLC_BINDING_SOURCE")),
		"CLC_PARAM_SOURCE":       common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_PARAM_SOURCE", "CLC_PARAM_SOURCE")),
		"CLC_PARAM_DERIVED_FROM": common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_PARAM_DERIVED_FROM", "CLC_PARAM_DERIVED_FROM")),
		"CLC_SNAPSHOT_TYPE":      common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_SNAPSHOT_TYPE", "CLC_SNAPSHOT_TYPE")),
		"CLC_TASK_KIND":          common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_TASK_KIND", "CLC_TASK_KIND")),
		"CLC_EXEC_PATH":          common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_EXEC_PATH", "CLC_EXEC_PATH")),
		"CLC_TRIGGER_SOURCE":     common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_TRIGGER_SOURCE", "CLC_TRIGGER_SOURCE")),
		// v1.2 易用性增补的五个字典：装机向导台与集群模板台的界面文案唯一事实源在 M1，
		// 只出目录（码值 + 中文名）、不出任何值与凭据；界面不留副本，字典改动即随接口生效。
		"CLC_STEP_MODE":      common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_STEP_MODE", "CLC_STEP_MODE")),
		"CLC_STEP_STATUS":    common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_STEP_STATUS", "CLC_STEP_STATUS")),
		"CLC_STEP_VERDICT":   common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_STEP_VERDICT", "CLC_STEP_VERDICT")),
		"CLC_LOG_LEVEL":      common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_LOG_LEVEL", "CLC_LOG_LEVEL")),
		"CLC_TEMPLATE_STATE": common.OrEmpty(reg.GetDictionaryItems("DICT-CLC_TEMPLATE_STATE", "CLC_TEMPLATE_STATE")),
	}))
}

// Rules GET /api/meta/rules M3 规则清单。
func Rules(c *gin.Context) {
	reg := ontology.Load()
	var out []map[string]any
	for _, r := range reg.Rules {
		out = append(out, map[string]any{
			"id":           r["id"],
			"name":         orDefaultStr(r["name"], r["id"]),
			"description":  r["description"],
			"expression":   strings.TrimSpace(service.Str(r["expression"])),
			"rule_type":    r["ruleType"],
			"input_params": common.OrEmpty(r["inputParams"]),
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	c.JSON(200, errcode.OK(out))
}

func orDefaultStr(v any, def any) any {
	if v == nil || service.Str(v) == "" {
		return def
	}
	return v
}
