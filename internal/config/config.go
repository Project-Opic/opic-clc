// Package config —— 全局配置(结构体 + yaml 默认 + 环境变量覆盖,对齐 gopherforge 配置模式)。
//
// 优先级:环境变量 > configs/config.yml > 内置默认值。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config 全局配置结构。
var Config struct {
	App struct {
		Name string `yaml:"name"`
		Port string `yaml:"port"`
	} `yaml:"app"`
	Log struct {
		Level  string `yaml:"level"`
		Output string `yaml:"output"` // std|file
		Dir    string `yaml:"dir"`
	} `yaml:"log"`
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		UserName string `yaml:"username"`
		Password string `yaml:"password"`
		DBName   string `yaml:"dbname"`
		SSLMode  string `yaml:"sslmode"`
		MinConns int    `yaml:"minconns"`
		MaxConns int    `yaml:"maxconns"`
	} `yaml:"database"`
	Redis struct {
		Enabled  bool   `yaml:"enabled"`
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`
	Metrics struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"metrics"`
	Auth struct {
		// Mode: local(用户名密码,HS256 会话) | zitadel(OIDC Authorization Code + PKCE,JWKS 验签)
		Mode             string `yaml:"mode"`
		JWTSecret        string `yaml:"jwt_secret"`
		JWTExpiresHours  int    `yaml:"jwt_expires_hours"`
		AllowLocalLogin  bool   `yaml:"allow_local_login"` // zitadel 模式下是否保留本地账号登录
		Issuer           string `yaml:"issuer"`            // ZITADEL issuer,如 http://zitadel:8080
		ClientID         string `yaml:"client_id"`
		ClientSecret     string `yaml:"client_secret"`
		RedirectURL      string `yaml:"redirect_url"`       // /api/auth/callback 的对外完整地址
		RoleClaim        string `yaml:"role_claim"`         // userinfo 中角色声明名(如 roles)
		AutoProvision    bool   `yaml:"auto_provision"`     // 按 userinfo 自动建用户并映射角色
		DefaultRoleCodes string `yaml:"default_role_codes"` // 自动 provisioning 时授予的本地角色编码,逗号分隔
	} `yaml:"auth"`
	Ontology struct {
		// ModelsDir 七模型 YAML 目录(默认随底座的 models/,可指向应用的 models/)
		ModelsDir string `yaml:"models_dir"`
	} `yaml:"ontology"`
	Frontend struct {
		// DistDir 前端构建产物目录(空则不托管)
		DistDir string `yaml:"dist_dir"`
		// BaseURL SSO 回调后 302 回前端的绝对基址(空则相对路径——仅前后端同域可用)
		BaseURL string `yaml:"base_url"`
	} `yaml:"frontend"`
}

var once sync.Once

// Init 装配配置(幂等)。path 为空则依次找 ./configs/config.yml、../configs/config.yml。
func Init(path string) {
	once.Do(func() {
		if path == "" {
			for _, p := range []string{"configs/config.yml", "../configs/config.yml"} {
				if _, err := os.Stat(p); err == nil {
					path = p
					break
				}
			}
		}
		applyDefaults()
		if path != "" {
			raw, err := os.ReadFile(path)
			if err != nil {
				panic(fmt.Sprintf("读取配置失败 %s: %v", path, err))
			}
			if err := yaml.Unmarshal(raw, &Config); err != nil {
				panic(fmt.Sprintf("解析配置失败 %s: %v", path, err))
			}
		}
		applyEnvOverrides()
		if Config.Ontology.ModelsDir != "" {
			Config.Ontology.ModelsDir = resolveDir(Config.Ontology.ModelsDir)
		}
		if Config.Frontend.DistDir != "" {
			Config.Frontend.DistDir = resolveDir(Config.Frontend.DistDir)
		}
		if Config.Log.Dir != "" && !filepath.IsAbs(Config.Log.Dir) {
			if abs, err := filepath.Abs(Config.Log.Dir); err == nil {
				Config.Log.Dir = abs
			}
		}
	})
}

func resolveDir(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	for _, base := range []string{".", "..", "../.."} {
		if _, err := os.Stat(filepath.Join(base, p)); err == nil {
			abs, _ := filepath.Abs(filepath.Join(base, p))
			return abs
		}
	}
	return p
}

func applyDefaults() {
	if Config.App.Name == "" {
		Config.App.Name = "opic-techbase"
	}
	if Config.App.Port == "" {
		Config.App.Port = "9680"
	}
	if Config.Log.Level == "" {
		Config.Log.Level = "info"
	}
	if Config.Log.Output == "" {
		Config.Log.Output = "std"
	}
	if Config.Log.Dir == "" {
		Config.Log.Dir = "./log"
	}
	if Config.Database.Host == "" {
		Config.Database.Host = "localhost"
	}
	if Config.Database.Port == 0 {
		Config.Database.Port = 5432
	}
	if Config.Database.DBName == "" {
		Config.Database.DBName = "go_techbase"
	}
	if Config.Database.SSLMode == "" {
		Config.Database.SSLMode = "disable"
	}
	if Config.Database.MaxConns == 0 {
		Config.Database.MaxConns = 20
	}
	if Config.Redis.Host == "" {
		Config.Redis.Host = "localhost"
	}
	if Config.Redis.Port == 0 {
		Config.Redis.Port = 6379
	}
	// 指标默认开启(yaml 显式 false 或 env METRICS_ENABLED=false 关闭)
	Config.Metrics.Enabled = true
	if Config.Auth.Mode == "" {
		Config.Auth.Mode = "local"
	}
	if Config.Auth.JWTSecret == "" {
		Config.Auth.JWTSecret = "go-techbase-dev-secret"
	}
	if Config.Auth.JWTExpiresHours == 0 {
		Config.Auth.JWTExpiresHours = 12
	}
	if Config.Auth.RoleClaim == "" {
		Config.Auth.RoleClaim = "roles"
	}
	if Config.Auth.DefaultRoleCodes == "" {
		Config.Auth.DefaultRoleCodes = "SALES"
	}
	if Config.Ontology.ModelsDir == "" {
		Config.Ontology.ModelsDir = "models"
	}
}

// applyEnvOverrides 环境变量覆盖(gopherforge 12-factor 风格;容器部署免改 yaml)。
func applyEnvOverrides() {
	if v := os.Getenv("APP_PORT"); v != "" {
		Config.App.Port = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		Config.Log.Level = v
	}
	if v := os.Getenv("DB_HOST"); v != "" {
		Config.Database.Host = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			Config.Database.Port = n
		}
	}
	if v := os.Getenv("DB_USER"); v != "" {
		Config.Database.UserName = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		Config.Database.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		Config.Database.DBName = v
	}
	if v := os.Getenv("DB_SSLMODE"); v != "" {
		Config.Database.SSLMode = v
	}
	if v := os.Getenv("REDIS_ENABLED"); v != "" {
		Config.Redis.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("REDIS_HOST"); v != "" {
		Config.Redis.Host = v
	}
	if v := os.Getenv("REDIS_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			Config.Redis.Port = n
		}
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		Config.Redis.Password = v
	}
	if v := os.Getenv("METRICS_ENABLED"); v != "" {
		Config.Metrics.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		Config.Auth.JWTSecret = v
	}
	if v := os.Getenv("AUTH_MODE"); v != "" {
		Config.Auth.Mode = v
	}
	if v := os.Getenv("ZITADEL_ISSUER"); v != "" {
		Config.Auth.Issuer = v
	}
	if v := os.Getenv("ZITADEL_CLIENT_ID"); v != "" {
		Config.Auth.ClientID = v
	}
	if v := os.Getenv("ZITADEL_CLIENT_SECRET"); v != "" {
		Config.Auth.ClientSecret = v
	}
	if v := os.Getenv("ZITADEL_REDIRECT_URL"); v != "" {
		Config.Auth.RedirectURL = v
	}
}
