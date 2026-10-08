// 测试装置:临时 PostgreSQL 实例(initdb/pg_ctl 自起自停,或 CLC_TEST_PG_DSN 指向既有实例)
// + goose 迁移(真实 migrations/0000NN),服务层语义用例(clc_delivery_test.go)依赖本装置。
package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/store"
)

var testDBReady bool

func TestMain(m *testing.M) {
	cleanup, err := startTestDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[clc_delivery_test] 测试库不可用,DB 依赖用例跳过: %v\n", err)
	} else {
		testDBReady = true
		defer cleanup()
	}
	code := m.Run()
	os.Exit(code)
}

func startTestDB() (func(), error) {
	dsn := os.Getenv("CLC_TEST_PG_DSN")
	if dsn == "" {
		initdb, err1 := exec.LookPath("initdb")
		pgCtl, err2 := exec.LookPath("pg_ctl")
		createdbBin, err3 := exec.LookPath("createdb")
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("缺少 initdb/pg_ctl/createdb(本地装 PostgreSQL 或设 CLC_TEST_PG_DSN)")
		}
		base, err := os.MkdirTemp("", "clc-test-pg-")
		if err != nil {
			return nil, err
		}
		data := filepath.Join(base, "data")
		if out, err := runCmd(append(os.Environ(), "LC_ALL=C"), initdb,
			"-D", data, "-U", "postgres", "--auth=trust", "-E", "UTF8", "--no-sync"); err != nil {
			os.RemoveAll(base)
			return nil, fmt.Errorf("initdb 失败: %v\n%s", err, out)
		}
		port, err := freeTCPPort()
		if err != nil {
			os.RemoveAll(base)
			return nil, err
		}
		logFile := filepath.Join(base, "pg.log")
		if out, err := runCmd(nil, pgCtl, "-D", data, "-l", logFile, "-w", "start",
			"-o", fmt.Sprintf("-p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories=%s", port, base)); err != nil {
			out2, _ := os.ReadFile(logFile)
			os.RemoveAll(base)
			return nil, fmt.Errorf("pg_ctl start 失败: %v\n%s\n%s", err, out, out2)
		}
		cleanup := func() {
			_, _ = runCmd(nil, pgCtl, "-D", data, "-m", "fast", "stop")
			_ = os.RemoveAll(base)
		}
		if out, err := runCmd(nil, createdbBin, "-h", "127.0.0.1", "-p", fmt.Sprint(port), "-U", "postgres", "opicdb"); err != nil {
			cleanup()
			return nil, fmt.Errorf("createdb 失败: %v\n%s", err, out)
		}
		dsn = fmt.Sprintf("host=127.0.0.1 port=%d user=postgres dbname=opicdb sslmode=disable", port)
		dbCleanup, err := openAndMigrate(dsn)
		if err != nil {
			cleanup()
			return nil, err
		}
		return func() { dbCleanup(); cleanup() }, nil
	}
	return openAndMigrate(dsn)
}

func openAndMigrate(dsn string) (func(), error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("连接测试库失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(8)
	goose.SetDialect("postgres")
	goose.SetLogger(goose.NopLogger())
	if err := goose.UpContext(context.Background(), sqlDB, filepath.Join("..", "..", "migrations")); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("goose 迁移失败: %w", err)
	}
	store.DB = db
	config.Config.Ontology.ModelsDir = filepath.Join("..", "..", "models")
	// DEF-06 之后,服务层派发口(clc_wizard.go DispatchLifecycleAction → clc_task.go
	// CheckTaskKindPermission)会真读 sys_user_role/sys_role_permission。既有用例以 testUser
	// (ID=1)直调派发口并期待放行,故装置在迁移后把 m5 的 CLC-ROLE-01(集群管理员,持有全部交付
	// 动作档 PERM-FUNC-03/04/09~12/46)绑到该用例用户上。
	// 只加授权、不改任何断言;迁移 000006 已按 idempotent 方式备好 sys_role/sys_permission/
	// sys_role_permission,这里只补用户↔角色这一条边(幂等)。
	// 无牌用例(DEF-06 拒绝路径)不在此授权 —— 由用例自带专用用户 ID 天然不持牌。
	if _, err := store.Exec(nil, `
		INSERT INTO sys_user_role (user_id, role_id)
		SELECT 1, r.id FROM sys_role r
		WHERE r.code = 'CLC-ROLE-01'
		  AND NOT EXISTS (SELECT 1 FROM sys_user_role ur WHERE ur.user_id = 1 AND ur.role_id = r.id)`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("测试装置授权(CLC-ROLE-01→用例用户)失败: %w", err)
	}
	return func() { _ = sqlDB.Close() }, nil
}

func runCmd(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// ---------- 用例助手 ----------

var testSeq int64

func nextSeq() int64 { return atomic.AddInt64(&testSeq, 1) }

func requireDB(t *testing.T) {
	t.Helper()
	if !testDBReady {
		t.Skip("测试库不可用,跳过 DB 用例")
	}
}

func mustSQL(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	res := store.DB.Exec(sql, args...)
	if res.Error != nil {
		t.Fatalf("SQL 失败: %v\n%s", res.Error, sql)
	}
	return res.RowsAffected
}

func mustOne(t *testing.T, sql string, args ...any) map[string]any {
	t.Helper()
	row, err := qOne(sql, args...)
	if err != nil {
		t.Fatalf("查询失败: %v\n%s", err, sql)
	}
	return row
}

func mustList(t *testing.T, sql string, args ...any) []map[string]any {
	t.Helper()
	rows, err := qList(sql, args...)
	if err != nil {
		t.Fatalf("查询失败: %v\n%s", err, sql)
	}
	return rows
}

func seedCluster(t *testing.T, status string) string {
	t.Helper()
	no := fmt.Sprintf("CLU-T%05d", nextSeq())
	mustSQL(t, `INSERT INTO clc_cluster (cluster_no, name, status, run_mode, health) VALUES (?, ?, ?, 'MCC', 'HEALTHY')`,
		no, "测试集群 "+no, status)
	return no
}

func seedTask(t *testing.T, clusterNo, kind, status, execPath string) string {
	t.Helper()
	no := fmt.Sprintf("TSK-T%05d", nextSeq())
	mustSQL(t, `
		INSERT INTO clc_task (task_no, cluster_ref, task_kind, task_status, execution_path, submitted_by, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, 'tester', 1, 1)`, no, clusterNo, kind, status, execPath)
	return no
}

func taskStatus(t *testing.T, taskNo string) string {
	t.Helper()
	row := mustOne(t, `SELECT task_status FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
	return Str(row["task_status"])
}

func waitTaskStatuses(t *testing.T, taskNo string, want ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		row := mustOne(t, `SELECT * FROM clc_task WHERE task_no=? AND flag=1`, taskNo)
		if row != nil {
			for _, w := range want {
				if Str(row["task_status"]) == w {
					return row
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("任务 %s 未在期限内到达期望状态 %v(当前 %s)", taskNo, want, taskStatus(t, taskNo))
	return nil
}

func stepRows(t *testing.T, taskNo string) []map[string]any {
	t.Helper()
	return mustList(t, `SELECT * FROM clc_task_step WHERE task_ref=? AND flag=1 ORDER BY step_index ASC`, taskNo)
}

func stepsByStatus(t *testing.T, taskNo, status string) []map[string]any {
	t.Helper()
	return mustList(t, `SELECT * FROM clc_task_step WHERE task_ref=? AND flag=1 AND step_status=? ORDER BY step_index ASC`, taskNo, status)
}

func countRows(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	n, err := qCount(sql, args...)
	if err != nil {
		t.Fatalf("计数失败: %v %s", err, sql)
	}
	return n
}

// writeDeliveryTool 生成可执行 fixture 脚本(仅测试夹具:shebang 属夹具自身机制,
// 被测代码始终 exec.Command(argv[0], argv[1:]...) 直调,不经 shell)。
func writeDeliveryTool(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "delivery-tool")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		if !strings.Contains(s, x) {
			return false
		}
	}
	return true
}
