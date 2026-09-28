// Package apiserver —— hertz 服务装配(hertz-admin internal/apiserver 风格)。
package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/middlewares/server/recovery"
	"github.com/cloudwego/hertz/pkg/app/server"

	v1 "github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/apiserver/router/v1"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/pkg/config"
	"github.com/sharptoolbox/ontology-driven-dev/go-techbase/internal/pkg/middleware"
)

// HttpStart 启动 HTTP 服务(前端静态托管 + /api)。
func HttpStart() {
	h := server.Default(
		server.WithHostPorts(fmt.Sprintf(":%s", config.Config.App.Port)),
	)
	h.Use(recovery.Recovery())
	h.Use(middleware.CORS())
	h.Use(requestLogger)

	{
		v1.InitApi(h.Engine) // /api 业务路由
	}

	// 前端静态托管(dist 存在时;与 Python 版 app.py 行为一致)
	dist := config.Config.Frontend.DistDir
	if dist != "" && dirExists(dist) {
		h.NoRoute(func(ctx context.Context, c *app.RequestContext) {
			path := string(c.Path())
			// API 未命中走 404
			if strings.HasPrefix(path, "/api/") {
				c.JSON(http.StatusNotFound, map[string]any{
					"returnInfo": map[string]string{"returnCode": "SYS1002", "errorMsg": "接口不存在"},
					"data":       nil,
				})
				return
			}
			// 静态文件;SPA 回退 index.html
			clean := filepath.Clean(strings.TrimPrefix(path, "/"))
			target := filepath.Join(dist, clean)
			if !strings.HasPrefix(target, dist) {
				c.String(http.StatusForbidden, "forbidden")
				return
			}
			if st, err := os.Stat(target); err == nil && !st.IsDir() {
				c.File(target)
				return
			}
			c.File(filepath.Join(dist, "index.html"))
		})
	}

	fmt.Println("============================================================")
	fmt.Printf("  go-techbase 已启动: http://127.0.0.1:%s\n", config.Config.App.Port)
	fmt.Printf("  认证模式: %s   领域: %s\n", config.Config.Auth.Mode, config.Config.App.Name)
	fmt.Println("============================================================")
	h.Spin()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// requestLogger 简易访问日志。
func requestLogger(ctx context.Context, c *app.RequestContext) {
	start := time.Now()
	c.Next(ctx)
	fmt.Printf("[http] %s %s %d %s\n",
		c.Method(), c.Path(), c.Response.StatusCode(), time.Since(start).Round(time.Millisecond))
}
