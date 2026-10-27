# opic-clc

> 简体中文: [README-zh.md](README-zh.md)

[![License](https://img.shields.io/badge/License-MulanPSL--2.0-2E7D32)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](frontend/package.json)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](migrations)

[Documentation](#documentation) | [Architecture](#architecture) | [Usage](#usage) | [Deployment](#deployment) | [Roadmap](#roadmap) | [License](#license)

**opic-clc is the Cluster Capability Center (O-CLC / CLC) of the OPIC open intelligent compute platform**: a runnable capability center for the cluster domain, built on the [opic-techbase](https://github.com/Project-Opic/opic-techbase) scaffold (gopherforge engineering pattern) and the seven-model ontology. It manages the full cluster lifecycle — ledgers, nodes, delivery orchestration, references and parameters, snapshots and rollback, health probing, and operations observation.

The center answers a recurring operations question: how do you keep a fleet of clusters truthful? Every state change carries its rule evidence (INV / R), every reference is dual-written, every snapshot sequence is monotonic and existence-checked, and every destroy passes a mandatory single-level approval. Delivery actions are handed to the ignition center (O-IGN) step by step through its facade.

This repository contains the backend service (Gin), the console frontend (React + Ant Design), business migrations, ontology models (YAML), deployment manifests, and developer documentation.

## Documentation

| Document | Contents |
|---|---|
| [Onboarding](docs/ONBOARDING.md) | Background, architecture, environment, first run, development workflow |
| [Build & Deployment](docs/DEPLOYMENT.md) | Build, production rollout, migration rollback, fork checklist |
| [Operations](docs/OPERATIONS.md) | Health checks, monitoring and alerting, logging, backup, credentials, troubleshooting |
| [FAQ](docs/FAQ.md) | Field notes on SSO, SQL, deployment, frontend, and AI |
| [LABK8S Delivery Manual](docs/LABK8S交付用户手册-2026-10-12.md) | LABK8S delivery user manual (2026-10-12) |

## Architecture

opic-clc runs as two units that share a single `internal/service` business layer:

- **`cluster-center-mcc`** — the primary backend. Gin serves the `/api/clc/*` REST surface (47 endpoints) plus `/health` and `/metrics` probes. On startup it runs goose migrations, imports M5 seeds (five roles / 32 permissions / menus / accounts) and the M6 approval flow, all idempotently.
- **`frontend`** — a React SPA served by nginx, covering eight business screens, three workbench pages, and the right-hand persistent AI workspace.

Request paths:

- Browser → Traefik (`clc.<domain>`) → `/api`, `/health`, `/metrics` to `cluster-center-mcc`; everything else to `frontend`.
- Cluster lifecycle actions → delivery facade of the ignition center (`IGNITION_BASE_URL`, Bearer `IGNITION_TOKEN`) → `POST /api/v1/ign/delivery/*`.

Data plane: PostgreSQL 16 (business database, center schema `clc`; platform tables `sys_*` / `flow_*` / `audit_logs` / `sys_notice` / `temporalflow_*` / `ai_*` plus seven aggregate main tables and the platform-parameter table), Redis (token revocation blacklist, optional), NATS JetStream (stream `clc`, subject domain `opic.clc.>`, optional), and Temporal (task queue `clc`, optional).

Every business response uses a uniform envelope — `{"returnInfo":{"returnCode","errorMsg"},"data":...}` with `SUC0000` on success, rule violations reported as `SYS1009` (ErrRule) with a Chinese reason, and permission denials as `SYS9103`.

### Core Capabilities

| Capability | What it does |
|---|---|
| **Cluster ledger** | Four-step wizard creation, information maintenance, and explicit recovery (AGG-CLC-01, FUNC-01/02/05/16/19/20/28/29; destroy approval linked) |
| **Node management** | Add / remove / rebind nodes with asset-occupancy interception (AGG-CLC-02, FUNC-06/07/08/21, R-02) |
| **Delivery orchestration** | Install / resume / upgrade / backup / restore / start-stop / destroy-approval tasks with failure write-back and success landing (AGG-CLC-06, FUNC-03/04/09~12/25, R-09/R-13) |
| **References and parameters** | Center-reference dual-write, parameter derivation with overwrite protection, and pending-sync confirmation (AGG-CLC-03/04, FUNC-13/14/15/22/23, R-01/R-04) |
| **Snapshots and rollback** | Monotonic sequence numbers, existence validation, and rollback that abandons references (AGG-CLC-05, FUNC-17/18/24, R-06) |
| **Health probing** | Dual-source probing with threshold-based `UNREACHABLE` and explicit recovery from `DEGRADED` only (AGG-CLC-07, FUNC-26/27, R-08/R-11) |
| **Operations observation** | Runtime statistics and platform parameters (FUNC-30, REF-01~12 configuration) |
| **Destroy approval** | Cluster destroy single-level approval (`FLOW-PROC-APP-01`, CLC-ROLE-02 platform ops administrator, R-10 mandatory — no exemption) |
| **AI workspace** | Ontology-registry injection plus a read-only SQL tool (SELECT/WITH only, whitelist, forced `LIMIT`, audited per query) |

## How opic-clc Relates to Other Tools

opic-clc is a **capability center**, not a replacement for a database, an identity provider, or a workflow engine. It inherits the opic-techbase engineering baseline and layers the cluster domain on top.

| Layer | Tools | Relationship |
|---|---|---|
| Engineering baseline | [opic-techbase](https://github.com/Project-Opic/opic-techbase) | Platform-level fork; the baseline carries the technology layer only, while business semantics belong to this center — the center evolves independently and security fixes flow back one-way |
| Engineering model and visual system | [gopherforge](https://github.com/SuperiorChuo/gopherforge) (MIT) | Technology selection and repository layout baseline (inherited via the baseline fork); the console's glass theme is ported from its frontend with source headers preserved |
| Identity and authentication | ZITADEL | OIDC provider (Authorization Code + PKCE, JWKS RS256 verification, role-claim mapping and auto-provisioning); `AUTH_MODE=local` keeps a local fallback with graphical captcha |
| Authorization | Casbin RBAC first, then ZITADEL | Behavior-level permission codes `PERM-FUNC-01..32` (M5 permission matrix) |
| Workflow runtime | Temporal | Task queue `clc`; the destroy approval runs on the inherited approval engine; `flowGuardian` SLA watchdog applies |
| Event bus | NATS JetStream | Business events (`opic.clc.business.*`) published side-channel with idempotent stream handling; failures never block the main path |
| Data and cache | PostgreSQL (provisioned by Pigsty), Redis | PostgreSQL holds business data in the center schema `clc`; Redis is used only for token revocation and degrades silently when disabled |
| Upstream delivery | O-IGN (ignition center) | Cluster lifecycle actions are delivered step-by-step through the ignition facade (`IGNITION_BASE_URL` / `IGNITION_TOKEN`); the local delivery chain (`DELIVERY_TOOL_*`) is the fallback execution plane |

## Repository Map

| Component | Path | Description |
|---|---|---|
| **Backend service** | [`cmd/techbase/`](cmd/techbase) | Composition root inherited from the baseline: config → logging → PostgreSQL (goose) → seeds → Redis → Gin → graceful shutdown |
| **HTTP layer** | [`internal/api/`](internal/api) | `routes.go` route table, `business/clc.go` center controller (47 endpoints under `/api/clc/*`, writes gated by `PERM-FUNC-xx`), plus `assistant` / `sql` route groups |
| **Business layer** | [`internal/service/`](internal/service) | `clc_common.go` numbering and events, `clc_rules.go` M3 rule equivalents, one file per aggregate (`clc_cluster` / `clc_node` / `clc_binding` / `clc_snapshot` / `clc_task` / `clc_probe` / `clc_params`), `assistant.go` AI workspace, `seed.go` M5 seeds, workbench and flow adapters |
| **Shared libraries** | [`pkg/`](pkg) | Error codes, logging, Redis, tokens, metrics, health checks, graceful shutdown, event bus, workflow wrappers (inherited from the baseline) |
| **Migrations** | [`migrations/`](migrations) | Sequential goose migrations; `000004_business_clc.sql` carries the domain DDL (seven aggregate main tables plus the platform-parameter table) |
| **Ontology models** | [`models/`](models) | Seven-model YAML (M1/M2/M3/M5/M6/M7/MU) |
| **Frontend** | [`frontend/`](frontend) | React 19 + Ant Design 6 + Redux Toolkit + Vite; eight screens under `src/pages/clc/` plus three workbench pages and the AI workspace |
| **Deployment** | [`docker-compose.yml`](docker-compose.yml), [`docker-compose.infra.yml`](docker-compose.infra.yml), [`Dockerfile`](Dockerfile) | Separate application stack and data stack, with `BASE_DOMAIN` subdomain routing |
| **Documentation** | [`docs/`](docs) | Onboarding, deployment, operations, FAQ, LABK8S delivery manual |

## Base Software

Versions are pinned in `go.mod`, `frontend/package.json`, and the Dockerfiles. Infrastructure versions are those used by the production deployment.

### Runtime and infrastructure

| Component | Version | Upstream repository |
|---|---|---|
| Go | 1.27 | <https://github.com/golang/go> |
| PostgreSQL | 16 | <https://github.com/postgres/postgres> |
| Redis | 8 (optional) | <https://github.com/redis/redis> |
| NATS Server | 2.14 (optional) | <https://github.com/nats-io/nats-server> |
| Temporal | 1.29 (optional) | <https://github.com/temporalio/temporal> |
| Traefik | v3 | <https://github.com/traefik/traefik> |
| nginx | (frontend serving) | <https://github.com/nginx/nginx> |
| Node.js | 20+ (build stage) | <https://github.com/nodejs/node> |

### Backend (Go modules)

| Module | Version | Upstream repository |
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

### Frontend (npm)

| Package | Version | Upstream repository |
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

All third-party components above ship under permissive licenses (MIT / Apache-2.0 / BSD). Refer to each upstream repository for the exact terms.

## Usage

### Local Development Environment

Prerequisites: Go 1.27+, Node 20.12+ (nvm 20.19.5 recommended), and a reachable PostgreSQL 16 instance (production Pigsty HAProxy at `192.168.3.200:5433`).

```bash
# 1) Create the center schema (the application also runs goose migrations on startup)
psql -c "CREATE SCHEMA IF NOT EXISTS clc AUTHORIZATION postgres;"   # database opicdb

# 2) Configure (passwords are injected via environment variables only, never in the repo)
export CENTER_CODE=clc DB_SCHEMA=clc
export DB_HOST=192.168.3.200 DB_PORT=5433 DB_USER=postgres DB_NAME=opicdb
export REDIS_ENABLED=false
export EVENTBUS_DRIVER=nats EVENTBUS_URL="nats://192.168.3.200:14222"
export WORKFLOW_DRIVER=local                    # or temporal (queue clc)
export METRICS_ENABLED=true APP_PORT=9710

# 3) Start (goose migrations + M5 seeds + M6 flow import, idempotent)
make dev          # or go run ./cmd/techbase → http://localhost:9710

# 4) Frontend (development mode)
make frontend-install && make dev-frontend   # http://localhost:5173, proxy → 9710
```

Default accounts (`AUTH_MODE=local`, captcha login, password `admin123`):

| Account | Password | Role | Permission summary |
|---|---|---|---|
| `admin` | admin123 | System administrator | Super-admin position (injected at code level) |
| `clcadmin` | admin123 | CLC-ROLE-01 Cluster administrator | FUNC-01~18 full maintenance plus queries |
| `opsadmin` | admin123 | CLC-ROLE-02 Platform ops administrator | Queries, platform parameters, and destroy approval (FUNC-31/32) |
| `auditor` | admin123 | CLC-ROLE-03 Cluster auditor | Read-only ledger |
| `viewuser` | admin123 | CLC-ROLE-04 Cluster read-only user | Clusters / details / nodes / probes / statistics |

CLC-ROLE-05 is a downstream-capability-center service identity (cross-center cluster query `PERM-FUNC-28` only) and has no interactive account.

One-command full stack (compose):

```bash
cp .env.example .env
make compose-up           # data stack (--wait) → app stack: clc.opic-ai.ccoe.tech
make compose-down
```

### Smoke Path

```bash
CENTER_CODE=clc DB_SCHEMA=clc go run ./cmd/smoke   # infrastructure probes (pg/redis/nats/temporal)
```

Verified path (schema `clc`, 2026-10-05): create a cluster through the wizard (FUNC-01, transaction creates the cluster plus four reference rows and the first snapshot) → rule blocks (duplicate name, LEGACY with references, unpaired references, in-cluster IP duplication, asset occupancy) return `SYS1009` with Chinese reasons → the delivery chain lands `RUNNING` with the control-plane entry and credential markers, or writes back `INSTALL_FAILED` with the step index → health probing degrades to `UNREACHABLE` after three consecutive failures and recovers only through explicit recovery → the destroy approval runs `clcadmin` submit → `opsadmin` approve → the task plus cascaded reference cleanup, ending in the retained `DESTROYED` row.

### Building Individual Components

```bash
make build                 # backend binary → bin/cluster-center-mcc
make frontend-build        # frontend assets → frontend/dist

make test                  # unit tests
make lint                  # go vet
make migrate-status        # migration status
```

## Deployment

Production deployment runs through `make deploy-prod`: it rsyncs the source to the target host, builds the images there, copies compose and config files, then runs `docker compose up -d`. Migrations, seeds, and the M6 flow import execute automatically when the container starts.

```bash
make deploy-prod           # build, deploy, start
make deploy-prod-down      # stop the application stack
```

Key environment variables (see the [Deployment Guide](docs/DEPLOYMENT.md) for the full list):

| Variable | Description |
|---|---|
| `CENTER_CODE` / `DB_SCHEMA` | Center domain code and schema (isolation root) |
| `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME/DB_SSLMODE` | PostgreSQL connection (Pigsty HAProxy) |
| `APP_PORT` | HTTP port |
| `EVENTBUS_DRIVER/URL/USERNAME/PASSWORD/NATS_STREAM/NATS_SUBJECT_PREFIX` | Event bus (default `inproc`, derived stream `clc` and prefix) |
| `WORKFLOW_DRIVER/ADDRESS/NAMESPACE/TASK_QUEUE` | Workflow runtime (`local` / queue `clc`) |
| `REDIS_ENABLED/REDIS_HOST/REDIS_PORT/REDIS_PASSWORD` | Redis (logout revocation blacklist) |
| `METRICS_ENABLED` / `JWT_SECRET` / `AUTH_MODE` | Observability / session secret / auth mode |
| `DELIVERY_TOOL_BIN` / `DELIVERY_TOOL_DIR` / `DELIVERY_KUBECONFIG_ENV` / `DELIVERY_KUBECONFIG_PATH_PATTERN` | Local delivery chain: delivery tool binary, working directory, and execution-plane credential injection (unset ⇒ `LOCAL` cannot be registered, REF-16) |
| `IGNITION_BASE_URL` / `IGNITION_TOKEN` | Remote delivery chain: ignition-center facade address and service token (unset ⇒ `REMOTE_EXECUTOR` cannot be registered, INV-46) |

### Distribution Channels

Images are pushed to the internal Harbor registry: backend `192.168.3.200:18700/opic/clc`, frontend `192.168.3.200:18700/opic/clc-frontend` (`make harbor-push`). The production subdomain is `clc.opic-ai.ccoe.tech`.

## Contributing

Before opening a pull request, please confirm:

1. `go build ./...`, `go vet ./...`, and `cd frontend && npm run build` all pass.
2. Any added or changed endpoint keeps the `returnInfo` envelope and OPIC O-ARC error codes intact.
3. Schema changes ship as a new goose migration (never `AutoMigrate`), with a working `Down`.
4. Rule violations (`INV-*` / `R-*`) keep their Chinese explanations in the service layer; numbering uses the domain prefixes with transaction-level advisory locks against duplicates.

Branch names and commit messages follow `feat|fix|docs|chore(scope): summary`.

By submitting a contribution, you agree to license it under the [Mulan PSL v2](LICENSE).

## Roadmap

This repository does not maintain a long-range feature roadmap. Planned work and progress are tracked in the open:

- **Recently completed**: the seven-model landing with all rules (REF / INV / R) implemented in the service layer, the seven aggregates with their 47 endpoints and 32 permission codes, the LABK8S delivery user manual, and the smoke-verified full destroy-approval chain.
- **Known gaps**: the O-IGN remote delivery facade is wired for lifecycle actions; remaining upstream channels (asset existence checks, certificate issue/revocation) follow the ignition center's own landing.
- **Limitations**: the local delivery chain and the remote ignition facade are two registration kinds (`LOCAL` / `REMOTE_EXECUTOR`) with disjoint prerequisites; snapshot rollback abandons references by design and never deletes history rows.

## Community

- **Issues and suggestions**: open a repository issue with reproduction steps and environment details (version, migration number, deployment mode).
- **Internal channels**: the OPIC platform team; production incidents go through the on-call channel.

## Security & Support

- **Reporting a vulnerability**: please do not open a public issue; contact the platform team directly. The security baseline includes Casbin RBAC with ZITADEL fallback, `SYS9103` permission denials on writes, a read-only SQL whitelist with forced `LIMIT`, and audit logs for every administrative write.
- **Support scope**: the current main branch and the deployed production version.

## License

Copyright © 2026 wjluo

This project is licensed under the **Mulan Permissive Software License, Version 2 (Mulan PSL v2)**. The full text is in [`LICENSE`](LICENSE) and is also available at <http://license.coscl.org.cn/MulanPSL2>.

Subject to the terms of the license, you may copy, use, modify, and distribute this software, including for commercial purposes; copyright and license notices must be retained when redistributing. Section 3 grants no trademark license, and Section 5 provides the software on an "AS IS" basis without warranties of any kind.

### Third-Party and Ported Code

- **opic-techbase** (Mulan PSL v2): this repository is a platform-level fork; the technology layer inherits the baseline, and the baseline's license continues to apply to that portion.
- **gopherforge** (MIT): the engineering pattern and technology selection baseline, and the admin console's visual system (glass theme, login page, dashboard styling) is ported from its frontend, with source headers preserved in the ported files. Its MIT notice continues to apply to that portion.
