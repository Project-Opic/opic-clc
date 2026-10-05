-- cluster-center-mcc(OPIC 集群域 CLC)业务表 DDL —— goose 迁移 000004
-- 由 m1-object-model.yaml 生成(七个聚合根 → 七张主表;本域七个聚合均为独立聚合根,无子实体从表)。
-- 约定:
--   * 5 个默认字段(created_by/created_at/updated_by/updated_at/flag)由服务层自动赋值;
--   * 业务编号(cluster_no/node_no/binding_no/snapshot_no/task_no/probe_no)
--     一律由服务层按 OPIC 统一口径「{类型码}-{8 位日期段}-{4 位流水段}」生成
--     (类型码 CLU/NOD/BND/SNP/TSK/PRB,三位前缀 + 四位流水号;服务层与业务写入同事务 + 事务级咨询锁);
--   * 名称类唯一约束按不区分大小写实现(m3 INV-01/INV-10/INV-16/INV-20):唯一索引建在 LOWER(col) 上;
--   * 删除一律逻辑删除(flag=0);快照/探活/任务记录只增不改(INV-25/INV-31/INV-35);
--   * kubeconfig 内容与任何凭据值不入库,仅存存放位置标识(INV-08)。
-- 时间列统一 TEXT,默认 to_char(now(),'YYYY-MM-DD HH24:MI:SS'),与底座平台表口径一致。

-- +goose Up

-- ============ AGG-CLC-01 集群(Cluster) ============
CREATE TABLE IF NOT EXISTS clc_cluster (
    id BIGSERIAL PRIMARY KEY,
    cluster_no VARCHAR(50) UNIQUE NOT NULL,           -- 集群编号(服务层生成 CLU-,只读)
    name VARCHAR(100) NOT NULL,                       -- 集群名称(INV-01 未销毁范围内唯一,不区分大小写)
    description VARCHAR(500),                         -- 说明
    cluster_dns_domain VARCHAR(100),                  -- 集群 DNS 域名(如 clc-01.opic-ai.ccoe.tech)
    version_ref VARCHAR(100),                         -- 交付版本引用(O-IGN 版本;REF-05 未归档且就绪及之后)
    executor_ref VARCHAR(100),                        -- 交付执行机引用(O-IGN 执行机)
    registry_ref VARCHAR(100),                        -- 镜像仓库实例引用(O-REG 实例)
    registry_project_ref VARCHAR(100),                -- 镜像仓库项目引用(与实例成对,INV-05)
    run_mode VARCHAR(20) NOT NULL DEFAULT 'MCC',      -- 运行模式(字典 CLC_RUN_MODE;INV-04 LEGACY ⇔ 四类引用全空)
    status VARCHAR(30) NOT NULL DEFAULT 'DRAFT',      -- 生命周期状态(字典 CLC_CLUSTER_STATUS;INV-02 单向迁移)
    install_step_index INTEGER,                       -- 失败步骤索引(INV-03 INSTALL_FAILED 必填)
    health VARCHAR(20) NOT NULL DEFAULT 'UNKNOWN',    -- 健康结论(字典 CLC_HEALTH;INV-06 DEGRADED 仅显式恢复清除)
    degraded_reason VARCHAR(40),                      -- 降级原因(字典 CLC_DEGRADED_REASON;R-05)
    last_probe_at TEXT,                               -- 最近探活时间
    kubeconfig_ref VARCHAR(255),                      -- 访问凭据存放标识(仅位置标识,INV-08 凭据值禁止入库)
    master_endpoints VARCHAR(255),                    -- 主控入口(INV-07 RUNNING 及之后非空)
    destroy_reason VARCHAR(500),                      -- 销毁申请原因(FUNC-05 提交时必填,审批留痕)
    destroy_draft TEXT,                               -- 销毁申请草稿(JSON;保存草稿/提交双按钮)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    destroyed_at TEXT,                                -- 销毁时间(INV-02 DESTROYED 终态且行保留)
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cluster_name_ci ON clc_cluster (LOWER(name)) WHERE flag = 1;
CREATE INDEX IF NOT EXISTS idx_cluster_status ON clc_cluster (status);
CREATE INDEX IF NOT EXISTS idx_cluster_health ON clc_cluster (health);

-- ============ AGG-CLC-02 集群节点(ClusterNode) ============
CREATE TABLE IF NOT EXISTS clc_cluster_node (
    id BIGSERIAL PRIMARY KEY,
    node_no VARCHAR(50) UNIQUE NOT NULL,              -- 节点编号(服务层生成 NOD-,只读)
    cluster_ref VARCHAR(50) NOT NULL,                 -- 所属集群(clc_cluster.cluster_no)
    asset_ref VARCHAR(100),                           -- 资产标识(O-AST 资产;裸机可空,INV-12 凭 IP 人工接管)
    node_role VARCHAR(20) NOT NULL,                   -- 节点角色(字典 CLC_NODE_ROLE,INV-11)
    ip_address VARCHAR(64) NOT NULL,                  -- IP 地址(INV-10 同集群内唯一)
    k8s_nodename VARCHAR(128),                        -- K8s 节点名(INV-10 非空时集群内唯一;INV-14 ADDED 必填)
    node_status VARCHAR(20) NOT NULL DEFAULT 'PENDING', -- 节点状态(字典 CLC_NODE_STATUS;INV-13 REMOVED 终局)
    node_health VARCHAR(20) NOT NULL DEFAULT 'UNKNOWN', -- 节点健康结论(字典 CLC_NODE_HEALTH)
    node_health_at TEXT,                              -- 最近健康时间
    remark VARCHAR(500),                              -- 备注
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_node_cluster_ip ON clc_cluster_node (cluster_ref, LOWER(ip_address)) WHERE flag = 1;
CREATE UNIQUE INDEX IF NOT EXISTS uq_node_cluster_name ON clc_cluster_node (cluster_ref, LOWER(k8s_nodename)) WHERE flag = 1 AND k8s_nodename IS NOT NULL AND k8s_nodename <> '';
CREATE INDEX IF NOT EXISTS idx_node_cluster ON clc_cluster_node (cluster_ref);
CREATE INDEX IF NOT EXISTS idx_node_asset ON clc_cluster_node (asset_ref) WHERE asset_ref IS NOT NULL;

-- ============ AGG-CLC-03 中心引用绑定(ClusterBinding) ============
CREATE TABLE IF NOT EXISTS clc_binding (
    id BIGSERIAL PRIMARY KEY,
    binding_no VARCHAR(50) UNIQUE NOT NULL,           -- 绑定编号(服务层生成 BND-,只读)
    cluster_ref VARCHAR(50) NOT NULL,                 -- 所属集群(clc_cluster.cluster_no)
    target_capability VARCHAR(20) NOT NULL,           -- 目标能力(字典 CLC_TARGET_CAPABILITY)
    target_kind VARCHAR(20) NOT NULL,                 -- 资源类型(字典 CLC_TARGET_KIND;组合合法性 INV-17)
    target_ref VARCHAR(100) NOT NULL,                 -- 资源标识(跨域跳转键)
    purpose VARCHAR(30) NOT NULL,                     -- 用途(字典 CLC_BINDING_PURPOSE)
    binding_source VARCHAR(20) NOT NULL DEFAULT 'MANUAL', -- 登记来源(字典 CLC_BINDING_SOURCE)
    reconcile_note VARCHAR(500),                      -- 对账注记(INV-18 EVENT_SYNC 行变更必填)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
-- INV-16:同集群「目标能力+资源类型+用途+资源标识」组合唯一(不区分大小写)
CREATE UNIQUE INDEX IF NOT EXISTS uq_binding_combo ON clc_binding
    (cluster_ref, LOWER(target_capability), LOWER(target_kind), LOWER(purpose), LOWER(target_ref)) WHERE flag = 1;
CREATE INDEX IF NOT EXISTS idx_binding_cluster ON clc_binding (cluster_ref);
CREATE INDEX IF NOT EXISTS idx_binding_target ON clc_binding (target_capability, target_ref);

-- ============ AGG-CLC-04 集群参数(ClusterParam) ============
CREATE TABLE IF NOT EXISTS clc_param (
    id BIGSERIAL PRIMARY KEY,
    cluster_ref VARCHAR(50) NOT NULL,                 -- 所属集群(clc_cluster.cluster_no;INV-20 集群内键唯一)
    param_key VARCHAR(100) NOT NULL,                  -- 参数键
    param_value TEXT NOT NULL,                        -- 参数值
    param_source VARCHAR(20) NOT NULL DEFAULT 'DERIVED', -- 参数来源(字典 CLC_PARAM_SOURCE;INV-21 DERIVED 派生来源必填)
    derived_from VARCHAR(20),                         -- 派生来源(字典 CLC_PARAM_DERIVED_FROM)
    derive_note VARCHAR(500),                         -- 派生注记(INV-22 待同步时必填:哪个上游、什么变更)
    sync_pending SMALLINT NOT NULL DEFAULT 0,         -- 待同步标记(R-04 派生置位;FUNC-15 确认后清除)
    updated_by BIGINT,                                -- 更新人(INV-21 MANUAL_OVERRIDE 每次修改留痕)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_param_cluster_key ON clc_param (cluster_ref, LOWER(param_key)) WHERE flag = 1;
CREATE INDEX IF NOT EXISTS idx_param_sync_pending ON clc_param (cluster_ref) WHERE sync_pending = 1;

-- ============ AGG-CLC-05 版本快照(ClusterSnapshot) ============
-- INV-25:创建后不可修改(无 updated 语义,默认字段仅为口径一致);回滚产生新快照、不覆盖旧快照
CREATE TABLE IF NOT EXISTS clc_snapshot (
    id BIGSERIAL PRIMARY KEY,
    snapshot_no VARCHAR(50) UNIQUE NOT NULL,          -- 快照编号(服务层生成 SNP-,只读)
    cluster_ref VARCHAR(50) NOT NULL,                 -- 所属集群(clc_cluster.cluster_no)
    revision_no INTEGER NOT NULL,                     -- 快照序号(INV-24 同集群单调递增、不可复用)
    change_summary VARCHAR(255) NOT NULL,             -- 变更摘要
    content_snapshot TEXT NOT NULL,                   -- 内容快照(集群行 JSON)
    binding_snapshot TEXT NOT NULL,                   -- 引用快照(引用台账 JSON)
    snapshot_type VARCHAR(20) NOT NULL,               -- 快照类型(字典 CLC_SNAPSHOT_TYPE)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_snapshot_cluster_rev ON clc_snapshot (cluster_ref, revision_no);
CREATE INDEX IF NOT EXISTS idx_snapshot_cluster ON clc_snapshot (cluster_ref);

-- ============ AGG-CLC-06 集群任务(ClusterTask) ============
-- INV-31:任务记录创建后不可改写(状态回写由系统流程承载,对账修正必须留痕)
CREATE TABLE IF NOT EXISTS clc_task (
    id BIGSERIAL PRIMARY KEY,
    task_no VARCHAR(50) UNIQUE NOT NULL,              -- 任务编号(服务层生成 TSK-,只读)
    cluster_ref VARCHAR(50) NOT NULL,                 -- 所属集群(clc_cluster.cluster_no)
    task_kind VARCHAR(20) NOT NULL,                   -- 任务类别(字典 CLC_TASK_KIND)
    task_status VARCHAR(20) NOT NULL DEFAULT 'QUEUED', -- 任务状态(字典 CLC_TASK_STATUS;INV-28 STALLED/FAILED 分列)
    execution_path VARCHAR(20) NOT NULL DEFAULT 'LOCAL', -- 执行位置(字典 CLC_EXEC_PATH;INV-32 登记值不得静默切换)
    remote_task_ref VARCHAR(100),                     -- 远端任务引用(执行机侧回执)
    fail_step_index INTEGER,                          -- 失败步骤索引(INV-30 INSTALL/RETRY_INSTALL 失败必填且写回集群)
    fail_reason VARCHAR(40),                          -- 失败原因(字典 CLC_TASK_FAIL_REASON;INV-29 FAILED 必填)
    result_summary VARCHAR(500),                      -- 结果摘要(INV-29 终态必填)
    started_at TEXT,                                  -- 开始时间
    ended_at TEXT,                                    -- 结束时间(INV-29 终态必填)
    submitted_by VARCHAR(50) NOT NULL,                -- 提交人
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_task_cluster ON clc_task (cluster_ref);
CREATE INDEX IF NOT EXISTS idx_task_kind_status ON clc_task (task_kind, task_status);

-- ============ AGG-CLC-07 探活记录(HealthProbe) ============
-- INV-35:创建后不可修改;INV-36 只读探测,探测过程不向集群写任何内容
CREATE TABLE IF NOT EXISTS clc_probe (
    id BIGSERIAL PRIMARY KEY,
    probe_no VARCHAR(50) UNIQUE NOT NULL,             -- 记录编号(服务层生成 PRB-,只读)
    cluster_ref VARCHAR(50) NOT NULL,                 -- 所属集群(clc_cluster.cluster_no)
    probed_at TEXT NOT NULL,                          -- 探活时间
    trigger_source VARCHAR(20) NOT NULL,              -- 触发方式(字典 CLC_TRIGGER_SOURCE)
    apiserver_reachable SMALLINT NOT NULL DEFAULT 0,  -- 控制面可达(0/1)
    tls_handshake_ok SMALLINT NOT NULL DEFAULT 0,     -- TLS 握手通过(0/1)
    ready_nodes INTEGER NOT NULL DEFAULT 0,           -- 就绪节点数(INV-34 不得大于总节点数)
    total_nodes INTEGER NOT NULL DEFAULT 0,           -- 总节点数
    probe_result VARCHAR(20) NOT NULL,                -- 探活结论(字典 CLC_PROBE_RESULT)
    fail_reason VARCHAR(255),                         -- 失败原因(INV-34 结论 FAILED 时必填)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_probe_cluster ON clc_probe (cluster_ref, probed_at);

-- ============ REF-01~12 平台参数(本域代管:规则级平台配置) ============
CREATE TABLE IF NOT EXISTS clc_platform_param (
    id BIGSERIAL PRIMARY KEY,
    param_key VARCHAR(80) UNIQUE NOT NULL,            -- 参数键(REF 规则声明的 clc.* 键)
    param_value TEXT NOT NULL,                        -- 参数值
    param_group VARCHAR(50) NOT NULL DEFAULT 'base',  -- 参数分组(legacy/snapshot/probe/wizard)
    description VARCHAR(255),                         -- 参数说明
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);

-- 平台参数初值(REF-01/02/03/04/07/08/11 模型声明的默认值;幂等)
INSERT INTO clc_platform_param (param_key, param_value, param_group, description) VALUES
    ('clc.legacy_mode_enabled', 'false', 'legacy', 'REF-01 legacy 运行模式开关(平台可强制禁用;禁用时创建必须至少一项中心引用)'),
    ('clc.snapshot_retention_days', '365', 'snapshot', 'REF-02 快照保留期(天;B-AUTO 清理基准,INV-27 清理留痕)'),
    ('clc.probe_interval_seconds', '60', 'probe', 'REF-03 探活周期(秒,B-AUTO-01 调度基准)'),
    ('clc.probe_fail_threshold', '3', 'probe', 'REF-03 连续失败阈值(次;R-08 连续失败 ⇒ UNREACHABLE)'),
    ('clc.probe_recover_threshold', '3', 'probe', 'REF-11 探活恢复健康连续成功阈值(次,防抖动)'),
    ('clc.probe_retention_days', '90', 'probe', 'REF-08 探活记录保留期(天)'),
    ('clc.wizard_default_executor', '', 'wizard', 'REF-04 创建向导默认执行机引用(缺省空,人工选择)'),
    ('clc.wizard_default_version', '', 'wizard', 'REF-04 创建向导默认交付版本引用(缺省空,人工选择)'),
    ('clc.wizard_default_registry_instance', '', 'wizard', 'REF-04 创建向导默认仓库实例引用(缺省空,人工选择)'),
    ('clc.health_cache_seconds', '60', 'probe', 'REF-07 聚合健康响应缓存时长(秒,防下游穿透)')
ON CONFLICT (param_key) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS clc_platform_param;
DROP TABLE IF EXISTS clc_probe;
DROP TABLE IF EXISTS clc_task;
DROP TABLE IF EXISTS clc_snapshot;
DROP TABLE IF EXISTS clc_param;
DROP TABLE IF EXISTS clc_binding;
DROP TABLE IF EXISTS clc_cluster_node;
DROP TABLE IF EXISTS clc_cluster;
