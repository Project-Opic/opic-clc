// Package config —— opicdemo 12-factor 配置（仅环境变量，前缀 OPICDEMO_；基准 config 风格）。
// 字段形态与 v1 monolith config 对齐，移植的服务代码以 config.Config.X.Y 直取。
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 全局配置结构（字段与 v1 config.yml 形态对齐）。
var Config struct {
	App struct {
		Name string
		Port string
	}
	Log struct {
		Level string
	}
	Database struct {
		Host     string
		Port     int
		UserName string
		Password string
		DBName   string
		SSLMode  string
		MinConns int
		MaxConns int
		DSN      string // OPICDEMO_DSN 直连串优先
	}
	Auth struct {
		Mode             string // local | zitadel
		JWTSecret        string
		JWTExpiresHours  int
		AllowLocalLogin  bool
		Issuer           string
		ClientID         string
		ClientSecret     string
		RedirectURL      string
		RoleClaim        string
		AutoProvision    bool
		DefaultRoleCodes string
	}
	Ontology struct {
		ModelsDir string
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv("OPICDEMO_" + key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv("OPICDEMO_" + key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv("OPICDEMO_" + key)); v != "" {
		return strings.EqualFold(v, "true") || v == "1"
	}
	return def
}

// Load 装配全局配置（幂等）。
func Load() {
	Config.App.Name = env("APP_NAME", "opicdemo")
	Config.App.Port = env("APP_PORT", "9700")
	Config.Log.Level = env("LOG_LEVEL", "info")
	Config.Database.Host = env("DB_HOST", "127.0.0.1")
	Config.Database.Port = envInt("DB_PORT", 5432)
	Config.Database.UserName = env("DB_USER", "postgres")
	Config.Database.Password = env("DB_PASSWORD", "postgres")
	Config.Database.DBName = env("DB_NAME", "opicdemo")
	Config.Database.SSLMode = env("DB_SSLMODE", "disable")
	Config.Database.MinConns = envInt("DB_MINCONNS", 5)
	Config.Database.MaxConns = envInt("DB_MAXCONNS", 20)
	Config.Database.DSN = env("DSN", "")
	Config.Auth.Mode = env("AUTH_MODE", "local")
	Config.Auth.JWTSecret = env("AUTH_JWT_SECRET", "opicdemo-dev-secret")
	Config.Auth.JWTExpiresHours = envInt("AUTH_JWT_EXPIRES_HOURS", 12)
	Config.Auth.AllowLocalLogin = envBool("AUTH_ALLOW_LOCAL_LOGIN", true)
	Config.Auth.Issuer = env("AUTH_ISSUER", "http://localhost:8080")
	Config.Auth.ClientID = env("AUTH_CLIENT_ID", "opicdemo")
	Config.Auth.ClientSecret = env("AUTH_CLIENT_SECRET", "")
	Config.Auth.RedirectURL = env("AUTH_REDIRECT_URL", "http://localhost:9700/api/auth/callback")
	Config.Auth.RoleClaim = env("AUTH_ROLE_CLAIM", "roles")
	Config.Auth.AutoProvision = envBool("AUTH_AUTO_PROVISION", true)
	Config.Auth.DefaultRoleCodes = env("AUTH_DEFAULT_ROLE_CODES", "SALES")
	Config.Ontology.ModelsDir = env("ONTOLOGY_MODELS_DIR", "models")
}

// DSN 组装 Postgres 连接串（空密码省略键——pgx 关键字解析约束）。
func DSN() string {
	if Config.Database.DSN != "" {
		return Config.Database.DSN
	}
	base := "host=%s port=%d user=%s dbname=%s sslmode=%s"
	args := []any{Config.Database.Host, Config.Database.Port, Config.Database.UserName, Config.Database.DBName, Config.Database.SSLMode}
	if Config.Database.Password != "" {
		base = "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s"
		args = []any{Config.Database.Host, Config.Database.Port, Config.Database.UserName, Config.Database.Password, Config.Database.DBName, Config.Database.SSLMode}
	}
	return fmtSprintf(base, args...)
}

func fmtSprintf(format string, args ...any) string {
	out := format
	for i, a := range args {
		var s string
		switch x := a.(type) {
		case string:
			s = x
		case int:
			s = strconv.Itoa(x)
		}
		out = replaceOnce(out, "%s", s, i)
	}
	return out
}

func replaceOnce(s, old, new string, _ int) string {
	idx := strings.Index(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}
