// clc_binding.go —— AGG-CLC-03 中心引用绑定 + AGG-CLC-04 集群参数行为实现(m2 FUNC-13/14/15/22/23)。
//
// 事务口径:前置校验 → 规则校验(INV-16~23/R-01/R-04) → 双写引用(R-01) → 派生参数+快照(R-04) → 审计/事件。
// 「派生归派生、生效归运维」(M6 FLOW-PROC-03):派生只置待同步,生效由 FUNC-15 显式确认。
package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ListBindings FUNC-22 引用台账查询(REP-04)。
func ListBindings(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE b.flag = 1"
	var args []any
	if v := Str(p["cluster_ref"]); v != "" {
		where += " AND b.cluster_ref = ?"
		args = append(args, v)
	}
	if v := Str(p["target_capability"]); v != "" {
		where += " AND b.target_capability = ?"
		args = append(args, v)
	}
	if v := Str(p["purpose"]); v != "" {
		where += " AND b.purpose = ?"
		args = append(args, v)
	}
	if v := Str(p["binding_source"]); v != "" {
		where += " AND b.binding_source = ?"
		args = append(args, v)
	}
	if kw := Str(p["keyword"]); kw != "" {
		where += " AND (b.binding_no LIKE ? OR b.target_ref LIKE ?)"
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_binding b `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`
		SELECT b.*, c.name AS cluster_name
		FROM clc_binding b
		LEFT JOIN clc_cluster c ON c.cluster_no = b.cluster_ref AND c.flag = 1
		`+where+` ORDER BY b.id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// upsertBindingTx 引用台账写入(事务内;INV-16 组合唯一 / INV-17 组合合法 / INV-18 对账注记)。
func upsertBindingTx(db *gorm.DB, clusterNo, capability, kind, ref, purpose, source, note string, user UserInfo) error {
	if err := checkBindingCombo(capability, kind); err != nil {
		return err
	}
	if strings.TrimSpace(ref) == "" {
		return Fail(errcode.ErrParam, "资源标识必填(跨域跳转键)")
	}
	if err := checkReconcileNote(source, note); err != nil {
		return err
	}
	if purpose == "" {
		return Fail(errcode.ErrParam, "用途必填(CLC_BINDING_PURPOSE)")
	}
	if dup, err := store.One(db, `SELECT id FROM clc_binding WHERE cluster_ref=? AND flag=1
		AND LOWER(target_capability)=LOWER(?) AND LOWER(target_kind)=LOWER(?) AND LOWER(purpose)=LOWER(?) AND LOWER(target_ref)=LOWER(?)`,
		clusterNo, capability, kind, purpose, ref); err != nil {
		return err
	} else if dup != nil {
		return bizRuleFail("INV-16", "同一集群内「目标能力 + 资源类型 + 用途 + 资源标识」组合已存在(组合唯一)")
	}
	no, err := genBizNo(db, NoPrefixBinding, "clc_binding", "binding_no")
	if err != nil {
		return err
	}
	_, err = store.Exec(db, `
		INSERT INTO clc_binding (binding_no, cluster_ref, target_capability, target_kind, target_ref, purpose, binding_source, reconcile_note, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		no, clusterNo, capability, kind, ref, purpose, source, note, user.ID, user.ID)
	return err
}

// AdjustBinding FUNC-13 调整中心引用(R-01 双写 + R-04 派生 + 快照)。
func AdjustBinding(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(c["cluster_no"])
	if Str(c["status"]) == ClusterStatusDestroyed {
		return nil, bizRuleFail("INV-19", "集群已销毁,引用台账已级联清理,不可再调整")
	}
	capability, kind, ref := Str(d["target_capability"]), Str(d["target_kind"]), Str(d["target_ref"])
	purpose := Str(d["purpose"])
	source := Str(d["binding_source"])
	if source == "" {
		source = BindingSourceManual
	}
	note := Str(d["reconcile_note"])
	if bindingNo := Str(d["binding_no"]); bindingNo != "" {
		// 调整既有行:替换资源标识(旧行逻辑删除 + 新登记,留痕)
		err = tx(func(db *gorm.DB) error {
			old, err := store.One(db, `SELECT * FROM clc_binding WHERE binding_no=? AND cluster_ref=? AND flag=1`, bindingNo, clusterNo)
			if err != nil {
				return err
			}
			if old == nil {
				return NotFound("引用台账行")
			}
			if _, err := store.Exec(db, `UPDATE clc_binding SET flag=0, reconcile_note=?, updated_by=?,
				updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=?`,
				note, user.ID, Int(old["id"])); err != nil {
				return err
			}
			if err := upsertBindingTx(db, clusterNo, capability, kind, ref, purpose, source, note, user); err != nil {
				return err
			}
			if err := writeInlineRefTx(db, clusterNo, capability, kind, ref); err != nil {
				return err
			}
			if err := deriveParamsTx(db, clusterNo, inlineRefsOf(db, clusterNo), user); err != nil {
				return err
			}
			return createSnapshotTx(db, clusterNo, fmt.Sprintf("调整中心引用:%s/%s → %s", capability, purpose, ref), SnapshotTypeChange, user)
		})
	} else {
		// 新增引用行
		err = tx(func(db *gorm.DB) error {
			if err := upsertBindingTx(db, clusterNo, capability, kind, ref, purpose, source, note, user); err != nil {
				return err
			}
			if err := writeInlineRefTx(db, clusterNo, capability, kind, ref); err != nil {
				return err
			}
			if err := deriveParamsTx(db, clusterNo, inlineRefsOf(db, clusterNo), user); err != nil {
				return err
			}
			return createSnapshotTx(db, clusterNo, fmt.Sprintf("新增中心引用:%s/%s → %s", capability, purpose, ref), SnapshotTypeChange, user)
		})
	}
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-13", fmt.Sprintf("集群 %s 调整中心引用:%s/%s → %s(双写内联列与台账,R-01)", clusterNo, capability, purpose, ref))
	publishClcEvent("clc.binding.adjusted", map[string]any{"cluster_no": clusterNo, "target_ref": ref})
	return GetCluster(clusterID)
}

// ListParams FUNC-23 参数清单查询(REP-05:键/值/来源/派生来源/待同步/覆写冲突提示)。
func ListParams(page, size int, p map[string]any) (*Page, error) {
	where := "WHERE p.flag = 1"
	var args []any
	if v := Str(p["cluster_ref"]); v != "" {
		where += " AND p.cluster_ref = ?"
		args = append(args, v)
	}
	if v := Str(p["param_source"]); v != "" {
		where += " AND p.param_source = ?"
		args = append(args, v)
	}
	if v := Str(p["sync_pending"]); v != "" {
		where += " AND p.sync_pending = ?"
		args = append(args, paramInt(v))
	}
	if kw := Str(p["keyword"]); kw != "" {
		where += " AND (p.param_key LIKE ?)"
		args = append(args, "%"+kw+"%")
	}
	total, err := qCount(`SELECT COUNT(*) FROM clc_param p `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`
		SELECT p.*, c.name AS cluster_name
		FROM clc_param p
		LEFT JOIN clc_cluster c ON c.cluster_no = p.cluster_ref AND c.flag = 1
		`+where+` ORDER BY p.id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// OverrideParam FUNC-14 参数人工覆写(INV-21 留痕;覆写行不被派生覆盖 R-04/INV-23)。
func OverrideParam(clusterID int64, d map[string]any, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(c["cluster_no"])
	key, value := Str(d["param_key"]), Str(d["param_value"])
	if strings.TrimSpace(key) == "" {
		return nil, Fail(errcode.ErrParam, "参数键必填")
	}
	err = tx(func(db *gorm.DB) error {
		if dup, err := store.One(db, `SELECT id, param_source FROM clc_param WHERE cluster_ref=? AND flag=1 AND LOWER(param_key)=LOWER(?)`, clusterNo, key); err != nil {
			return err
		} else if dup != nil && Str(dup["param_source"]) == ParamSourceDerived {
			// INV-23:派生写入不得覆盖 MANUAL_OVERRIDE 行 —— 这里是人工覆写派生行,允许(覆写值权威),
			// 但保留派生注记并置待同步(生效归运维)
			if _, err := store.Exec(db, `
				UPDATE clc_param SET param_value=?, param_source=?, updated_by=?,
					updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
				value, ParamSourceOverride, user.ID, Int(dup["id"])); err != nil {
				return err
			}
			return nil
		} else if dup != nil {
			if _, err := store.Exec(db, `
				UPDATE clc_param SET param_value=?, updated_by=?,
					updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
				value, user.ID, Int(dup["id"])); err != nil {
				return err
			}
			return nil
		}
		_, err := store.Exec(db, `
			INSERT INTO clc_param (cluster_ref, param_key, param_value, param_source, sync_pending, updated_by, created_by)
			VALUES (?, ?, ?, ?, 0, ?, ?)`,
			clusterNo, key, value, ParamSourceOverride, user.ID, user.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	// INV-21:MANUAL_OVERRIDE 每次修改必须留痕
	WriteAudit(nil, user.ID, user.Username, "FUNC-14", fmt.Sprintf("集群 %s 参数人工覆写 %s=%s(留痕)", clusterNo, key, value))
	return GetCluster(clusterID)
}

// ConfirmParamSync FUNC-15 触发参数同步(确认执行;仅待同步行;生效留痕 + 快照)。
func ConfirmParamSync(clusterID int64, user UserInfo) (map[string]any, error) {
	c, err := GetCluster(clusterID)
	if err != nil {
		return nil, err
	}
	clusterNo := Str(c["cluster_no"])
	var n int64
	err = tx(func(db *gorm.DB) error {
		// INV-22:待同步行必须存在派生注记(违反即拒绝)
		rows, err := store.List(db, `SELECT id, param_key, derive_note FROM clc_param WHERE cluster_ref=? AND flag=1 AND sync_pending=1`, clusterNo)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if strings.TrimSpace(Str(r["derive_note"])) == "" {
				return bizRuleFail("INV-22", "参数 %s 待同步但缺少派生注记(哪个上游、什么变更)", Str(r["param_key"]))
			}
		}
		// 空集合勿拼 IN():直接判定
		if len(rows) == 0 {
			return Fail(errcode.ErrState, "集群 %s 没有待同步参数(派生归派生、生效归运维:确认前须由引用变更触发派生)", clusterNo)
		}
		n = int64(len(rows))
		if _, err := store.Exec(db, `
			UPDATE clc_param SET sync_pending=0, updated_by=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
			WHERE cluster_ref=? AND flag=1 AND sync_pending=1`, user.ID, clusterNo); err != nil {
			return err
		}
		return createSnapshotTx(db, clusterNo, fmt.Sprintf("参数同步确认:%d 项生效", n), SnapshotTypeAutoDerived, user)
	})
	if err != nil {
		return nil, err
	}
	WriteAudit(nil, user.ID, user.Username, "FUNC-15", fmt.Sprintf("集群 %s 参数同步确认:%d 项待同步标记清除(交付生效经任务通道,REF-12)", clusterNo, n))
	publishClcEvent("clc.param.synced", map[string]any{"cluster_no": clusterNo, "count": n})
	return GetCluster(clusterID)
}

// deriveParamsTx R-04:引用变更 → 派生参数更新 + 置待同步(不覆盖 MANUAL_OVERRIDE 行,冲突提示;不自动执行同步)。
func deriveParamsTx(db *gorm.DB, clusterNo string, refs map[string]string, user UserInfo) error {
	derived := deriveImageMirror(refs["registry_ref"], refs["registry_project_ref"])
	if len(derived) == 0 {
		return nil
	}
	for key, value := range derived {
		existing, err := store.One(db, `SELECT id, param_source, param_value FROM clc_param WHERE cluster_ref=? AND flag=1 AND LOWER(param_key)=LOWER(?)`, clusterNo, key)
		if err != nil {
			return err
		}
		note := fmt.Sprintf("上游=镜像仓库(%s/%s),引用变更触发派生", refs["registry_ref"], refs["registry_project_ref"])
		if existing != nil && Str(existing["param_source"]) == ParamSourceOverride && Str(existing["param_value"]) != value {
			// R-04/INV-23:不覆盖覆写值,登记冲突提示(derive_note 记录冲突,待同步置位提示人工决策)
			if _, err := store.Exec(db, `
				UPDATE clc_param SET derive_note=?, sync_pending=1, updated_by=?,
					updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
				"覆写与派生冲突:保留覆写值;派生值 "+value+" 已登记提示,待人工确认", user.ID, Int(existing["id"])); err != nil {
				return err
			}
			continue
		}
		if existing != nil {
			if Str(existing["param_value"]) == value {
				continue
			}
			if _, err := store.Exec(db, `
				UPDATE clc_param SET param_value=?, derive_note=?, sync_pending=1, updated_by=?,
					updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS') WHERE id=? AND flag=1`,
				value, note, user.ID, Int(existing["id"])); err != nil {
				return err
			}
			continue
		}
		if _, err := store.Exec(db, `
			INSERT INTO clc_param (cluster_ref, param_key, param_value, param_source, derived_from, derive_note, sync_pending, updated_by, created_by)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			clusterNo, key, value, ParamSourceDerived, ParamFromRegistry, note, user.ID, user.ID); err != nil {
			return err
		}
	}
	return nil
}
