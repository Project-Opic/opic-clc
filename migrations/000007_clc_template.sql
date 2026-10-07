-- cluster-center-mcc(OPIC 集群域 CLC)易用性增补 DDL —— goose 迁移 000007(v1.2 集群模板 + 向导步进)
-- 由 m1-object-model.yaml v1.2 生成:
--   新增聚合 AGG-CLC-08 集群模板(ClusterTemplate,表 clc_cluster_template)及其两组内嵌子实体
--   (模板节点组 clc_template_node_group / 模板参数行 clc_template_param,均不独立编号、整组读写);
--   AGG-CLC-01 集群增列 template_ref(ASSOC-CLC-CLUSTER-TEMPLATE,MANY_TO_ONE);
--   AGG-CLC-06 任务增列 step_mode / operation_reason。
-- 约定:
--   * 5 个默认字段(created_by/created_at/updated_by/updated_at/flag)由服务层自动赋值;
--     m1 属性 createdBy(字符串)沿用既有口径映射到平台默认列 created_by,与 clc_cluster 同判据;
--   * 字典 CLC_TEMPLATE_STATE / CLC_STEP_MODE 由本体注册表运行期自动注册,本件不插字典行(与 000004/000005 一致);
--   * INV-51:模板名称未删除范围内唯一(与 INV-01 同口径,不区分大小写);is_default 为真者全局至多一行
--     且该行须为 ENABLED —— 库侧只兜「至多一行」这半边(部分唯一索引),状态半边由服务层执行;
--   * INV-52:子实体随模板整组替换 —— 组名模板内唯一、同角色模板内至多一行、参数键模板内唯一;
--     子实体行不做行级增量改写,历史行以 flag=0 保留(cascadeDelete 为逻辑删除);
--   * INV-49 / R-20:集群 template_ref 只在向导实例化瞬间写一次,此后只读,模板变更不回写集群;
--   * REF-17 / INV-50:step_mode 缺省按创建入口取 —— 库侧缺省 AUTO,与 v1.1 已验收的连跑入口
--     (编排屏 / 审批回调 / 定时看护)行为逐字不变;向导入口由服务层显式落 STEP,不靠库侧缺省;
--     任务创建瞬间落定、运行中不可互转,故本列不参与任何 UPDATE 语句(服务层口径,库侧不做触发器保护);
--   * 模板与集群之间无级联:销毁集群不动模板,删除模板(软删)不动集群引用。
-- 时间列统一 TEXT,默认 to_char(now(),'YYYY-MM-DD HH24:MI:SS'),与底座平台表口径一致。

-- +goose Up

-- ============ AGG-CLC-08 集群模板(ClusterTemplate) ============
CREATE TABLE IF NOT EXISTS clc_cluster_template (
    id BIGSERIAL PRIMARY KEY,
    template_no VARCHAR(50) UNIQUE NOT NULL,          -- 模板编号(服务层生成 TPL-,登记时创建,只读)
    name VARCHAR(100) NOT NULL,                       -- 模板名称(INV-51 未删除范围内唯一,不区分大小写)
    description VARCHAR(500),                         -- 说明(列表页直接可见)
    version_ref VARCHAR(100),                         -- 交付版本引用(O-IGN 版本;只存标识,空 = 实例化时由向导选定 R-20)
    param_template_ref VARCHAR(100),                  -- O-IGN 参数面模板标识(对端 AGG-IGN-07;只透传标识,不校验内容 R-21)
    template_status VARCHAR(20) NOT NULL DEFAULT 'DRAFT', -- 模板状态(字典 CLC_TEMPLATE_STATE;INV-51 DRAFT→ENABLED⇄DISABLED)
    is_default BOOLEAN NOT NULL DEFAULT false,        -- 是否默认(INV-51 全局至多一行为真且须 ENABLED;仅预选向导第二步)
    source_cluster_ref VARCHAR(50),                   -- 来源集群引用(FUNC-43「从既有集群克隆」留痕;空 = 人工登记;建成后只读)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_template_name_ci ON clc_cluster_template (LOWER(name)) WHERE flag = 1;
CREATE UNIQUE INDEX IF NOT EXISTS uq_template_default ON clc_cluster_template (is_default) WHERE is_default AND flag = 1;
CREATE INDEX IF NOT EXISTS idx_template_status ON clc_cluster_template (template_status);

-- ============ AGG-CLC-08 内嵌子实体:模板节点组(CLC-CLC-08-S1) ============
-- 角色拓扑配方,不承载 IP / 主机名 / 凭据(那是实例事实);随模板整组读写(INV-52)
CREATE TABLE IF NOT EXISTS clc_template_node_group (
    id BIGSERIAL PRIMARY KEY,
    template_ref VARCHAR(50) NOT NULL,                -- 所属模板(clc_cluster_template.template_no;级联从属)
    group_name VARCHAR(100) NOT NULL,                 -- 分组名称(INV-52 组内唯一)
    node_role VARCHAR(20) NOT NULL,                   -- 节点角色(字典 CLC_NODE_ROLE:MASTER/WORKER/INFRA;INV-52 同角色至多一行)
    expected_count INTEGER NOT NULL CHECK (expected_count >= 1), -- 期望数量(实例化时生成待登记节点位,真实地址由向导填入)
    sort_no INTEGER NOT NULL,                         -- 排序号(组内展示次序;整组替换语义)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tng_name ON clc_template_node_group (template_ref, group_name) WHERE flag = 1;
CREATE UNIQUE INDEX IF NOT EXISTS uq_tng_role ON clc_template_node_group (template_ref, node_role) WHERE flag = 1;
CREATE INDEX IF NOT EXISTS idx_tng_template_sort ON clc_template_node_group (template_ref, sort_no);

-- ============ AGG-CLC-08 内嵌子实体:模板参数行(CLC-CLC-08-S2) ============
-- 与集群参数(AGG-CLC-04)是「配方 ⇄ 实例」关系、不是同一张表;随模板整组读写(INV-52)
CREATE TABLE IF NOT EXISTS clc_template_param (
    id BIGSERIAL PRIMARY KEY,
    template_ref VARCHAR(50) NOT NULL,                -- 所属模板(clc_cluster_template.template_no;级联从属)
    param_key VARCHAR(100) NOT NULL,                  -- 参数键(INV-52 模板内唯一;键域沿用集群参数口径,不建字典)
    param_value VARCHAR(500) NOT NULL,                -- 参数默认值(可空串表示须现场填;INV-08 同纪律不得含凭据类值)
    description VARCHAR(500),                         -- 说明
    sort_no INTEGER NOT NULL,                         -- 排序号(整组替换语义)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tp_key ON clc_template_param (template_ref, param_key) WHERE flag = 1;
CREATE INDEX IF NOT EXISTS idx_tp_template_sort ON clc_template_param (template_ref, sort_no);

-- ============ AGG-CLC-01 集群:来源模板引用 ============
ALTER TABLE clc_cluster ADD COLUMN IF NOT EXISTS template_ref VARCHAR(50);
COMMENT ON COLUMN clc_cluster.template_ref IS '来源模板引用(clc_cluster_template.template_no;INV-49 实例化瞬间写一次后只读,空 = 未走模板)';
CREATE INDEX IF NOT EXISTS idx_cluster_template_ref ON clc_cluster (template_ref);

-- ============ AGG-CLC-06 任务:步进模式 + 操作事由 ============
-- 存量行一律 AUTO:v1.1 已验收的连跑入口是唯一创建方,库侧缺省必须保持该口径(REF-17)
ALTER TABLE clc_task ADD COLUMN IF NOT EXISTS step_mode VARCHAR(10) NOT NULL DEFAULT 'AUTO';
ALTER TABLE clc_task ADD COLUMN IF NOT EXISTS operation_reason VARCHAR(500);
COMMENT ON COLUMN clc_task.step_mode IS '步进模式(字典 CLC_STEP_MODE;REF-17 缺省按创建入口取,INV-50 创建瞬间落定且运行中不可互转)';
COMMENT ON COLUMN clc_task.operation_reason IS '操作事由(FUNC-45 跳步与 FUNC-46 高危动作派发必填,R-24 随任务行留痕)';

-- +goose Down
ALTER TABLE clc_task DROP COLUMN IF EXISTS operation_reason;
ALTER TABLE clc_task DROP COLUMN IF EXISTS step_mode;
DROP INDEX IF EXISTS idx_cluster_template_ref;
ALTER TABLE clc_cluster DROP COLUMN IF EXISTS template_ref;
DROP TABLE IF EXISTS clc_template_param;
DROP TABLE IF EXISTS clc_template_node_group;
DROP TABLE IF EXISTS clc_cluster_template;
