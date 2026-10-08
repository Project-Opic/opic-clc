-- cluster-center-mcc(OPIC 集群域 CLC)易用性增补 DDL —— goose 迁移 000008(v1.2 只读代理增补)
-- 由 m2 FUNC-47 与 m3 REF-18 派生:
--   节点 SSH 登录标识复制(FUNC-47)的展示口径 = 登录用户@节点地址,
--   登录用户取「平台配置的 SSH 全局变量口径」—— 与 FUNC-01 第二步裸机模式同源
--   (裸机添加节点不建 SSH 用户字段、凭据零持久化,登录用户是平台级全局变量而非节点属性)。
-- 口径:
--   * 参数键沿用 clc_platform_param(REF-01~15 已建立的口径),不新建表、不新增配置机制;
--   * 缺省值 root 是裸机接管场景的常识值,不代表任何凭据;私钥/口令/口令指纹一律不入库(INV-08);
--   * FUNC-47 复制行为不留痕(与 FUNC-49 逐次留痕刻意分档);本件不引入审计字段;
--   * 平台参数保存(FUNC-30)的白名单已在 service 层扩展同键,平台运维管理员可维护。
-- 幂等:ON CONFLICT 保证重复执行安全;Down 仅收回本次新增键,不影响其他平台参数。
-- +goose Up
INSERT INTO clc_platform_param (param_key, param_value, param_group, description) VALUES
    ('clc.ssh_login_user', 'root', 'ssh', 'REF-18 节点 SSH 登录标识的登录用户口径(全局变量;与 FUNC-01 第二步裸机模式同源;私钥/口令一律不入库,INV-08)')
ON CONFLICT (param_key) DO NOTHING;

-- +goose Down
DELETE FROM clc_platform_param WHERE param_key = 'clc.ssh_login_user';
