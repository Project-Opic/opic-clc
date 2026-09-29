# go-techbase —— Go 技术底座(ontology-driven-dev)

`ontology-driven-dev` 技能的**默认技术底座**(v2 起),与 Python 版 `techbase/`(code-paas)同构:
《系统管理 + 流程引擎需求规格说明书》的可运行技术空框架,含客户申请/客户查询两个示例功能
(暂存→提交→客户经理审批→部门总经理审批的严格串行审批流)。

## 1. 技术选型(OPIC 规划对齐)

| 层 | 选型 | 说明 |
|---|---|---|
| 后端框架 | **[hertz-admin](https://github.com/gjing1st/hertz-admin)**(CloudWeGo Hertz + GORM) | 采用其工程布局(cmd/ + internal/apiserver 分层 router→controller→service→store)与配置/日志/中间件风格;`pkg/errcode` 重写为 OPIC 统一错误码 |
| 数据库 | **PostgreSQL 16**(OPIC-数据服务域集成 [Pigsty](https://pigsty.io) 部署) | 连接串经数据服务域 FUNC-05/FUNC-12 自助申请获取;GORM 仅承担连接池与事务,数据访问为原生 SQL(与 Python 版零 ORM 语义一致) |
| 身份认证 | **ZITADEL**(OPIC-零信任安全中心集成) | OIDC Authorization Code + PKCE;中间件 JWKS(RS256)验签;userinfo 角色声明映射本地角色并自动 provisioning;`mode=local` 保留本地账号密码回退(开发/单机) |
| 错误码 | **OPIC 架构元数据域(O-ARC)统一错误码** | 7 位 `[3 位前缀][4 位数字]`,成功码 `SUC0000`,响应结构 `{"returnInfo":{"returnCode","errorMsg"},"data":...}`;底座前缀 `SYS`(底座平台,见 O-ARC 前缀分配表) |
| 前端 | 与 Python techbase **完全同源**(React 18 + TypeScript + Vite + reactflow) | 仅两处适配:`request.ts` 解包 returnInfo 结构;vite proxy 指向 9680 |
| 本体 | 七模型 YAML(M1/M2/M5/M6/MU) | 与 Python 版同一套 `models/`,运行时语义注册表加载 |

## 2. 目录结构(hertz-admin 布局)

```
go-techbase/
├── cmd/go-techbase/main.go          # 入口:配置→PG16→本体注册表→种子→HTTP
├── configs/config.yml               # 配置(数据库/认证模式/ZITADEL/本体目录)
├── internal/
│   ├── apiserver/
│   │   ├── apiserver.go             # hertz 装配(CORS/日志/前端静态托管)
│   │   ├── router/v1/init.go        # /api 路由(公开组+登录组+权限中间件)
│   │   ├── controller/              # REST 控制器(统一 returnInfo 响应)
│   │   ├── service/                 # 业务服务(auth/user/rbac/flow/workbench/customer)
│   │   │   └── engine/              # 轻量工作流引擎(start/approve/reject/return)
│   │   └── store/                   # GORM PG16 连接 + 原生 SQL 数据访问
│   └── pkg/
│       ├── auth/                    # local HS256 会话 + ZITADEL OIDC(发现/JWKS/PKCE/userinfo)
│       ├── config/                  # yaml 配置(hertz-admin config 风格)
│       ├── expr/                    # 安全表达式求值器(网关条件/M3 规则,替代 simpleeval)
│       ├── middleware/              # 认证(双令牌)/权限/CORS
│       └── ontology/                # 运行时语义注册表(七模型 YAML)
├── pkg/errcode/                     # OPIC 统一错误码(7 位 + returnInfo)
├── migrations/schema.sql            # PG16 DDL(与 Python 版 schema.sql 同构,13 表)
├── models/                          # 七模型示例(与 Python 版同一套)
└── frontend/                        # 与 Python techbase 同源前端(两处适配)
```

## 3. 快速开始

前置:Go 1.24+;一个 PostgreSQL 16 实例(生产经 OPIC-DBS/Pigsty 供给;本机可用任意 PG)。

```bash
# 1) 建库(示例)
createdb go_techbase

# 2) 改 configs/config.yml 的 database 段(或用 GO_TECHBASE_DSN 环境变量)

# 3) 启动(自动建表 + 种子,幂等)
cd go-techbase
go run ./cmd/go-techbase            # http://localhost:9680

# 4) 前端(开发模式)
cd frontend && npm install && npm run dev   # http://localhost:5173,proxy → 9680
```

默认账号(local 模式):`admin/admin123`(超管)、`sales/123456`、`cmanager/123456`、`gm/123456`。

### 冒烟自测(已验证)

```
sales 登录 → 建草稿 → 提交(启动 FLOW-CUSTOMER-APPROVAL,状态「待客户经理审批」)
→ cmanager 待办通过 → gm 待办通过 → 客户状态「已通过」(flow_instance APPROVED,flow_history 完整)
```

## 4. ZITADEL 接入(mode=zitadel)

1. 零信任中心 docker-compose 起 ZITADEL(未修改容器,Login UI 由其托管);
2. 在 ZITADEL 建项目/应用(Web OIDC),`redirect_uri` 填 `http://<host>:9680/api/auth/callback`;
3. `configs/config.yml`:`auth.mode: zitadel`,填 `issuer/client_id/client_secret/redirect_url`;
4. 浏览器访问 `GET /api/auth/login-url` 取授权地址跳转;回调后服务端换 token、读 userinfo、
   按角色声明(默认 `roles`)映射本地角色并自动建用户,随后签发底座会话令牌;
5. 中间件同时接受 ZITADEL 原生 access token(服务间调用),口径与零信任设计
   (token 验签 + 角色检查,不复制身份库)一致。

## 5. OPIC 错误码(pkg/errcode)

| 码 | 含义 | | 码 | 含义 |
|---|---|---|---|---|
| SUC0000 | 成功(全平台唯一) | | SYS9101 | 未登录/凭证失效 |
| SYS1001 | 参数错误 | | SYS9102 | 凭证无效 |
| SYS1002 | 对象不存在 | | SYS9103 | 无权限 |
| SYS1003 | 唯一性冲突 | | SYS9104 | 用户名/密码错误或禁用 |
| SYS1004 | 状态不允许 | | SYS9001 | 服务内部错误 |
| SYS1005-1008 | 流程状态/未发布/图非法 | | SYS9002 | 数据库错误 |

业务错误 0001-8999,系统错误 9000-9999;新前缀须经架构管理中心(O-ARC)注册平台登记。

## 6. 与 Python techbase(code-paas)的对应

| Python(Flask+SQLite) | Go(Hertz+PG16) |
|---|---|
| `backend/app.py` | `cmd/go-techbase/main.go` + `internal/apiserver/apiserver.go` |
| `api/*.py`(9 组) | `router/v1/init.go` + `controller/`(同路径同语义) |
| `services/*.py` | `internal/apiserver/service/*.go` |
| `engine/flow_engine.py` | `service/engine/flow_engine.go`(start/approve/reject/return 语义一致) |
| `ontology/registry.py` | `internal/pkg/ontology/registry.go` |
| `simpleeval` | `internal/pkg/expr`(自研安全求值器) |
| `schema.sql`(SQLite) | `migrations/schema.sql`(PG16;时间列 TEXT 保持输出格式一致) |
| `seed.py` | `service/seed.go`(同一套种子:4 角色/26 权限/16 资源/4 用户/M6 流程导入) |
| PyJWT + `login_required` | `internal/pkg/auth` + `middleware`(双令牌 + 权限中间件) |

差异(有意为之):响应结构从 `{success,message,data,errorCode}` 升级为 OPIC `returnInfo` 规范;
认证新增 ZITADEL 模式;数据库从 SQLite 升级为 PostgreSQL 16(Pigsty 供给)。

## 事件总线与持久工作流（v3.3 新增模块）

| 模块 | 说明 | 依赖 |
|---|---|---|
| `pkg/mq` | 事件总线客户端封装：主题 `opic.<source>.<type>`、信封（ID/Type/Source/TraceID/UserID/OccurredAt/Data）、审计头 `X-Trace-Id`/`X-User-Id` 透传；`enabled=false` 如实降级为 Noop | NATS / JetStream（第 3 层，可替换） |
| `internal/workflow` | 持久工作流接入点：task queue `opic.<中心名>`、WorkflowID 与 `O-APP` `WFL` 编号族对齐；执行明细供 O-MON 采集 | Temporal Server（第 3 层，可替换） |

契约（基线 v3.3 附录 A.2 #14/#15）：业务代码只依赖本仓库接口（`mq.Publisher` / `workflow.Runner`），**不得直接 import NATS / Temporal SDK**（契约不漂移）；编排结构合法性判定在 O-APP，本底座只承载运行（Q-4 同型边界）。`go test ./pkg/mq/... ./internal/workflow/...` 为纯本地单测（无需服务端）。
