package delivery

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeTool 生成可执行 fixture 脚本(仅测试夹具:shebang 属夹具自身机制,
// 被测代码始终 exec.Command(argv[0], argv[1:]...) 直调,不经 shell)。
func writeTool(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "delivery-tool")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

type sinkCapture struct {
	mu    sync.Mutex
	lines []Line
}

func (c *sinkCapture) On(l Line) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, l)
}

func (c *sinkCapture) Text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, l := range c.lines {
		b.WriteString(l.Level)
		b.WriteByte(' ')
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func testCfg(tool string) Config {
	return Config{ToolBin: tool, StepTimeout: 30 * time.Second}
}

// selfPidTool 夹具:把自身 PID 写入 pidFile 后长睡(验证进程组终止)。
func selfPidTool(t *testing.T) (string, string) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pid")
	tool := writeTool(t, "echo $$ > "+pidFile+"\nsleep 30")
	return tool, pidFile
}

// waitPid 等待夹具写入自身 PID(避免与子进程 exec 竞速)。
func waitPid(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(pidFile); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(raw))); pid > 0 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("夹具未在期限内登记自身 PID")
	return 0
}

// waitGone 等待 PID 不可存活 —— 进程组已终止的正证据(B-AUTO-11 / R-15 判据)。
func waitGone(t *testing.T, pidFile string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var pid int
	for {
		if pid == 0 {
			if raw, err := os.ReadFile(pidFile); err == nil {
				pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
			}
		}
		if pid > 0 && !Alive(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("PID %d 在进程组终止后仍存活", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestLocalExecutesRecapSuccess(t *testing.T) {
	tool := writeTool(t, `
echo "TASK [step] ok"
echo "PLAY RECAP ****"
echo "node-1  : ok=10 changed=4 unreachable=0 failed=0 skipped=0"
echo "node-2  : ok=8 changed=2 unreachable=0 failed=0 skipped=0"
exit 0`)
	a := NewLocal(testCfg(tool))
	cap := &sinkCapture{}
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-1", ClusterID: "CLU-1", StepIndex: 1, StepCode: "01",
		Args: []string{tool, "setup", "CLU-1", "01"}}, cap.On)
	if !out.Success || out.Verdict != VerdictPlayRecap || out.ExitCode != 0 {
		t.Fatalf("recap 全成功应判成功: %+v", out)
	}
	if !strings.Contains(cap.Text(), "node-1") {
		t.Fatal("逐行输出必须进 sink")
	}
}

func TestLocalRecapFailureOverridesExitZero(t *testing.T) {
	tool := writeTool(t, `
echo "PLAY RECAP ****"
echo "node-1  : ok=5 changed=1 unreachable=0 failed=2 skipped=0"
exit 0`)
	a := NewLocal(testCfg(tool))
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-2", ClusterID: "CLU-1", Args: []string{tool, "setup", "CLU-1", "04"}}, nil)
	if out.Success {
		t.Fatal("recap failed>0 必须判失败")
	}
	if out.Verdict != VerdictPlayRecap || !strings.Contains(out.Detail, "2 个失败") {
		t.Fatalf("失败结论应带剧本汇总依据: %+v", out)
	}
}

func TestLocalStderrWarnLevel(t *testing.T) {
	tool := writeTool(t, `echo "warn line" 1>&2; exit 0`)
	a := NewLocal(testCfg(tool))
	cap := &sinkCapture{}
	a.Execute(context.Background(), StepRef{TaskNo: "TSK-W", Args: []string{tool}}, cap.On)
	if !strings.Contains(cap.Text(), "WARN warn line") {
		t.Fatalf("stderr 行应记 WARN 级别(CLC_LOG_LEVEL): %q", cap.Text())
	}
}

func TestLocalExitCodeOnly(t *testing.T) {
	tool := writeTool(t, `echo "boom"; exit 3`)
	a := NewLocal(testCfg(tool))
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-3", ClusterID: "CLU-1", Args: []string{tool, "setup", "CLU-1", "05"}}, nil)
	if out.Success || out.Verdict != VerdictExitCode || out.ExitCode != 3 {
		t.Fatalf("无 recap 时按退出码判定: %+v", out)
	}
}

func TestLocalArgsPassThroughVerbatim(t *testing.T) {
	tool := writeTool(t, `printf 'argc=%s\n' "$#"; for a in "$@"; do printf 'arg=[%s]\n' "$a"; done; exit 0`)
	a := NewLocal(testCfg(tool))
	cap := &sinkCapture{}
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-4", ClusterID: "CLU-1",
		Args: []string{tool, "setup", "a b c", "03"}}, cap.On)
	if !out.Success {
		t.Fatalf("直调应成功: %+v", out)
	}
	txt := cap.Text()
	// argv 原样传递:含空格的单参数不被再切分,不经 shell 二次解析
	if !strings.Contains(txt, "argc=3") || !strings.Contains(txt, "arg=[a b c]") {
		t.Fatalf("参数数组未按原样直调:\n%s", txt)
	}
}

func TestLocalRejectsShellMetachars(t *testing.T) {
	a := NewLocal(testCfg("/bin/true"))
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-5", Args: []string{"/bin/echo", "x; touch /tmp/opic-inject-proof"}}, nil)
	if out.Success {
		t.Fatal("含 shell 元字符的参数数组必须被直调入口拒绝(INV-47)")
	}
	if _, err := os.Stat("/tmp/opic-inject-proof"); err == nil {
		t.Fatal("注入落盘!安全红线事故")
	}
}

func TestLocalLostWhenProcessNotSurvivable(t *testing.T) {
	a := NewLocal(testCfg("/nonexistent/delivery-tool"))
	out := a.Execute(context.Background(), StepRef{TaskNo: "TSK-6", Args: []string{"/nonexistent/delivery-tool", "setup", "CLU-1", "01"}}, nil)
	if !out.Lost || out.Success {
		t.Fatalf("进程不可存活应判失联(B-AUTO-11),不是失败: %+v", out)
	}
}

func TestLocalTimeoutKillsGroup(t *testing.T) {
	tool, pidFile := selfPidTool(t)
	a := NewLocal(Config{ToolBin: tool, StepTimeout: 2 * time.Second})
	done := make(chan Outcome, 1)
	go func() { done <- a.Execute(context.Background(), StepRef{TaskNo: "TSK-7", Args: []string{tool}}, nil) }()
	select {
	case out := <-done:
		if !out.TimedOut || out.Success {
			t.Fatalf("超时必须按失败结论落行(REF-15): %+v", out)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("超时后 Execute 未返回(进程组未被终止?)")
	}
	waitGone(t, pidFile)
}

func TestLocalCancelKillsGroup(t *testing.T) {
	tool, pidFile := selfPidTool(t)
	a := NewLocal(Config{ToolBin: tool, StepTimeout: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Outcome, 1)
	go func() { done <- a.Execute(ctx, StepRef{TaskNo: "TSK-8", Args: []string{tool}}, nil) }()
	waitPid(t, pidFile)
	cancel()
	select {
	case out := <-done:
		if !out.Cancelled || out.Success {
			t.Fatalf("取消必须终止进程组并给出取消结论(R-15): %+v", out)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("取消后 Execute 未返回")
	}
	waitGone(t, pidFile)
}

func TestLocalTerminateByTask(t *testing.T) {
	tool, pidFile := selfPidTool(t)
	a := NewLocal(Config{ToolBin: tool, StepTimeout: time.Minute})
	done := make(chan Outcome, 1)
	go func() { done <- a.Execute(context.Background(), StepRef{TaskNo: "TSK-9", Args: []string{tool}}, nil) }()
	waitPid(t, pidFile)
	if !a.Terminate("TSK-9") {
		t.Fatal("运行中任务应可被终止")
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("Terminate 未终止执行")
	}
	waitGone(t, pidFile)
	if a.Terminate("TSK-9") {
		t.Fatal("已结束的步骤不应仍在执行表")
	}
}

func TestAlive(t *testing.T) {
	if Alive(0) || Alive(-1) {
		t.Fatal("非正 PID 必判不可存活")
	}
	if !Alive(os.Getpid()) {
		t.Fatal("本进程 PID 应可存活")
	}
}

func TestExitCodeOf(t *testing.T) {
	if got := ExitCodeOf(nil); got != 0 {
		t.Fatalf("nil err 退出码应为 0,得到 %d", got)
	}
}
