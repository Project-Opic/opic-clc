# cluster-center-mcc —— OPIC 集群能力中心（O-CLC / CLC）

OPIC 开放智能算力平台 **集群域能力中心**：基于 opic-techbase 脚手架（gopherforge 工程模式）与
七模型本体（M1 对象 / M2 行为 / M3 规则 / M5 主体 / M6 流程 / M7 报表 / MU 界面，见 `models/`）
全量落地的可运行 MCC（独立能力中心）。

- **中心域码**：`clc`（隔离三件套：opicdb schema=clc、NATS 流=clc + 主题 `opic.clc.>`、Temporal 任务队列=clc）
- **业务范围**：集群台账（四步向导创建 / 信息维护 / 显式恢复）、节点管理（添加 / 移除 / 重绑定、资产占用拦截）、
  交付编排（安装 / 续跑 / 升级 / 备份 / 恢复 / 启停 / 销毁审批）、引用与参数（中心引用双写、参数派生与覆写保护、待同步确认）、
  快照与回滚（序号单调、现存性校验、放弃引用回滚）、健康探活（双源口径、降级显式恢复）、运行观测与平台参数
- **审批流**：集群销毁单级审批（`FLOW-PROC-APP-01`，CLC-ROLE-02 平台运维管理员审批，R-10 强制不免审批）

## 1. 技术选型

| 层 | 选型 | 说明 |
|---|---|---|
| 后端框架 | **Gin** | 组合根 `cmd/techbase/main.go`；中间件链 metrics → recovery → 路由 |
| 数据库 | **PostgreSQL 16**（Pigsty 集群 haproxy :5433） | 库 `opicdb`，中心专属 schema `clc`；GORM 仅承担连接池与事务，数据访问为原生 SQL + map 行 |
| 迁移 | **goose** | `migrations/000NNN_*.sql`；000004_business_clc.sql 为本域业务 DDL（七聚合主表 + 平台参数表），启动自动 Up |
| 缓存/吊销 | **Redis 8**（可选） | 登出令牌 jti 黑名单；未启用时静默降级 |
| 认证 | **ZITADEL** OIDC + 本地回退 | `mode=local` 图形验证码登录；自动 provisioning 兜底角色 CLC-ROLE-04 |
| 授权 | Casbin RBAC 优先 → 拒则查 ZITADEL | 行为级权限 `PERM-FUNC-01..32`（m5 权限矩阵） |
| 错误码 | OPIC O-ARC 统一错误码 | `SUC0000` 成功；规则违反统一 `SYS1009`（ErrRule）+ 中文原因；权限拒绝 `SYS9103` |
| 事件总线 | NATS JetStream（inproc 降级） | 业务事件旁路发布 `opic.clc.business.*`；存量流幂等（取回并 UpdateStream） |
| 工作流 | Temporal（local 降级） | 任务队列 `clc`；flowGuardian SLA 看护 |
| 前端 | React 19 + Ant Design 6 + Redux Toolkit + Vite | 八业务屏 + 工作台三页 + 右侧常驻 AI 工作区 |

## 2. 目录结构（业务增量）

```
cluster-center-mcc/
├── cmd/techbase/main.go            # 组合根(与底座一致)
├── configs/config.yml              # center_code=clc / schema=clc / NATS 流=clc / Temporal 队列=clc
├── internal/api/business/clc.go    # 集群域控制器(47 端点,/api/clc/*,写接口挂 PERM-FUNC-XX)
├── internal/api/routes.go          # 路由注册(客户示例已移除,assistant/sql 新增)
├── internal/service/
│   ├── clc_common.go               # 编号生成(CLU/NOD/BND/SNP/TSK/PRB 三位前缀+四位流水,事务级咨询锁)/事件发布
│   ├── clc_rules.go                # M3 规则等价实现(REF/INV/R 全量,违反给中文提示)
│   ├── clc_cluster.go              # AGG-CLC-01 集群(FUNC-01/02/05/16/19/20/28/29;销毁审批联动)
│   ├── clc_node.go                 # AGG-CLC-02 节点(FUNC-06/07/08/21;R-02 占用拦截)
│   ├── clc_binding.go              # AGG-CLC-03/04 引用与参数(FUNC-13/14/15/22/23;R-01 双写/R-04 派生)
│   ├── clc_snapshot.go             # AGG-CLC-05 快照(FUNC-17/18/24;R-06 现存性校验)
│   ├── clc_task.go                 # AGG-CLC-06 任务(FUNC-03/04/09~12/25;R-09 失败写回/R-13 成功落点)
│   ├── clc_probe.go                # AGG-CLC-07 探活(FUNC-26/27;R-08 双源写回)
│   ├── clc_params.go               # 平台参数(FUNC-30;REF-01~12 配置)
│   ├── assistant.go                # AI 助理(本体注册表注入 + 只读 SQL 工具)
│   ├── seed.go                     # M5 种子(五角色/32 权限/菜单/账号)+ M6 流程导入(node_graph)
│   └── workbench.go / flow.go      # 审批中心(销毁审批联动 SyncClusterDestroyFromFlow)
├── migrations/000004_business_clc.sql
├── models/                         # 七模型 YAML(阶段二产物,m3/m7 新增)
└── frontend/src/pages/clc/         # 八屏(ClusterManage/NodeManage/OrchManage/RefManage/SnapManage/HealthManage/OpsManage/ParamsManage)
```

## 3. 快速开始

前置：Go 1.27+、Node ≥ 20.12（推荐 nvm 20.19.5）、可达的 PostgreSQL 16（生产 192.168.3.200:5433）。

```bash
# 1) 建中心 schema（首次执行；应用启动也会跑 goose 迁移）
#    psql: CREATE SCHEMA IF NOT EXISTS clc AUTHORIZATION postgres;

# 2) 配置（config.yml 默认指生产 192.168.3.200；口令走环境变量,不入库）
export CENTER_CODE=clc DB_SCHEMA=clc
export DB_HOST=192.168.3.200 DB_PORT=5433 DB_USER=postgres DB_PASSWORD=<口令> DB_NAME=opicdb
export REDIS_ENABLED=false                      # 可选,禁用则令牌吊销降级内存
export EVENTBUS_DRIVER=nats EVENTBUS_URL="nats://192.168.3.200:14222" \
       EVENTBUS_USERNAME=opic EVENTBUS_PASSWORD=<口令>
export WORKFLOW_DRIVER=local                    # 或 temporal(队列 clc)
export METRICS_ENABLED=true APP_PORT=9710

# 3) 启动(自动 goose 迁移 + M5 种子 + M6 流程导入,幂等)
make dev          # 或 go run ./cmd/techbase → http://localhost:9710

# 4) 前端(开发模式)
make frontend-install && make dev-frontend   # http://localhost:5173,proxy → 9710
```

默认账号（local 模式，验证码登录）：

| 账号 | 口令 | 角色 | 权限口径 |
|---|---|---|---|
| `admin` | admin123 | 系统管理员 | 超管位（代码层注入） |
| `clcadmin` | admin123 | CLC-ROLE-01 集群管理员 | FUNC-01~18 全量维护 + 查询 |
| `opsadmin` | admin123 | CLC-ROLE-02 平台运维管理员 | 查询 + 平台参数 + 销毁审批（FUNC-31/32） |
| `auditor` | admin123 | CLC-ROLE-03 集群审计员 | 只读台账 |
| `viewuser` | admin123 | CLC-ROLE-04 集群只读用户 | 集群/详情/节点/探活/统计 |

（CLC-ROLE-05 下游能力中心为服务身份，仅跨中心集群查询 PERM-FUNC-28，不建账号。）

### 一键全栈（compose）

```bash
cp .env.example .env      # 按需修改
make compose-up           # 数据栈(--wait) → 应用栈:clc.opic-ai.ccoe.tech
make compose-down
```

## 4. 冒烟自测（2026-10-05 实测，schema=clc）

```bash
# 基础设施探针(pg/redis/nats/temporal;口令经环境变量注入)
CENTER_CODE=clc DB_SCHEMA=clc go run ./cmd/smoke
```

实测记录（app=9710）：

| 项 | 结果 |
|---|---|
| `/health`、`/health/ready` | 200 / ready；goose 迁移至版本 4；eventbus(nats) 流 clc 就绪 |
| 登录 | admin/admin123 验证码（SVG text 拼接）登录通过；viewuser/opsadmin 各角色登录通过 |
| 创建集群（FUNC-01） | 事务内 集群 + 引用台账×4 + 首快照；CLU-20261005-xxxx 编号 |
| 规则阻断×5 | INV-01 重名 / INV-04 LEGACY+引用 / INV-05 引用不成对 / INV-10 集群内 IP 重复 / R-02 资产占用 → `SYS1009` 中文提示 |
| 交付链路 | 安装成功 → RUNNING + 主控入口/凭据标识登记（R-13）；注入失败 → INSTALL_FAILED + 步骤索引写回（R-09/INV-30）；续跑 → 从失败步骤继续（INV-03） |
| 健康双源（R-08） | 连续失败 3 次达阈值 → UNREACHABLE；不覆盖 DEGRADED；显式恢复（R-11 仅 DEGRADED 可调用）→ HEALTHY |
| 引用与参数 | 引用调整 → 双写（R-01）+ 镜像源参数派生 4 项待同步（R-04）→ FUNC-15 确认清除 + 快照 |
| 销毁审批全链路 | clcadmin 提交（影响面强制展示 + 保存草稿/提交双按钮）→ opsadmin 待办通过 → DESTROY 任务 + 引用台账级联清理 + 参数上下文清理 → DESTROYED 终态保留行 |
| 权限矩阵 | viewuser 创建集群 → `SYS9103 无权限执行该操作`；查询接口 200 |
| AI 只读 SQL | 合法 SELECT 表格返回；`sys_user` 白名单外拒绝；DELETE 拒绝；无 LIMIT 自动追加 200 |
| 编译 | `go build ./...` + `go vet ./...` 通过；前端 `tsc --noEmit` + `npm run build` 通过 |

## 5. 配置（环境变量 > config.yml）

| 环境变量 | 说明 | 默认 |
|---|---|---|
| `CENTER_CODE` | 中心域码（隔离三件套根） | clc |
| `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME/DB_SCHEMA/DB_SSLMODE` | 数据面（Pigsty haproxy） | config.yml |
| `APP_PORT` | HTTP 端口 | 9680 |
| `EVENTBUS_DRIVER/URL/USERNAME/PASSWORD/NATS_STREAM/NATS_SUBJECT_PREFIX` | 事件总线 | inproc / 派生 |
| `WORKFLOW_DRIVER/ADDRESS/NAMESPACE/TASK_QUEUE` | 工作流 | local / clc |
| `REDIS_ENABLED/REDIS_HOST/REDIS_PORT/REDIS_PASSWORD` | Redis（登出吊销黑名单） | disabled |
| `METRICS_ENABLED` / `JWT_SECRET` / `AUTH_MODE` | 可观测 / 会话密钥 / 认证模式 | true / 见生产 .env / local |
| `DELIVERY_TOOL_BIN` / `DELIVERY_TOOL_DIR` / `DELIVERY_KUBECONFIG_ENV` / `DELIVERY_KUBECONFIG_PATH_PATTERN` | 本地交付链路：交付工具可执行文件、工作目录与执行面凭据注入方式（未配置 ⇒ `LOCAL` 不可登记，REF-16） | config.yml |
| `IGNITION_BASE_URL` / `IGNITION_TOKEN` | 远程交付链路：点火中心交付门面地址与服务令牌（对端 `delivery.facade_token`；未配置 ⇒ `REMOTE_EXECUTOR` 不可登记，INV-46） | 空 |

平台表保留口径：`sys_*`、`flow_*`、`audit_logs`、`sys_notice`、`temporalflow_*`、`ai_*`（000001~000003）；
底座客户申请示例表（customer_application）与页面已移除。

## 6. 文档导航

| 文档 | 内容 |
|---|---|
| [新人培训入门](docs/ONBOARDING.md) | 背景/架构/环境/首次启动/开发流程 |
| [编译与部署指南](docs/DEPLOYMENT.md) | 构建、生产部署、迁移回滚、fork checklist |
| [运维手册](docs/OPERATIONS.md) | 健康检查/监控告警/日志/备份/凭据/故障速查 |
| [FAQ](docs/FAQ.md) | 实战踩坑速查（SSO/SQL/部署/前端/AI） |
