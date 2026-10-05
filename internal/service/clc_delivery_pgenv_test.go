// clc_delivery_pgenv_test.go —— 临时 PostgreSQL 测试实例的环境自举(仅测试构建,不改生产代码)。
//
// 两个本机实测坑(clc_delivery_pg_test.go 的装置结构不动,故在此补齐):
//  1. PATH 上的 initdb 可能来自 libpq(客户端 formula)符号链接,同目录无 postgres 服务端
//     二进制 ⇒ initdb 直接失败;需优先选中带完整服务端工具集的目录。
//  2. pg_ctl 由本进程(多线程 Go 进程)派生且无有效 locale 环境变量时,
//     postmaster 以「became multithreaded during startup」终止 ⇒ 预置 LC_ALL。
package service

import (
	"os"
	"path/filepath"
	"sort"
)

// pgServerToolset 一个可用的 PostgreSQL 服务端工具集所需的可执行文件名。
var pgServerToolset = []string{"initdb", "pg_ctl", "createdb", "postgres"}

func init() {
	if os.Getenv("CLC_TEST_PG_DSN") != "" {
		return // 指向既有实例,无需本机装置
	}
	if os.Getenv("LC_ALL") == "" {
		_ = os.Setenv("LC_ALL", "C")
	}
	if hasToolsetIn(dirOf(firstOnPath("initdb"))) {
		return
	}
	for _, dir := range candidatePGServerDirs() {
		if hasToolsetIn(dir) {
			_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			return
		}
	}
}

// dirOf 文件所在目录(空串原样返回)。
func dirOf(exe string) string {
	if exe == "" {
		return ""
	}
	return filepath.Dir(exe)
}

// firstOnPath PATH 上首个命中的可执行文件绝对路径。
func firstOnPath(name string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		if p := filepath.Join(dir, name); fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0
}

// hasToolsetIn 给定目录内是否具备全套服务端工具集
// (initdb 与 postgres 必须同目录,否则 initdb 运行期找不到 postgres)。
func hasToolsetIn(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range pgServerToolset {
		if !fileExists(filepath.Join(dir, name)) {
			return false
		}
	}
	return true
}

// candidatePGServerDirs 常见 Homebrew / 官方安装包的服务端 bin 目录(有序,可复现)。
func candidatePGServerDirs() []string {
	globs := []string{
		"/opt/homebrew/opt/postgresql@*/bin",
		"/opt/homebrew/opt/postgresql/bin",
		"/opt/homebrew/Cellar/postgresql@*/*/bin",
		"/opt/homebrew/Cellar/postgresql/*/*/bin",
		"/usr/local/opt/postgresql@*/bin",
		"/usr/local/Cellar/postgresql@*/*/bin",
		"/Library/PostgreSQL/*/bin",
	}
	var out []string
	seen := map[string]bool{}
	for _, g := range globs {
		matches, err := filepath.Glob(g)
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}
