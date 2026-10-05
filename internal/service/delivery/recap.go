package delivery

import (
	"regexp"
	"strconv"
	"strings"
)

// 剧本汇总(recap)判定器 —— 移植参考 worker 形态:汇总标记行之后的
// 「<host> : ok=N changed=N ... failed=N unreachable=N」逐主机行聚合出结论;
// failed>0 即失败(判定依据 PLAY_RECAP,B-AUTO-10 / A13)。
var (
	recapHeaderRe = regexp.MustCompile(`(?i)^\s*(PLAY|TASK) RECAP\b`)
	recapLineRe   = regexp.MustCompile(`^\s*\S+\s*:\s*ok=\d+`)
	failedRe      = regexp.MustCompile(`failed=(\d+)`)
	unreachRe     = regexp.MustCompile(`unreachable=(\d+)`)
)

// RecapTracker 流式逐行喂入(单 goroutine 或外部加锁;local 适配器内部串行化喂入)。
type RecapTracker struct {
	inBlock bool
	hosts   int
	failed  int
}

// Feed 喂入一行;返回该行是否为 recap 汇总行(供 ERROR 级别判定)。
func (t *RecapTracker) Feed(line string) bool {
	if recapHeaderRe.MatchString(line) {
		t.inBlock = true
		return false
	}
	if !t.inBlock {
		return false
	}
	if !recapLineRe.MatchString(line) {
		t.inBlock = false
		return false
	}
	t.hosts++
	any := false
	if m := failedRe.FindStringSubmatch(line); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			t.failed += n
			any = true
		}
	}
	if m := unreachRe.FindStringSubmatch(line); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			t.failed += n
			any = true
		}
	}
	return any
}

// Seen 是否出现过 recap 汇总。
func (t *RecapTracker) Seen() bool { return t.hosts > 0 }

// Failed 汇总失败数(failed + unreachable)。
func (t *RecapTracker) Failed() int { return t.failed }

// Summary 汇总面(供失败摘要;不含凭据,仍由调用方掩码兜底)。
func (t *RecapTracker) Summary() string {
	if !t.Seen() {
		return ""
	}
	return strings.TrimSpace(strconv.Itoa(t.hosts) + " 个执行面主机," + strconv.Itoa(t.failed) + " 个失败")
}
