// opicdemo —— OPIC 功能示例服务（v2 架构：Gin + 基准 shared 层 + goose 迁移）。
//
// 由 v1 monolith（Hertz 单体）按 gopherforge 基准融合而来：客户申请审批流 +
// 流程引擎 + RBAC/工作台 + 本体注册表 + ZITADEL 双模式认证（基准 auth/identity
// 服务为平台 SSO 主承载，本服务的 zitadel 模式为演示面）。
package main

import (
	"database/sql"
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/api"
	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/config"
	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/dao"
	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/middleware"
	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/service"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/ontology"
)

func main() {
	config.Load()
	if !strings.EqualFold(config.Config.Auth.Mode, "development") && os.Getenv("APP_ENV") != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	// ① PostgreSQL 连接（GORM 连接池；schema 由 goose 迁移承载，禁 AutoMigrate）
	gdb, err := gorm.Open(postgres.Open(config.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("[opicdemo] 连接 PostgreSQL 失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		log.Fatalf("[opicdemo] 取 *sql.DB 失败: %v", err)
	}
	if config.Config.Database.MaxConns > 0 {
		sqlDB.SetMaxOpenConns(config.Config.Database.MaxConns)
	}
	if config.Config.Database.MinConns > 0 {
		sqlDB.SetMaxIdleConns(config.Config.Database.MinConns)
	}
	dao.Init(gdb)

	// ② goose 迁移
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("[opicdemo] goose dialect: %v", err)
	}
	migDir := os.Getenv("OPICDEMO_MIGRATIONS_DIR")
	if migDir == "" {
		migDir = "migrations"
	}
	if err := goose.Up(sqlDB, migDir); err != nil {
		log.Fatalf("[opicdemo] goose 迁移失败: %v", err)
	}

	// ③ 本体注册表（七模型 YAML）+ 种子
	ontology.SetModelsDir(config.Config.Ontology.ModelsDir)
	ontology.Load()
	if err := service.EnsureSeed(); err != nil {
		log.Fatalf("[opicdemo] 种子数据失败: %v", err)
	}

	// ④ HTTP 服务
	h := gin.New()
	h.Use(gin.Recovery(), middleware.CORS())
	api.InitApi(h)

	log.Printf("[opicdemo] listening :%s（事件总线/工作流运行时由平台 shared 层承载）", config.Config.App.Port)
	if err := h.Run(":" + config.Config.App.Port); err != nil {
		log.Fatalf("[opicdemo] HTTP 退出: %v", err)
	}
}

// sqlDBWrap 供 goose 使用（占位：goose 接受 *sql.DB）。
var _ = sql.DB{}
