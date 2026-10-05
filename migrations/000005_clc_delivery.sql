-- cluster-center-mcc(OPIC 集群域 CLC)交付执行内核 DDL —— goose 迁移 000005(v1.1 真实交付链)
-- 由 m1-object-model.yaml AGG-CLC-06 内嵌子实体生成:
--   交付步骤(ClusterTaskStep,表 clc_task_step)与交付日志(TaskLogLine,表 clc_task_log)。
--   两表均为任务聚合内子实体(非独立聚合、不独立编号,任务内序号唯一,级联从属)。
-- 约定:
--   * 5 个默认字段(created_by/created_at/updated_by/updated_at/flag)由服务层自动赋值;
--   * INV-37:步骤序号任务内唯一且自 1 连续 —— 唯一索引 (task_ref, step_index) WHERE flag=1;
--   * INV-43:日志序号任务内单调递增、只增不改 —— 唯一索引 (task_ref, line_no) WHERE flag=1;
--   * INV-42:日志内容落库前完成凭据掩码(服务层承载,原文不留存);
--   * INV-44 / B-AUTO-13:日志受保留期约束(REF-15),到期逻辑删除(flag=0)并留痕。
-- 时间列统一 TEXT,与底座口径一致。

-- +goose Up

-- ============ AGG-CLC-06 内嵌子实体:交付步骤(CLC-CLC-06-S1) ============
CREATE TABLE IF NOT EXISTS clc_task_step (
    id BIGSERIAL PRIMARY KEY,
    task_ref VARCHAR(50) NOT NULL,                    -- 所属任务(clc_task.task_no;级联从属)
    step_index INTEGER NOT NULL,                      -- 步骤序号(INV-37 任务内唯一、自 1 连续;续跑沿用上次序号语义)
    step_code VARCHAR(20) NOT NULL,                   -- 步骤编码(字典 CLC_INSTALL_STEP 01..07;非安装类取与 CLC_TASK_KIND 同名单步编码)
    step_name VARCHAR(100) NOT NULL,                  -- 步骤名称(取字典显示文本;界面不得内置步骤清单)
    step_status VARCHAR(20) NOT NULL DEFAULT 'PENDING', -- 步骤状态(字典 CLC_STEP_STATUS;INV-41 终局保护)
    command_summary VARCHAR(255),                     -- 命令摘要(参数数组渲染的动作+参数骨架;INV-40 不得含凭据)
    verdict_source VARCHAR(20),                       -- 判定依据(字典 CLC_STEP_VERDICT;INV-38 终态必填)
    exit_code INTEGER,                                -- 退出码(本地链路终态必填;远程链路以回执为准可空)
    started_at TEXT,                                  -- 起始时间(进入 RUNNING 时写入)
    ended_at TEXT,                                    -- 结束时间(INV-38 终态必填)
    log_start_line INTEGER,                           -- 日志游标区间起点(回放定位用,不另建日志副本)
    log_end_line INTEGER,                             -- 日志游标区间终点(随执行推进右移)
    fail_detail VARCHAR(500),                         -- 失败摘要(INV-38 FAILED 必填;落库前掩码)
    skip_reason VARCHAR(255),                         -- 跳过原因(INV-38 SKIPPED 必填;续跑固定「上次任务该步已成功(INV-03 不重跑)」)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_step_task_index ON clc_task_step (task_ref, step_index) WHERE flag = 1;
CREATE INDEX IF NOT EXISTS idx_step_task_status ON clc_task_step (task_ref, step_status);

-- ============ AGG-CLC-06 内嵌子实体:交付日志(CLC-CLC-06-S2) ============
CREATE TABLE IF NOT EXISTS clc_task_log (
    id BIGSERIAL PRIMARY KEY,
    task_ref VARCHAR(50) NOT NULL,                    -- 所属任务(clc_task.task_no;级联从属)
    line_no INTEGER NOT NULL,                         -- 日志序号(INV-43 任务内单调递增、只增不改;断线回放续传起点)
    step_index INTEGER,                               -- 所属步骤(空 = 任务级输出:展开、收尾、取消、失联判定)
    logged_at TEXT NOT NULL,                          -- 记录时间
    log_level VARCHAR(10) NOT NULL,                   -- 日志级别(字典 CLC_LOG_LEVEL)
    content TEXT NOT NULL,                            -- 日志内容(INV-42 落库前完成凭据掩码;超长截断并标注)
    created_by BIGINT,
    created_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    updated_by BIGINT,
    updated_at TEXT DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS'),
    flag SMALLINT DEFAULT 1
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_log_task_line ON clc_task_log (task_ref, line_no) WHERE flag = 1;
CREATE INDEX IF NOT EXISTS idx_log_task_level ON clc_task_log (task_ref, log_level);
CREATE INDEX IF NOT EXISTS idx_log_logged_at ON clc_task_log (logged_at);

-- 交付执行平台参数初值(REF-14 / REF-15 模型声明缺省值;幂等)
INSERT INTO clc_platform_param (param_key, param_value, param_group, description) VALUES
    ('clc.exec_slot_limit', '2', 'delivery', 'REF-14 交付执行全局并发槽位上限(占满 ⇒ 新任务排队 QUEUED 而不是失败,R-18)'),
    ('clc.step_timeout_minutes', '45', 'delivery', 'REF-15 单步骤超时时长(分钟;超时按失败结论落步骤行)'),
    ('clc.log_retention_days', '14', 'delivery', 'REF-15 交付日志保留期(天;B-AUTO-13 到期清理留痕,INV-44 清理不影响任务与步骤台账)')
ON CONFLICT (param_key) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS clc_task_log;
DROP TABLE IF EXISTS clc_task_step;
DELETE FROM clc_platform_param WHERE param_key IN ('clc.exec_slot_limit', 'clc.step_timeout_minutes', 'clc.log_retention_days');
