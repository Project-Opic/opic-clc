package delivery

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// 步骤 → 命令面映射为代码内固定契约(REF-13),不入字典、不可配置:
// 安装类步骤序号即 `setup <cluster> <01..07>`;其余动作为固定命令字。
var fixedCommandWords = map[string]string{
	"START":      "start",
	"STOP":       "stop",
	"UPGRADE":    "upgrade",
	"BACKUP":     "backup",
	"RESTORE":    "restore",
	"DESTROY":    "destroy",
	"CERT_RENEW": "kca-renew",
	"ADD_NODE":   "add-node",
	"DEL_NODE":   "del-node",
}

var (
	clusterIDRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	installCodeRe = regexp.MustCompile(`^0[1-7]$`)
	commandRe     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// shellMetaRe 命令参数禁止的 shell 元字符(INV-47:不经 shell 直调,纵深防御)。
	shellMetaRe = regexp.MustCompile("[;&|`$<>()\\[\\]{}*?!~#'\"\\\\#\\n\\r\\t]")
)

// ValidateToken 三类入参各自的格式白名单(INV-47):
// 集群标识 / 步骤序号 / 节点地址必须命中格式后才允许进入参数数组。
func ValidateToken(kind, value string) error {
	switch kind {
	case "cluster":
		if !clusterIDRe.MatchString(value) {
			return fmt.Errorf("集群标识「%s」不符合格式白名单(字母数字与 . _ -,1-64 位)", value)
		}
	case "step":
		if !installCodeRe.MatchString(value) {
			return fmt.Errorf("安装步骤序号「%s」不在固定契约取值域(01~07)", value)
		}
	case "node":
		if ip := strings.TrimSpace(value); ip == "" || net.ParseIP(ip) == nil {
			return fmt.Errorf("节点地址「%s」不是合法 IP", value)
		}
	case "command":
		if !commandRe.MatchString(value) {
			return fmt.Errorf("命令字「%s」不符合固定契约", value)
		}
	default:
		return fmt.Errorf("未知入参类型 %s", kind)
	}
	return nil
}

// checkArgvSafety 参数数组逐元素拒绝 shell 元字符(INV-47;命令注入尝试在此拦截)。
func checkArgvSafety(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("命令数组不得为空")
	}
	for _, a := range argv {
		if shellMetaRe.MatchString(a) {
			return fmt.Errorf("命令参数「%s」含 shell 元字符,已拒绝(直调不经 shell,INV-47)", a)
		}
	}
	return nil
}

// BuildStepArgs 按 REF-13 固定契约渲染单步命令数组(含 argv[0]=可执行文件路径,路径来自配置值)。
//
// kind:任务类别(CLC_TASK_KIND);stepCode:安装类取 CLC_INSTALL_STEP 编码(01~07),
// 其余类别取与 kind 同名的单步编码;m1 ClusterTaskStep.stepCode)。nodeIPs 仅节点增删类使用。
func BuildStepArgs(toolBin, kind, clusterID, stepCode string, nodeIPs []string) ([]string, error) {
	if err := ValidateToken("cluster", clusterID); err != nil {
		return nil, err
	}
	switch kind {
	case "INSTALL", "RETRY_INSTALL":
		if err := ValidateToken("step", stepCode); err != nil {
			return nil, err
		}
		return safe(toolBin, "setup", clusterID, stepCode)
	case "START", "STOP", "UPGRADE", "BACKUP", "RESTORE", "DESTROY", "CERT_RENEW":
		if stepCode != kind {
			return nil, fmt.Errorf("非安装类任务的单步编码必须与任务类别同名(REF-13):收到 %s/%s", kind, stepCode)
		}
		return safe(toolBin, fixedCommandWords[kind], clusterID)
	case "ADD_NODE", "DEL_NODE":
		if stepCode != kind {
			return nil, fmt.Errorf("非安装类任务的单步编码必须与任务类别同名(REF-13):收到 %s/%s", kind, stepCode)
		}
		if len(nodeIPs) == 0 {
			return nil, fmt.Errorf("节点增删步骤必须携带节点地址")
		}
		argv := []string{toolBin, fixedCommandWords[kind], clusterID}
		for _, ip := range nodeIPs {
			if err := ValidateToken("node", ip); err != nil {
				return nil, err
			}
			argv = append(argv, strings.TrimSpace(ip))
		}
		return check(argv)
	default:
		return nil, fmt.Errorf("任务类别「%s」无固定命令契约(REF-13)", kind)
	}
}

func safe(toolBin string, words ...string) ([]string, error) {
	return check(append([]string{toolBin}, words...))
}

func check(argv []string) ([]string, error) {
	if err := checkArgvSafety(argv); err != nil {
		return nil, err
	}
	return argv, nil
}

// RenderCommandSummary 命令摘要:由已校验参数数组渲染的动作 + 参数骨架(INV-40/47,
// 不得由用户输入拼接;凭据不落此面,调用方仍须掩码兜底)。
func RenderCommandSummary(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	parts := make([]string, 0, len(argv)-1)
	for _, a := range argv[1:] {
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
