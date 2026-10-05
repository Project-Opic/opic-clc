package delivery

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Config 本地适配器运行前置(全部来自部署配置值:C1 纪律 —— 真实挂载路径/可执行文件路径
// 只允许出现在配置里,代码只消费配置键,不散落硬编码)。
type Config struct {
	ToolBin               string        // 交付工具可执行文件路径(配置值)
	ToolDir               string        // 交付工具工作目录(挂载目录,配置值)
	KubeconfigEnv         string        // 注入的执行面凭据环境变量名(配置值)
	KubeconfigPathPattern string        // 凭据文件路径模板,{cluster} 占位(配置值)
	ExtraEnv              []string      // 附加环境变量(配置值)
	StepTimeout           time.Duration // 单步超时(REF-15,缺省 45 分钟)
}

type runningStep struct {
	pid      int
	kill     chan struct{}
	killOnce sync.Once
}

// Local 本地链路适配器:后端容器即执行机(裁定 4),直调交付工具进程组。
type Local struct {
	cfg Config

	mu  sync.Mutex
	cur map[string]*runningStep // taskNo → 正在执行的单步(取消/失联判定用)
}

// NewLocal 构造本地适配器。
func NewLocal(cfg Config) *Local {
	return &Local{cfg: cfg, cur: map[string]*runningStep{}}
}

// Form 适配器形态(REF-16:运行期不互转)。
func (a *Local) Form() string { return FormLocal }

// Alive 进程是否可存活(信号 0 探测;B-AUTO-11 失联判定的唯一触发源)。
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// killGroup 以进程组为粒度终止(R-15:TERM 宽限后 KILL,覆盖全部子进程)。
func killGroup(pid int, grace time.Duration) {
	pgid := -pid
	if syscall.Kill(pgid, syscall.SIGTERM) != nil {
		return
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !Alive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pgid, syscall.SIGKILL)
}

// kubeconfigPath 凭据文件定位:模板渲染 + 通配兜底(执行面凭据按登记存放标识解析,不落库)。
func (a *Local) kubeconfigPath(clusterID string) string {
	if a.cfg.KubeconfigPathPattern == "" {
		return ""
	}
	rel := filepath.Clean(a.cfg.KubeconfigPathPattern)
	path := filepath.Join(a.cfg.ToolDir, replaceClusterPlaceholder(rel, clusterID))
	if matches, err := filepath.Glob(path); err == nil && len(matches) > 0 {
		return matches[0]
	}
	return path
}

func replaceClusterPlaceholder(p, clusterID string) string {
	return replaceAll(p, "{cluster}", clusterID)
}

func replaceAll(s, old, new string) string {
	out := make([]byte, 0, len(s))
	for {
		i := indexOf(s, old)
		if i < 0 {
			out = append(out, s...)
			return string(out)
		}
		out = append(out, s[:i]...)
		out = append(out, new...)
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Execute 直调交付工具(无 shell:exec.Command(argv[0], argv[1:]...),SysProcAttr.Setpgid),
// 逐行采集 stdout/stderr 至 sink,喂 recap 判定器,按退出码 + 剧本汇总给结论;
// ctx 取消 → 进程组终止并返回 Cancelled;超时 → 按失败落行(REF-15)。
func (a *Local) Execute(ctx context.Context, ref StepRef, sink LineSink) Outcome {
	if len(ref.Args) == 0 {
		return Outcome{Detail: "命令数组为空,拒绝执行(INV-47)"}
	}
	if err := checkArgvSafety(ref.Args); err != nil {
		return Outcome{Detail: err.Error()} // 纵深防御:直调入口二次拒绝 shell 元字符(INV-47)
	}
	timeout := a.cfg.StepTimeout
	if timeout <= 0 {
		timeout = 45 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(ref.Args[0], ref.Args[1:]...) // 参数数组直调,绝不经过 shell(INV-47)
	cmd.Env = a.stepEnv(ref.ClusterID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Outcome{Lost: true, Detail: "标准输出管道失败: " + err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Outcome{Lost: true, Detail: "标准错误管道失败: " + err.Error()}
	}
	if err := cmd.Start(); err != nil {
		// 进程不可存活 = 失联(B-AUTO-11),不是失败,也不静默回退
		return Outcome{Lost: true, Detail: "交付工具进程不可存活: " + err.Error()}
	}

	step := &runningStep{pid: cmd.Process.Pid, kill: make(chan struct{})}
	a.bind(ref.TaskNo, step)
	defer a.unbind(ref.TaskNo, step)

	var mu sync.Mutex
	var recap RecapTracker
	emit := func(level, text string) {
		if sink != nil {
			sink(Line{Level: level, Text: text})
		}
	}
	lineFeed := func(text string, baseLevel string) {
		mu.Lock()
		isFailRecap := recap.Feed(text)
		mu.Unlock()
		level := baseLevel
		if isFailRecap {
			level = LevelError
		}
		emit(level, text)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanLines(stdout, func(l string) { lineFeed(l, LevelInfo) })
	}()
	go func() {
		defer wg.Done()
		scanLines(stderr, func(l string) { lineFeed(l, LevelWarn) })
	}()

	errCh := make(chan error, 1)
	// cmd.Wait 会关闭 StdoutPipe/StderrPipe 交出的读端描述符 ⇒ 采集协程必须先读到 EOF,
	// 否则末行(往往是 PLAY RECAP 汇总行)随描述符关闭一并丢失(B-AUTO-10 / INV-42 / REP-13 不漏行)。
	go func() {
		wg.Wait()
		errCh <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-errCh:
	case <-step.kill: // 外部强制终止(FUNC-33 取消链)
		killGroup(step.pid, 3*time.Second)
		waitErr = <-errCh
	case <-runCtx.Done(): // 超时或被取消:先终止进程组,再收退出结论
		killGroup(step.pid, 3*time.Second)
		waitErr = <-errCh
	}
	wg.Wait() // errCh 已隐含采集收束,此处为幂等兜底
	_ = stdout.Close()
	_ = stderr.Close()

	exit := ExitCodeOf(waitErr)
	mu.Lock()
	seen, summary, failed := recap.Seen(), recap.Summary(), recap.Failed()
	mu.Unlock()

	switch {
	case wasCancelled(ctx):
		return Outcome{Cancelled: true, ExitCode: exit, Detail: "人工取消:进程组已终止(R-15)"}
	case wasDeadline(runCtx, ctx):
		return Outcome{TimedOut: true, ExitCode: exit,
			Detail: "单步超时(REF-15):进程组已终止,按失败结论落步骤行"}
	}

	verdict := VerdictExitCode
	success := exit == 0
	detail := ""
	if seen {
		verdict = VerdictPlayRecap
		success = success && failed == 0
		if failed > 0 {
			detail = "剧本汇总:" + summary
		}
	}
	if !success && detail == "" {
		switch {
		case exit < 0:
			detail = "执行进程异常终止(信号)"
		case exit != 0:
			detail = "退出码 " + itoa(exit)
		}
	}
	return Outcome{Success: success, Verdict: verdict, ExitCode: exit, Detail: detail}
}

func (a *Local) stepEnv(clusterID string) []string {
	env := os.Environ()
	env = append(env, a.cfg.ExtraEnv...)
	if a.cfg.KubeconfigEnv != "" {
		if p := a.kubeconfigPath(clusterID); p != "" {
			env = append(env, a.cfg.KubeconfigEnv+"="+p)
		}
	}
	return env
}

func (a *Local) bind(taskNo string, s *runningStep) {
	a.mu.Lock()
	a.cur[taskNo] = s
	a.mu.Unlock()
}

func (a *Local) unbind(taskNo string, s *runningStep) {
	a.mu.Lock()
	if cur, ok := a.cur[taskNo]; ok && cur == s {
		delete(a.cur, taskNo)
	}
	a.mu.Unlock()
}

// Terminate 外部强制终止该任务的当前单步进程组(FUNC-33 取消链的兜底通道;幂等)。
func (a *Local) Terminate(taskNo string) bool {
	a.mu.Lock()
	s, ok := a.cur[taskNo]
	a.mu.Unlock()
	if !ok {
		return false
	}
	s.killOnce.Do(func() { close(s.kill) })
	killGroup(s.pid, 3*time.Second)
	return true
}

func wasCancelled(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

func wasDeadline(runCtx, parent context.Context) bool {
	return errors.Is(runCtx.Err(), context.DeadlineExceeded) && parent.Err() == nil
}

// scanLines 逐行采集(缓冲放大到 1MB,防超长行断流)。
func scanLines(r io.Reader, fn func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		fn(sc.Text())
	}
}

// ExitCodeOf 退出码解析:未启动/被信号杀死返回 -1(与 ksk8s 参考口径一致)。
func ExitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				return -1
			}
			return ws.ExitStatus()
		}
	}
	return -1
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
