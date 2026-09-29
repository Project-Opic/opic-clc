// Package service —— 业务服务层(与 Python techbase services/ 一一对应)。
package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/dao"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/errcode"
)

// CtxKey 上下文键。
type CtxKey string

const (
	CtxUserID   CtxKey = "user_id"
	CtxUsername CtxKey = "username"
)

// UserInfo 当前用户(经认证中间件注入)。
type UserInfo struct {
	ID       int64
	Username string
}

// FromCtx 从 hertz 上下文取当前用户。
func FromCtx(ctx context.Context) UserInfo {
	id, _ := ctx.Value(CtxUserID).(int64)
	name, _ := ctx.Value(CtxUsername).(string)
	return UserInfo{ID: id, Username: name}
}

// BizErr 业务错误(别名,统一由 pkg/errcode 承载)。
type BizErr = errcode.BizErr

// Fail 业务错误快捷构造。
func Fail(code errcode.ErrCode, format string, args ...any) *BizErr {
	return errcode.Fail(code, format, args...)
}

// AsBizErr 归一化错误。
func AsBizErr(err error) *BizErr {
	var be *BizErr
	if errors.As(err, &be) {
		return be
	}
	return &BizErr{Code: errcode.ErrAuthServer, Msg: err.Error()}
}

// ---------- 数据访问快捷方式(默认连接) ----------

func qList(sql string, args ...any) ([]map[string]any, error) {
	return dao.List(dao.DB, sql, args...)
}
func qOne(sql string, args ...any) (map[string]any, error) {
	return dao.One(dao.DB, sql, args...)
}
func qCount(sql string, args ...any) (int64, error) {
	return dao.Count(dao.DB, sql, args...)
}
func qExec(sql string, args ...any) (int64, error) {
	return dao.Exec(dao.DB, sql, args...)
}

// tx 事务包装(Python db.transaction 对应)。
func tx(fn func(tx *gorm.DB) error) error {
	return dao.DB.Transaction(fn)
}

// txList/txOne/txExec 事务内快捷方式。
func txList(tx *gorm.DB, sql string, args ...any) ([]map[string]any, error) {
	return dao.List(tx, sql, args...)
}
func txOne(tx *gorm.DB, sql string, args ...any) (map[string]any, error) {
	return dao.One(tx, sql, args...)
}
func txCount(tx *gorm.DB, sql string, args ...any) (int64, error) {
	return dao.Count(tx, sql, args...)
}
func txExec(tx *gorm.DB, sql string, args ...any) (int64, error) {
	return dao.Exec(tx, sql, args...)
}

// Page 分页结构(前端 PageResult 契约)。
type Page struct {
	List  []map[string]any `json:"list"`
	Total int64            `json:"total"`
	PageV int              `json:"page"`
	Size  int              `json:"size"`
}
