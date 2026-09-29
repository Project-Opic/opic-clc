# opic-techbase v2 · OPIC 技术底座（gopherforge 基准重构版）

> **v2.0.0** ｜ 以 [GopherForge](https://github.com/SuperiorChuo/gopherforge) 技术栈为基准（冲突以基准为主），融合 v1（Hertz 单体 go-techbase）的全部 OPIC 功能模块。
> 基线对齐：OPIC 需求基线 v3.3（《114-版本冻结基线》附录 A.2 #14 NATS / #15 Temporal）。

## 技术栈（基准）

| 层 | 组件 |
|---|---|
| 后端 | Go + Gin（微服务 ×9，单模块 monorepo `microservices/services/`） |
| 数据 | PostgreSQL + GORM（连接池/事务）+ goose 迁移（禁 AutoMigrate）+ Redis |
| 消息 | **NATS / JetStream**（`shared/pkg/natsx` + OPIC 契约封装 `shared/pkg/opicmq`） |
| 工作流 | **Temporal**（基准 `bpm` 流程服务 + OPIC 接入点 `shared/pkg/opicworkflow`） |
| 网关 | Traefik（统一鉴权入口） |
| 前端 | React 19 + Ant Design 6 + Vite（`microservices/web`） |
| 可观测 | Prometheus / Grafana / Loki / OTel Collector（`platform/deploy/`） |
| 编排 | Docker Compose（数据栈与应用栈分离，`make compose-up`） |

## 目录

```
microservices/services/
├── shared/pkg/          # 46+ 公共库（基准）+ OPIC 移植包：
│   ├── opicmq/          #   事件总线契约封装（opic.<source>.<type>，审计头透传，Noop 如实降级）
│   ├── opicworkflow/    #   持久工作流接入（WFL 编号族对齐，task queue 每中心一队列）
│   ├── errcode/         #   O-ARC 统一错误码（[3位前缀][4位数字]，SUC0000，returnInfo 信封）
│   ├── ontology/        #   七模型本体注册表（M1/M2/M5/M6/MU YAML）
│   └── expr/            #   M3 规则表达式求值器
├── opicdemo/            # ★ OPIC 功能演示服务（v1 应用整体移植，gin 化）：
│   └── internal/        #   api/ service/（流程引擎+审批工作台+RBAC）/ dao/ auth/（ZITADEL 双模式）/ config/
├── system|auth|identity|api|audit|bpm|file|monitor/   # 基准 8 服务
└── web/                 # React 前端
platform/deploy/         # NATS / Prometheus / Grafana / Loki / OTel / Alertmanager
```

## 冲突裁决（融合原则：基准为主）

| 领域 | 基准（胜出） | v1（处置） |
|---|---|---|
| Web 框架 | Gin | Hertz 弃用，opicdemo 控制器/中间件已 gin 化 |
| 配置 | 12-factor 仅环境变量（`OPICDEMO_` 前缀） | yaml 弃用；字段形态对齐迁移 |
| 迁移 | goose（`00001_init.sql`） | 幂等 schema.sql 转为 goose Up；AutoMigrate 移除 |
| 错误信封 | 基准机器可读 ErrorCode（线缆格式） | `errcode` 作为 OPIC 族（opicdemo）响应信封保留 |
| RBAC/用户 | system/auth/identity 服务 | v1 同名模块不移植（避免双权威），演示面在 opicdemo |
| ZITADEL SSO | 基准 auth 服务 | v1 双模式客户端随 opicdemo 保留（演示面） |
| NATS | 基准 natsx | OPIC 契约层 opicmq 共存（发布接口/审计头/降级） |
| 前端 | React 19 + AntD 6（基准 web/） | v1 Vue 前端弃用 |

## 快速开始

```bash
cd microservices/services
go build ./... && go test ./shared/...
# 单服务运行（需 PostgreSQL；OPICDEMO_DSN 或 OPICDEMO_DB_* 环境变量）
go run ./opicdemo/cmd        # :9700，goose 自动迁移 + 七模型加载 + 种子
# 全栈（基准）
make compose-up
```

## 与 v1 的兼容性

v1 冻结于 tag **v1.1.0**（Hertz 单体形态，含 go-techbase 原始模块）。部署形态变更（单服务 → 微服务 + Traefik 网关），API 路由面在 opicdemo 保持 v1 语义（`/api/...`）。
