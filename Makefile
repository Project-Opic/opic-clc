# opic-techbase —— gopherforge 工程模式 Makefile
SHELL := /bin/bash

DSN ?= postgres://postgres:postgres@localhost:5432/go_techbase?sslmode=disable
GOOSE := go run github.com/pressly/goose/v3/cmd/goose

.PHONY: setup dev build test lint clean frontend-install frontend-build \
        migrate-up migrate-status migrate-down migrate-create \
        compose-up compose-down compose-logs docker-build

setup: ## 初始化:下载依赖 + 复制 .env
	go mod download
	@test -f .env || cp .env.example .env
	@echo "setup 完成"

dev: ## 本地启动后端(:9680,自动 goose 迁移 + 种子)
	go run ./cmd/techbase

dev-frontend: ## 本地启动前端(:5173,proxy → 9680)
	cd frontend && npm run dev

build: ## 编译后端二进制
	go build -o bin/techbase ./cmd/techbase

test: ## 单元测试
	go test ./...

lint: ## go vet
	go vet ./...

frontend-install: ## 安装前端依赖
	cd frontend && npm install

frontend-build: ## 构建前端产物(frontend/dist)
	cd frontend && npm run build

migrate-up: ## goose 迁移 Up(DSN 变量可覆盖)
	$(GOOSE) -dir migrations postgres "$(DSN)" up

migrate-status: ## goose 迁移状态
	$(GOOSE) -dir migrations postgres "$(DSN)" status

migrate-down: ## goose 回滚最近一笔
	$(GOOSE) -dir migrations postgres "$(DSN)" down

migrate-create: ## goose 新建迁移(make migrate-create MIGRATION_NAME=add_xxx)
	$(GOOSE) -dir migrations create $(MIGRATION_NAME) sql

compose-up: ## 拉起数据栈 + 应用栈(含网关/前端)
	docker compose -f docker-compose.infra.yml -p opic-techbase-infra up -d --wait
	docker compose -p opic-techbase up -d --build

compose-down: ## 停止应用栈与数据栈
	docker compose -p opic-techbase down
	docker compose -f docker-compose.infra.yml -p opic-techbase-infra down

compose-logs: ## 跟踪应用栈日志
	docker compose -p opic-techbase logs -f

docker-build: ## 构建后端镜像
	docker build -t opic-techbase:latest .

clean: ## 清理构建产物
	rm -rf bin frontend/dist frontend/node_modules log
