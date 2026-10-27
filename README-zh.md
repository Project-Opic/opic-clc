# opic-clc

> English: [README.md](README.md)

[![License](https://img.shields.io/badge/License-MulanPSL--2.0-2E7D32)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](frontend/package.json)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](migrations)

[文档](#文档) | [架构](#架构) | [使用](#使用) | [部署](#部署) | [路线图](#路线图) | [许可证](#许可证)

**opic-clc 是 OPIC 开放智能算力平台的集群能力中心（O-CLC / CLC）**：集群域的可运行 MCC（独立能力中心），基于 [opic-techbase](https://github.com/Project-Opic/opic-techbase) 脚手架（gopherforge 工程模式）与七模型本体全量落地，覆盖集群台账、节点管理、交付编排、引用与参数、快照与回滚、健康探活、运行观测的完整生命周期。

中心侧关注的是「一个集群 fleet 如何保持事实为真」：每次状态变更都带规则证据（INV / R），每条引用都双写，每个快照序号单调且做现存性校验，每次销毁都走强制单级审批。集群生命周期动作经点火中心（O-IGN）交付门面单步下发。

本仓库包含后端服务（Gin）、控制台前端（React + Ant Design）、业务迁移、本体模型（YAML）、部署编排与开发文档。

## 文档

| 文档 | 内容 |
|---|---|
| [新人培训入门](docs/ONBOARDING.md) | 背景 / 架构 / 环境 / 首次启动 / 开发流程 |
| [编译与部署指南](docs/DEPLOYMENT.md) | 构建、生产部署、迁移回滚、fork 清单 |
| [运维手册](docs/OPERATIONS.md) | 健康检查 / 监控告警 / 日志 / 备份 / 凭据 / 故障速查 |
| [FAQ](docs/FAQ.md) | 实战踩坑速查（SSO / SQL / 部署 / 前端 / AI） |
| [LABK8S 交付用户手册](docs/LABK8S交付用户手册-2026-10-12.md) | LABK8S 交付用户手册（2026-10-12） |

## 架构

opic-clc 由两个运行单元组成，共享同一套 `internal/service` 业务层：

- **`cluster-center-mcc`** —— 后端主服务。Gin 提供 `/api/clc/*` REST 接口（47 个端点）与 `/health`、`/metrics` 探针；启动时自动执行 goose 迁移、导入 M5 种子（五角色 / 32 权限 / 菜单 / 账号）与 M6 审批流，全部幂等。
- **`frontend`** —— nginx 托管的 React SPA，含八大业务屏、工作台三页与右侧常驻 AI 工作区。

请求链路：

- 浏览器 → Traefik（`clc.<domain>`）→ `/api`、`/health`、`/metrics` 转发到 `cluster-center-mcc`，其余路径到 `frontend`。
- 集群生命周期动作 → 点火中心交付门面（`IGNITION_BASE_URL`，Bearer `IGNITION_TOKEN`）→ `POST /api/v1/ign/delivery/*`。

数据面依赖：PostgreSQL 16（业务库，中心 schema `clc`；平台表 `sys_*` / `flow_*` / `audit_logs` / `sys_notice` / `temporalflow_*` / `ai_*` 之外，另有七聚合主表 + 平台参数表）、Redis（令牌吊销黑名单，可选）、NATS JetStream（stream `clc`，主题域 `opic.clc.>`，可选）、Temporal（任务队列 `clc`，可选）。

所有业务接口使用统一的 `{"returnInfo":{"returnCode","errorMsg"},"data":...}` 信封，成功码 `SUC0000`；规则违反统一 `SYS1009`（ErrRule）+ 中文原因；权限拒绝 `SYS9103`。

### 核心能力

| 能力 | 说明 |
|---|---|
| **集群台账** | 四步向导创建 / 信息维护 / 显式恢复（AGG-CLC-01，FUNC-01/02/05/16/19/20/28/29；销毁审批联动） |
| **节点管理** | 节点添加 / 移除 / 重绑定，资产占用拦截（AGG-CLC-02，FUNC-06/07/08/21，R-02） |
| **交付编排** | 安装 / 续跑 / 升级 / 备份 / 恢复 / 启停 / 销毁审批任务，失败写回与成功落点（AGG-CLC-06，FUNC-03/04/09~12/25，R-09/R-13） |
| **引用与参数** | 中心引用双写、参数派生与覆写保护、待同步确认（AGG-CLC-03/04，FUNC-13/14/15/22/23，R-01/R-04） |
| **快照与回滚** | 序号单调、现存性校验、放弃引用回滚（AGG-CLC-05，FUNC-17/18/24，R-06） |
| **健康探活** | 双源口径、连续失败达阈值判 `UNREACHABLE`、仅 `DEGRADED` 可显式恢复（AGG-CLC-07，FUNC-26/27，R-08/R-11） |
| **运行观测** | 运行统计与平台参数（FUNC-30，REF-01~12 配置） |
| **销毁审批** | 集群销毁单级审批（`FLOW-PROC-APP-01`，CLC-ROLE-02 平台运维管理员审批，R-10 强制不免审批） |
| **AI 工作区** | 本体注册表注入 + 只读 SQL 工具（仅 SELECT/WITH、白名单、强制 LIMIT、每次写审计） |

## 与其他工具的关系

opic-clc 是**能力中心**，不替代数据库、身份提供方或工作流引擎，而是在 opic-techbase 工程基线之上叠加集群域语义。

| 层次 | 相关工具 | 关系 |
|---|---|---|
| 工程基线 | [opic-techbase](https://github.com/Project-Opic/opic-techbase) | 平台级 fork：底座仅承载技术设施层，业务语义层归本中心——中心业务独立演进，底座安全修复单向回流 |
| 工程模式与视觉体系 | [gopherforge](https://github.com/SuperiorChuo/gopherforge)（MIT） | 经基线 fork 继承的技术选型与工程布局基准；控制台玻璃主题移植自其前端并保留来源注释 |
| 身份与认证 | ZITADEL | OIDC 身份提供方（Authorization Code + PKCE、JWKS RS256 验签、角色声明映射与自动 provisioning）；`AUTH_MODE=local` 保留本地回退（图形验证码登录） |
| 授权 | Casbin RBAC 优先 → 拒则查 ZITADEL | 行为级权限码 `PERM-FUNC-01..32`（M5 权限矩阵） |
| 流程运行时 | Temporal | 任务队列 `clc`；销毁审批跑在继承的审批引擎上；`flowGuardian` SLA 看护适用 |
| 事件总线 | NATS JetStream | 业务事件 `opic.clc.business.*` 旁路发布，存量流幂等（取回并 UpdateStream）；失败不阻塞主链路 |
| 数据与缓存 | PostgreSQL（Pigsty 供给）、Redis | PG 承载中心 schema `clc` 内的业务数据；Redis 仅用于令牌吊销，未启用时静默降级 |
| 上游交付 | O-IGN（点火中心） | 集群生命周期动作经点火门面单步下发（`IGNITION_BASE_URL` / `IGNITION_TOKEN`）；本地交付链路（`DELIVERY_TOOL_*`）为后备执行面 |

## 仓库结构

| 组件 | 路径 | 说明 |
|---|---|---|
| **后端服务** | [`cmd/techbase/`](cmd/techbase) | 组合根（继承基线）：配置 → 日志 → PostgreSQL（goose）→ 种子 → Redis → Gin → 优雅退出 |
| **HTTP 层** | [`internal/api/`](internal/api) | `routes.go` 路由表、`business/clc.go` 中心控制器（`/api/clc/*` 47 端点，写接口挂 `PERM-FUNC-xx`），以及 `assistant` / `sql` 路由组 |
| **业务层** | [`internal/service/`](internal/service) | `clc_common.go` 编号与事件、`clc_rules.go` M3 规则等价实现、各聚合文件（`clc_cluster` / `clc_node` / `clc_binding` / `clc_snapshot` / `clc_task` / `clc_probe` / `clc_params`）、`assistant.go` AI 工作区、`seed.go` M5 种子、工作台与流程适配 |
| **共享库** | [`pkg/`](pkg) | 错误码、日志、Redis、令牌、指标、健康检查、优雅退出、事件总线、工作流封装（继承基线） |
| **数据库迁移** | [`migrations/`](migrations) | goose 序号迁移；`000004_business_clc.sql` 为本域业务 DDL（七聚合主表 + 平台参数表） |
| **本体模型** | [`models/`](models) | 七模型 YAML（M1/M2/M3/M5/M6/M7/MU） |
| **前端** | [`frontend/`](frontend) | React 19 + Ant Design 6 + Redux Toolkit + Vite；`src/pages/clc/` 八大屏 + 工作台三页 + AI 工作区 |
| **部署编排** | [`docker-compose.yml`](docker-compose.yml)、[`docker-compose.infra.yml`](docker-compose.infra.yml)、[`Dockerfile`](Dockerfile) | 应用栈与数据栈分离，`BASE_DOMAIN` 子域路由 |
| **文档** | [`docs/`](docs) | 入门、部署、运维、FAQ、LABK8S 交付用户手册 |

## 基础软件依赖

版本以 `go.mod`、`frontend/package.json` 与 Dockerfile 为准；基础设施版本为生产部署实际使用的版本。

### 运行时与基础设施

| 组件 | 版本 | 上游仓库 |
|---|---|---|
| Go | 1.27 | <https://github.com/golang/go> |
| PostgreSQL | 16 | <https://github.com/postgres/postgres> |
| Redis | 8（可选） | <https://github.com/redis/redis> |
| NATS Server | 2.14（可选） | <https://github.com/nats-io/nats-server> |
| Temporal | 1.29（可选） | <https://github.com/temporalio/temporal> |
| Traefik | v3 | <https://github.com/traefik/traefik> |
| nginx | （前端托管） | <https://github.com/nginx/nginx> |
| Node.js | 20+（构建阶段） | <https://github.com/nodejs/node> |

### 后端（Go 模块）

| 模块 | 版本 | 上游仓库 |
|---|---|---|
| `github.com/gin-gonic/gin` | v1.12.0 | <https://github.com/gin-gonic/gin> |
| `gorm.io/gorm` | v1.31.0 | <https://github.com/go-gorm/gorm> |
| `gorm.io/driver/postgres` | v1.6.0 | <https://github.com/go-gorm/postgres> |
| `github.com/pressly/goose/v3` | v3.27.3 | <https://github.com/pressly/goose> |
| `github.com/golang-jwt/jwt/v5` | v5.3.0 | <https://github.com/golang-jwt/jwt> |
| `github.com/redis/go-redis/v9` | v9.22.0 | <https://github.com/redis/go-redis> |
| `github.com/nats-io/nats.go` | v1.54.0 | <https://github.com/nats-io/nats.go> |
| `go.temporal.io/sdk` | v1.49.0 | <https://github.com/temporalio/sdk-go> |
| `go.uber.org/zap` | v1.28.0 | <https://github.com/uber-go/zap> |
| `gopkg.in/natefinch/lumberjack.v2` | v2.2.1 | <https://github.com/natefinch/lumberjack> |
| `golang.org/x/crypto` | v0.57.0 | <https://github.com/golang/crypto> |
| `gopkg.in/yaml.v3` | v3.0.1 | <https://github.com/go-yaml/yaml> |

### 前端（npm）

| 包 | 版本 | 上游仓库 |
|---|---|---|
| `react` / `react-dom` | 19 | <https://github.com/facebook/react> |
| `antd` | 6 | <https://github.com/ant-design/ant-design> |
| `@ant-design/icons` | 6 | <https://github.com/ant-design/ant-design-icons> |
| `@reduxjs/toolkit` | 2.5 | <https://github.com/reduxjs/redux-toolkit> |
| `react-router-dom` | 7.2 | <https://github.com/remix-run/react-router> |
| `axios` | 1.8 | <https://github.com/axios/axios> |
| `@xyflow/react` | 12.3 | <https://github.com/xyflow/xyflow> |
| `vite` | 8 | <https://github.com/vitejs/vite> |
| `typescript` | 5.8 | <https://github.com/microsoft/TypeScript> |

以上第三方组件均使用宽松许可证（MIT / Apache-2.0 / BSD），具体条款以上游仓库为准。

## 使用

### 本地开发环境

前置：Go 1.27+、Node ≥ 20.12（推荐 nvm 20.19.5）、可达的 PostgreSQL 16（生产 192.168.3.200:5433）。

```bash
# 1) 建中心 schema（首次执行；应用启动也会跑 goose 迁移）
psql -c "CREATE SCHEMA IF NOT EXISTS clc AUTHORIZATION postgres;"   # 库 opicdb

# 2) 配置（口令只经环境变量注入，不写进命令行与仓库）
export CENTER_CODE=clc DB_SCHEMA=clc
export DB_HOST=192.168.3.200 DB_PORT=5433 DB_USER=postgres DB_NAME=opicdb
export REDIS_ENABLED=false
export EVENTBUS_DRIVER=nats EVENTBUS_URL="nats://192.168.3.200:14222"
export WORKFLOW_DRIVER=local                    # 或 temporal（队列 clc）
export METRICS_ENABLED=true APP_PORT=9710

# 3) 启动（goose 迁移 + M5 种子 + M6 流程导入，幂等）
make dev          # 或 go run ./cmd/techbase → http://localhost:9710

# 4) 前端（开发模式）
make frontend-install && make dev-frontend   # http://localhost:5173, proxy → 9710
```

默认账号（`AUTH_MODE=local`，验证码登录，口令均为 `admin123`）：

| 账号 | 口令 | 角色 | 权限口径 |
|---|---|---|---|
| `admin` | admin123 | 系统管理员 | 超管位（代码层注入） |
| `clcadmin` | admin123 | CLC-ROLE-01 集群管理员 | FUNC-01~18 全量维护 + 查询 |
| `opsadmin` | admin123 | CLC-ROLE-02 平台运维管理员 | 查询 + 平台参数 + 销毁审批（FUNC-31/32） |
| `auditor` | admin123 | CLC-ROLE-03 集群审计员 | 只读台账 |
| `viewuser` | admin123 | CLC-ROLE-04 集群只读用户 | 集群 / 详情 / 节点 / 探活 / 统计 |

CLC-ROLE-05 下游能力中心为服务身份，仅跨中心集群查询 `PERM-FUNC-28`，不建账号。

一键全栈（compose）：

```bash
cp .env.example .env
make compose-up           # 数据栈(--wait) → 应用栈: clc.opic-ai.ccoe.tech
make compose-down
```

### 冒烟路径

```bash
CENTER_CODE=clc DB_SCHEMA=clc go run ./cmd/smoke   # 基础设施探针（pg/redis/nats/temporal）
```

实测路径（schema `clc`，2026-10-05）：四步向导创建集群（FUNC-01，事务内 集群 + 引用台账×4 + 首快照）→ 规则阻断（重名 / LEGACY+引用 / 引用不成对 / 集群内 IP 重复 / 资产占用）返回 `SYS1009` 中文提示 → 交付链落 `RUNNING`（主控入口 / 凭据标识登记，R-13）或写回 `INSTALL_FAILED`（步骤索引，R-09）→ 健康双源连续失败 3 次判 `UNREACHABLE`、仅 `DEGRADED` 可显式恢复 → 销毁审批全链路（`clcadmin` 提交 → `opsadmin` 通过 → 任务 + 引用台账级联清理 → `DESTROYED` 终态保留行）。

### 单独构建

```bash
make build                 # 后端二进制 → bin/cluster-center-mcc
make frontend-build        # 前端产物 → frontend/dist

make test                  # 单元测试
make lint                  # go vet
make migrate-status        # 查看迁移状态
```

## 部署

生产部署通过 `make deploy-prod` 完成：rsync 源码到目标机 → 在目标机构建镜像 → 复制 compose 与配置 → `docker compose up -d`。迁移、种子与 M6 流程导入在容器启动时自动执行。

```bash
make deploy-prod           # 构建 + 部署 + 拉起
make deploy-prod-down      # 停止应用栈
```

关键环境变量（完整清单见 [部署指南](docs/DEPLOYMENT.md)）：

| 变量 | 说明 |
|---|---|
| `CENTER_CODE` / `DB_SCHEMA` | 中心域码与 schema（隔离三件套根） |
| `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME/DB_SSLMODE` | PostgreSQL 连接（Pigsty haproxy） |
| `APP_PORT` | HTTP 端口 |
| `EVENTBUS_DRIVER/URL/USERNAME/PASSWORD/NATS_STREAM/NATS_SUBJECT_PREFIX` | 事件总线（默认 `inproc`，派生 stream `clc` 与主题前缀） |
| `WORKFLOW_DRIVER/ADDRESS/NAMESPACE/TASK_QUEUE` | 工作流运行时（`local` / 队列 `clc`） |
| `REDIS_ENABLED/REDIS_HOST/REDIS_PORT/REDIS_PASSWORD` | Redis（登出吊销黑名单） |
| `METRICS_ENABLED` / `JWT_SECRET` / `AUTH_MODE` | 可观测 / 会话密钥 / 认证模式 |
| `DELIVERY_TOOL_BIN` / `DELIVERY_TOOL_DIR` / `DELIVERY_KUBECONFIG_ENV` / `DELIVERY_KUBECONFIG_PATH_PATTERN` | 本地交付链路：交付工具可执行文件、工作目录与执行面凭据注入方式（未配置 ⇒ `LOCAL` 不可登记，REF-16） |
| `IGNITION_BASE_URL` / `IGNITION_TOKEN` | 远程交付链路：点火中心交付门面地址与服务令牌（未配置 ⇒ `REMOTE_EXECUTOR` 不可登记，INV-46） |

### 分发渠道

镜像推送到内部 Harbor：后端 `192.168.3.200:18700/opic/clc`，前端 `192.168.3.200:18700/opic/clc-frontend`（`make harbor-push`）。生产子域 `clc.opic-ai.ccoe.tech`。

## 贡献

提交前请确认：

1. `go build ./...`、`go vet ./...` 通过，且 `cd frontend && npm run build` 通过；
2. 新增或修改接口时保持 `returnInfo` 信封与 OPIC O-ARC 错误码口径不变；
3. 涉及数据库变更时新增 goose 迁移文件（禁止 AutoMigrate），并保证 `Down` 可回滚；
4. 规则违反（`INV-*` / `R-*`）在服务层保留中文提示；编号使用域码前缀，事务级咨询锁防重号。

分支与提交信息沿用 `feat|fix|docs|chore(scope): 说明` 格式。

提交贡献即表示同意以[木兰宽松许可证第 2 版](LICENSE)授权该贡献。

## 路线图

本仓库不维护长期功能路线图，计划与进展以下列方式公开：

- **已完成**（近期）：七模型落地且规则（REF / INV / R）全量落服务层；七聚合 47 端点 32 权限码；LABK8S 交付用户手册；销毁审批全链路冒烟实测通过。
- **待办**（已知缺口）：O-IGN 远程交付门面已接生命周期动作；其余上游通道（资产存在性核对、证书签发/吊销）随点火中心自身落地跟进。
- **限制**：本地交付链路与远程点火门面是两种登记形态（`LOCAL` / `REMOTE_EXECUTOR`），前置条件互斥；快照回滚按设计放弃引用，不删历史行。

## 社区

- **问题与建议**：通过仓库 Issue 反馈，附复现步骤与环境信息（版本、迁移号、部署方式）。
- **内部沟通**：OPIC 平台团队；生产问题走运维值班渠道。

## 安全与支持

- **报告漏洞**：请勿公开提交 Issue，直接联系平台团队。安全基线包括 Casbin RBAC + ZITADEL 兜底、写接口 `SYS9103` 权限拒绝、只读 SQL 白名单与强制 LIMIT、管理端写操作全审计。
- **支持范围**：当前主分支与生产部署版本。

## 许可证

Copyright © 2026 wjluo

本项目采用**木兰宽松许可证第 2 版（Mulan PSL v2）**，全文见 [`LICENSE`](LICENSE)，也可从 <http://license.coscl.org.cn/MulanPSL2> 获取。

在遵守许可证条款的前提下，你可以自由复制、使用、修改和分发本软件，包括商业使用；分发时需保留版权与许可声明。许可证第 3 条明确不授予商标许可，第 5 条按「现状」提供软件、不附带任何明示或默示担保。

### 第三方与移植代码

- **opic-techbase**（Mulan PSL v2）：本仓库为平台级 fork，技术设施层继承自基线，基线的许可证继续适用于该部分代码。
- **gopherforge**（MIT）：工程模式与技术选型基准；管理控制台视觉体系（玻璃主题、登录页、Dashboard 样式）移植自其前端，移植文件头部保留来源注释；其 MIT 许可声明继续适用于该部分代码。
