// Package config —— 全局配置(hertz-admin internal/pkg/config 风格:结构体 + yaml + 默认值)。
package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	} `yaml:"frontend"`
	MQ struct {
		// Enabled false 时使用 Noop 发布器(如实降级,不报错)
		Enabled       bool   `yaml:"enabled"`
		URL           string `yaml:"url"`            // NATS 地址,如 nats://127.0.0.1:4222
		SubjectPrefix string `yaml:"subject_prefix"` // 固定 opic(opic.<source>.<type>)
		JetStream     bool   `yaml:"jetstream"`      // JetStream 持久化(事件回放/持久订阅)
		TimeoutSec    int    `yaml:"timeout_sec"`
	} `yaml:"mq"`
	Workflow struct {
		// Enabled false 时使用 Noop Runner(如实降级)
		Enabled   bool   `yaml:"enabled"`
		Host      string `yaml:"host"`      // Temporal Server,如 localhost:7233
		Namespace string `yaml:"namespace"`
		TaskQueue string `yaml:"task_queue"` // opic.<中心名>,每中心一个队列
	} `yaml:"workflow"`
}

var once sync.Once

// Init 装配配置(幂等)。path 为空则依次找 ./configs/config.yml、../configs/config.yml。
func Init(path string) {
	once.Do(func() {
		if path == "" {
			for _, p := range []string{"configs/config.yml", "../configs/config.yml", "go-techbase/configs/config.yml"} {
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
			if abs, err := filepath.Abs(path); err == nil {
				// 相对路径以配置文件所在目录为基准
				_ = abs
			}
		}
		if Config.Ontology.ModelsDir != "" {
			Config.Ontology.ModelsDir = resolveDir(Config.Ontology.ModelsDir)
		}
		if Config.Frontend.DistDir != "" {
			Config.Frontend.DistDir = resolveDir(Config.Frontend.DistDir)
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
		Config.App.Name = "go-techbase"
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
