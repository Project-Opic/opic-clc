// clc_params.go —— 平台参数(REF-01~12 规则级平台配置,FUNC-30 运行参数维护)。
package service

import (
	"fmt"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// platformParam 读平台参数(缺省回退 def)。
func platformParam(key, def string) string {
	row, err := qOne(`SELECT param_value FROM clc_platform_param WHERE param_key = ? AND flag = 1`, key)
	if err != nil || row == nil {
		return def
	}
	v := Str(row["param_value"])
	if v == "" {
		return def
	}
	return v
}

// ListPlatformParams FUNC-30 平台参数清单(运行观测页)。
func ListPlatformParams() ([]map[string]any, error) {
	return qList(`SELECT param_key, param_value, param_group, description, updated_at
		FROM clc_platform_param WHERE flag = 1 ORDER BY param_group ASC, param_key ASC`)
}

// SavePlatformParams FUNC-30 平台参数保存(逐键校验,审计留痕)。
func SavePlatformParams(items map[string]string, user UserInfo) ([]map[string]any, error) {
	if len(items) == 0 {
		return nil, Fail(errcode.ErrParam, "没有要保存的参数")
	}
	// 仅允许已登记键(防越权新增);值校验按参数语义
	known := map[string]func(string) error{
		"clc.legacy_mode_enabled": func(v string) error {
			if v != "true" && v != "false" {
				return bizRuleFail("REF-01", "legacy 模式开关只能取 true/false")
			}
			return nil
		},
		"clc.snapshot_retention_days": positiveInt("REF-02", "快照保留期"),
		"clc.probe_interval_seconds":  positiveInt("REF-03", "探活周期"),
		"clc.probe_fail_threshold":    positiveInt("REF-03", "连续失败阈值"),
		"clc.probe_recover_threshold": positiveInt("REF-11", "恢复健康连续成功阈值"),
		"clc.probe_retention_days":    positiveInt("REF-08", "探活记录保留期"),
		"clc.health_cache_seconds":    positiveInt("REF-07", "聚合健康缓存时长"),
		"clc.wizard_default_executor": func(string) error { return nil },
		"clc.wizard_default_version":  func(string) error { return nil },
		"clc.wizard_default_registry_instance": func(string) error { return nil },
	}
	err := tx(func(db *gorm.DB) error {
		for k, v := range items {
			validator, ok := known[k]
			if !ok {
				return Fail(errcode.ErrParam, "参数键「%s」未登记,禁止新增平台参数", k)
			}
			if err := validator(v); err != nil {
				return err
			}
			if _, err := store.Exec(db, `
				UPDATE clc_platform_param SET param_value=?, updated_by=?,
					updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
				WHERE param_key=? AND flag=1`, v, user.ID, k); err != nil {
				return err
			}
		}
		WriteAudit(db, user.ID, user.Username, "FUNC-30", fmt.Sprintf("保存运行参数 %d 项", len(items)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ListPlatformParams()
}
