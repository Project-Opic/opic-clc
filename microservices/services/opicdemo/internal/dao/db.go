// Package dao —— opicdemo 数据访问（GORM 仅承担连接池与事务；业务查询用原生 SQL + map 行）。
// v1 的 internal/apiserver/store 以基准服务形态落位；schema 由 goose 迁移承载（禁 AutoMigrate）。
package dao

import (
	"gorm.io/gorm"
)

// DB 全局连接（main 装配后可用）。
var DB *gorm.DB

// Init 装配全局连接（连接建立由 main 经 shared/pkg/database 或 DSN 直连完成）。
func Init(db *gorm.DB) { DB = db }

// List 查询多行(map 行,列名为键)。db 为 nil 时使用默认连接。
func List(db *gorm.DB, sql string, args ...any) ([]map[string]any, error) {
	if db == nil {
		db = DB
	}
	var rows []map[string]any
	if err := db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return rows, nil
}

// One 查询单行(无结果返回 nil,不视为错误)。
func One(db *gorm.DB, sql string, args ...any) (map[string]any, error) {
	rows, err := List(db, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// Count 计数。
func Count(db *gorm.DB, sql string, args ...any) (int64, error) {
	if db == nil {
		db = DB
	}
	var n int64
	err := db.Raw(sql, args...).Scan(&n).Error
	return n, err
}

// Exec 执行写语句,返回影响行数。
func Exec(db *gorm.DB, sql string, args ...any) (int64, error) {
	if db == nil {
		db = DB
	}
	res := db.Exec(sql, args...)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// LastID 取最近插入的自增主键(PostgreSQL LASTVAL,须与 INSERT 同会话/事务)。
func LastID(db *gorm.DB) (int64, error) {
	var id int64
	err := db.Raw("SELECT LASTVAL()").Scan(&id).Error
	return id, err
}
