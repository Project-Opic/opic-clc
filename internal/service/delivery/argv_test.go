package delivery

import (
	"strings"
	"testing"
)

func TestBuildStepArgsInstallContract(t *testing.T) {
	// REF-13:安装步骤 = setup <cluster> <01..07> 固定契约,argv[0]=配置的可执行文件路径
	argv, err := BuildStepArgs("/opt/delivery/tool", "INSTALL", "CLU-20261005-0001", "03", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/opt/delivery/tool", "setup", "CLU-20261005-0001", "03"}
	if strings.Join(argv, "|") != strings.Join(want, "|") {
		t.Fatalf("命令面不符 REF-13: %v", argv)
	}
	for _, code := range []string{"01", "02", "03", "04", "05", "06", "07"} {
		if _, err := BuildStepArgs("/bin/true", "RETRY_INSTALL", "c-1", code, nil); err != nil {
			t.Fatalf("步骤 %s 应合法: %v", code, err)
		}
	}
	if _, err := BuildStepArgs("/bin/true", "INSTALL", "c-1", "08", nil); err == nil {
		t.Fatal("步骤序号 08 应被取值域拦截(INV-47)")
	}
	if _, err := BuildStepArgs("/bin/true", "INSTALL", "c-1", "1", nil); err == nil {
		t.Fatal("步骤序号非两位编码应被拦截")
	}
}

func TestBuildStepArgsFixedCommandWords(t *testing.T) {
	cases := map[string][]string{
		"START":      {"start", "c-1"},
		"STOP":       {"stop", "c-1"},
		"UPGRADE":    {"upgrade", "c-1"},
		"BACKUP":     {"backup", "c-1"},
		"RESTORE":    {"restore", "c-1"},
		"DESTROY":    {"destroy", "c-1"},
		"CERT_RENEW": {"kca-renew", "c-1"},
	}
	for kind, want := range cases {
		argv, err := BuildStepArgs("/bin/tool", kind, "c-1", kind, nil)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if strings.Join(argv[1:], " ") != strings.Join(want, " ") {
			t.Fatalf("%s 命令字不符: %v", kind, argv)
		}
	}
	// 节点类携带 IP 白名单校验
	argv, err := BuildStepArgs("/bin/tool", "ADD_NODE", "c-1", "ADD_NODE", []string{"10.0.0.8"})
	if err != nil || strings.Join(argv[1:], " ") != "add-node c-1 10.0.0.8" {
		t.Fatalf("ADD_NODE 命令面不符: %v %v", argv, err)
	}
	if _, err := BuildStepArgs("/bin/tool", "ADD_NODE", "c-1", "ADD_NODE", []string{"evil; rm -rf /"}); err == nil {
		t.Fatal("非法节点地址应被拒绝(INV-47)")
	}
	if _, err := BuildStepArgs("/bin/tool", "ADD_NODE", "c-1", "ADD_NODE", nil); err == nil {
		t.Fatal("节点增删缺节点地址应被拒绝")
	}
	if _, err := BuildStepArgs("/bin/tool", "NO_SUCH_KIND", "c-1", "NO_SUCH_KIND", nil); err == nil {
		t.Fatal("无固定契约的类别必须拒绝(REF-13)")
	}
}

func TestInjectionRejected(t *testing.T) {
	// 命令注入尝试:集群标识 / 参数一律格式白名单拦截(INV-47)
	injects := []string{
		"c-1; rm -rf /", "c-1 && curl evil", "c-1|tee", "$(id)", "`id`", "c-1\nrm -rf /",
		"c-1 > /etc/passwd", "c-1*", "c-1'", `c-1"`,
	}
	for _, bad := range injects {
		if _, err := BuildStepArgs("/bin/tool", "START", bad, "START", nil); err == nil {
			t.Fatalf("注入尝试未被拦截: %q", bad)
		}
	}
}

func TestRenderCommandSummarySkeleton(t *testing.T) {
	s := RenderCommandSummary([]string{"/opt/tool", "setup", "CLU-1", "04"})
	if s != "setup CLU-1 04" {
		t.Fatalf("命令摘要应为动作+参数骨架: %q", s)
	}
	if RenderCommandSummary(nil) != "" {
		t.Fatal("空数组摘要应为空")
	}
}

func TestMaskSecretIrreversible(t *testing.T) {
	// INV-40/42:私钥、口令赋值、Bearer、JWT、选项口令一律掩码;掩码后原文不可还原
	secrets := []string{
		"-----BEGIN RSA PRIVATE KEY-----",
		"password=Sup3rSecret!",
		"token: abcdefghijklmnop",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJvcGljIn0.abcdefGHIJKLmnop",
		"--password hunter2 connect host",
	}
	for _, line := range secrets {
		got := MaskSecret(line)
		if got == line || strings.Contains(got, "Sup3rSecret") || strings.Contains(got, "hunter2") {
			t.Fatalf("凭据未被掩码: %q -> %q", line, got)
		}
		if !strings.Contains(got, maskMark) {
			t.Fatalf("掩码标记缺失: %q", got)
		}
		if MaskSecret(got) != got {
			t.Fatalf("掩码不幂等: %q", got)
		}
	}
	plain := "PLAY RECAP host-1 : ok=12 changed=3 unreachable=0 failed=0"
	if MaskSecret(plain) != plain {
		t.Fatalf("正常输出被误掩: %q", MaskSecret(plain))
	}
}

func TestRecapTracker(t *testing.T) {
	var tr RecapTracker
	lines := []string{
		"TASK [prepare] *********",
		"",
		"PLAY RECAP ****",
		"node-1  : ok=10 changed=4 unreachable=0 failed=0 skipped=1",
		"node-2  : ok=8 changed=2 unreachable=0 failed=1 skipped=0",
		"",
		"post-run completed",
	}
	failLineSeen := false
	for _, l := range lines {
		if tr.Feed(l) {
			failLineSeen = true
		}
	}
	if !tr.Seen() || tr.hosts != 2 || tr.Failed() != 1 || !failLineSeen {
		t.Fatalf("recap 判定不符: seen=%v hosts=%d failed=%d failLine=%v", tr.Seen(), tr.hosts, tr.Failed(), failLineSeen)
	}
	var ok2 RecapTracker
	ok2.Feed("PLAY RECAP")
	ok2.Feed("node-1  : ok=10 changed=4 unreachable=0 failed=0")
	if !ok2.Seen() || ok2.Failed() != 0 {
		t.Fatal("全成功 recap 应判 0 失败")
	}
	var none RecapTracker
	none.Feed("no recap here")
	if none.Seen() {
		t.Fatal("未出现 recap 不应误判")
	}
}

func TestSlotsQueueWhenFull(t *testing.T) {
	// R-18/REF-14:全局槽位=2,占满 ⇒ TryAcquire 失败(任务保持 QUEUED 而不是失败)
	s := NewSlots(2)
	if !s.TryAcquire() || !s.TryAcquire() {
		t.Fatal("前两个槽位应可取得")
	}
	if s.TryAcquire() {
		t.Fatal("槽位占满时第三请求必须失败排队")
	}
	s.Release()
	if !s.TryAcquire() {
		t.Fatal("释放后应可取得")
	}
}

func TestClusterLockMutex(t *testing.T) {
	// INV-48:同集群互斥
	l := NewClusterLock()
	if ok, _ := l.TryLock("CLU-1", "TSK-1"); !ok {
		t.Fatal("首个任务应取得集群执行权")
	}
	if ok, holder := l.TryLock("CLU-1", "TSK-2"); ok || holder != "TSK-1" {
		t.Fatal("同集群第二任务必须被拒并回占用者")
	}
	if ok, _ := l.TryLock("CLU-2", "TSK-3"); !ok {
		t.Fatal("不同集群互不阻塞")
	}
	l.Unlock("CLU-1", "TSK-2") // 非持有者释放无效
	if _, ok := l.Holder("CLU-1"); !ok {
		t.Fatal("非持有者不得解锁")
	}
	l.Unlock("CLU-1", "TSK-1")
	if ok, _ := l.TryLock("CLU-1", "TSK-2"); !ok {
		t.Fatal("释放后应可再取得")
	}
}

func TestValidateTokenWhitelist(t *testing.T) {
	if err := ValidateToken("cluster", "CLU-20261005-0001"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToken("cluster", "中文集群"); err == nil {
		t.Fatal("集群标识必须命中格式白名单(INV-47)")
	}
	if err := ValidateToken("node", "192.168.3.200"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToken("node", "192.168.3.200; echo"); err == nil {
		t.Fatal("节点地址必须命中 IP 格式")
	}
	if err := ValidateToken("command", "setup"); err != nil {
		t.Fatal(err)
	}
}
