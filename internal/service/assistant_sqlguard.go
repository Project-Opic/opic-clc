// 只读 SQL 表引用词法级校验(cluster-center-mcc 审计修复)。
//
// 旧实现用「FROM/JOIN 后紧跟裸标识符」的正则做尽力而为抽取:双引号标识符、
// 注释替代空白、逗号交叉连接、派生表等合法 Postgres 变体零捕获即整体跳过
// 白名单比对(best-effort,非 fail-closed)。本文件把校验升级为词法级:
//
//  1. 先做词法分析:注释整体剥离、字符串字面量整体丢弃、引号标识符(双引号/反引号)
//     归一为裸名并统一小写;原始文本中任何位置的 FROM/JOIN 关键字都必然产出
//     词法记号,不可能被引号/注释隐藏;
//  2. 每个 FROM/JOIN(含 TABLE 查询形态)之后的逗号分隔关系项都必须解析出
//     [schema.]identifier,否则拒绝(FROM (、FROM ""、异常 token 一律 fail-closed);
//  3. WITH ... AS (...) 的 CTE 别名登记为本查询局部名,比对时放行
//     (CTE 体内的 FROM/JOIN 同样受查);
//  4. 每个表名(取末段、小写)必须命中 WhitelistedTables,出现白名单外的表
//     一律拒绝。派生表/括号连接/函数表等括号形态全部拒绝(不在只读工具范围内)。
//
// 宁可误拒也不放行;语句执行模型(只读、强制 LIMIT、审计)保持不变。
package service

import (
	"strings"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// sqlTokKind 词法记号类别。
const (
	sqlTokWord   = 'w' // 裸词(标识符/关键字,统一小写)
	sqlTokIdent  = 'q' // 引号标识符("x" / `x`,去引号并小写)
	sqlTokNumber = 'n' // 数字字面量
	sqlTokPunct  = 'p' // 标点/运算符(单字符)
)

// sqlTok 一个词法记号。
type sqlTok struct {
	kind byte
	text string
}

// sqlClauseStops FROM 关系项解析的终止词(出现即结束当前关系项列表)。
var sqlClauseStops = map[string]bool{
	"where": true, "group": true, "order": true, "having": true, "limit": true,
	"offset": true, "fetch": true, "union": true, "intersect": true, "except": true,
	"window": true, "for": true, "on": true, "using": true, "join": true,
}

// validateSQLTableRefs 词法级表白名单校验(fail-closed):任何无法解析的表引用
// 或白名单外的表名都会使整条语句被拒绝。
func validateSQLTableRefs(sqlText string, whitelist map[string]ontology.WhitelistEntry) error {
	toks := lexSQL(sqlText)
	ctes := collectCTENames(toks)
	refs, err := collectFromJoinRefs(toks)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return Fail(errcode.ErrParam, "只读 SQL 必须包含 FROM/JOIN 表引用")
	}
	for _, name := range refs {
		if ctes[name] {
			continue
		}
		if _, ok := whitelist[name]; !ok {
			return Fail(errcode.ErrParam, "表 %s 不在只读白名单内(允许:%s)", name, strings.Join(mapKeys(whitelist), ", "))
		}
	}
	return nil
}

// lexSQL 词法分析:注释整体剥离、字符串字面量整体丢弃(其内容不参与结构判定)、
// 引号标识符归一为去引号小写裸名、裸词统一小写;其余字符逐一输出为标点记号。
func lexSQL(s string) []sqlTok {
	toks := make([]sqlTok, 0, 64)
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-': // 行注释
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*': // 块注释(Postgres 允许嵌套)
			depth := 1
			i += 2
			for i < len(s) && depth > 0 {
				if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if i+1 < len(s) && s[i] == '*' && s[i+1] == '/' {
					depth--
					i += 2
					continue
				}
				i++
			}
		case c == '\'': // 字符串字面量:整体丢弃('' 与反斜杠转义均按内容处理)
			i++
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '"' || c == '`': // 引号标识符
			q := c
			i++
			var b strings.Builder
			for i < len(s) {
				if s[i] == q {
					if i+1 < len(s) && s[i+1] == q {
						b.WriteByte(q)
						i += 2
						continue
					}
					i++
					break
				}
				b.WriteByte(s[i])
				i++
			}
			toks = append(toks, sqlTok{sqlTokIdent, strings.ToLower(b.String())})
		case isSQLWordStart(c):
			start := i
			for i < len(s) && isSQLWordByte(s[i]) {
				i++
			}
			toks = append(toks, sqlTok{sqlTokWord, strings.ToLower(s[start:i])})
		case c >= '0' && c <= '9':
			for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
				i++
			}
			toks = append(toks, sqlTok{sqlTokNumber, ""})
		default:
			toks = append(toks, sqlTok{sqlTokPunct, string(c)})
			i++
		}
	}
	return toks
}

func isSQLWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isSQLWordByte(c byte) bool {
	return isSQLWordStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// collectCTENames 收集 WITH [RECURSIVE] name [(cols)] AS (...) 定义的查询局部名。
// 仅登记确实带 AS (...) 体的名字;CTE 体本身仍由主扫描校验其中的 FROM/JOIN。
func collectCTENames(toks []sqlTok) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < len(toks); i++ {
		if toks[i].kind != sqlTokWord || toks[i].text != "with" {
			continue
		}
		j := i + 1
		if j < len(toks) && toks[j].kind == sqlTokWord && toks[j].text == "recursive" {
			j++
		}
		for j < len(toks) && (toks[j].kind == sqlTokWord || toks[j].kind == sqlTokIdent) {
			name := toks[j].text
			j++
			// 可选列清单 name(col1, col2)
			if j < len(toks) && toks[j].kind == sqlTokPunct && toks[j].text == "(" {
				depth := 0
				for ; j < len(toks); j++ {
					if toks[j].kind == sqlTokPunct && toks[j].text == "(" {
						depth++
					}
					if toks[j].kind == sqlTokPunct && toks[j].text == ")" {
						depth--
						if depth == 0 {
							j++
							break
						}
					}
				}
			}
			if j >= len(toks) || toks[j].kind != sqlTokWord || toks[j].text != "as" {
				break // 不是合法的 CTE 定义形态,不登记(比对时按普通表名处理)
			}
			j++
			// 可选 [NOT] MATERIALIZED(Postgres 12+)
			if j < len(toks) && toks[j].kind == sqlTokWord && toks[j].text == "not" {
				j++
			}
			if j < len(toks) && toks[j].kind == sqlTokWord && toks[j].text == "materialized" {
				j++
			}
			if j >= len(toks) || toks[j].kind != sqlTokPunct || toks[j].text != "(" {
				break
			}
			out[name] = true
			// 跳过 CTE 体(按括号配平)
			depth := 0
			for ; j < len(toks); j++ {
				if toks[j].kind == sqlTokPunct && toks[j].text == "(" {
					depth++
				}
				if toks[j].kind == sqlTokPunct && toks[j].text == ")" {
					depth--
					if depth == 0 {
						j++
						break
					}
				}
			}
			if j < len(toks) && toks[j].kind == sqlTokPunct && toks[j].text == "," {
				j++
				continue
			}
			break
		}
	}
	return out
}

// collectFromJoinRefs 扫描全部 FROM/JOIN/TABLE 关键字,解析其后(含逗号多表)的关系项并返回表名。
// TABLE 亦按关系引入词处理(Postgres 的 `TABLE t` 可作 UNION 分支,等价 SELECT * FROM t)。
func collectFromJoinRefs(toks []sqlTok) ([]string, error) {
	var refs []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.kind != sqlTokWord || (t.text != "from" && t.text != "join" && t.text != "table") {
			continue
		}
		names, next, err := parseFromItems(toks, i+1)
		if err != nil {
			return nil, err
		}
		refs = append(refs, names...)
		i = next - 1
	}
	return refs, nil
}

// parseFromItems 解析 FROM/JOIN 后的逗号分隔关系项列表:每个关系项必须是可归一为
// [schema.]identifier 的表引用;遇到连接词/子句词/括号边界停止。返回下一个待扫描下标。
func parseFromItems(toks []sqlTok, i int) ([]string, int, error) {
	var names []string
	for {
		name, ni, err := parseRelationRef(toks, i)
		if err != nil {
			return nil, 0, err
		}
		names = append(names, name)
		i = ni
		// 可选别名:AS ident | 裸/引号 ident(非子句词)
		if i < len(toks) && toks[i].kind == sqlTokWord && toks[i].text == "as" {
			if i+1 < len(toks) && (toks[i+1].kind == sqlTokWord || toks[i+1].kind == sqlTokIdent) {
				i += 2
			} else {
				return nil, 0, Fail(errcode.ErrParam, "FROM/JOIN 别名无法解析，只读 SQL 拒绝执行")
			}
		} else if i < len(toks) && (toks[i].kind == sqlTokIdent ||
			(toks[i].kind == sqlTokWord && !sqlClauseStops[toks[i].text])) {
			i++
		}
		if i < len(toks) && toks[i].kind == sqlTokPunct && toks[i].text == "," {
			i++ // 逗号多表:继续校验下一关系项
			continue
		}
		return names, i, nil
	}
}

// parseRelationRef 解析单个关系项:[schema.]identifier(引号标识符已归一);schema 前缀只取末段。
// 括号(派生表/函数/括号连接)与其他形态一律拒绝。
func parseRelationRef(toks []sqlTok, i int) (string, int, error) {
	if i >= len(toks) {
		return "", 0, Fail(errcode.ErrParam, "FROM/JOIN 后缺少表引用，只读 SQL 拒绝执行")
	}
	t := toks[i]
	if t.kind == sqlTokPunct && t.text == "(" {
		return "", 0, Fail(errcode.ErrParam, "FROM/JOIN 后的括号形式(派生表/函数/括号连接)不在只读工具范围内")
	}
	if t.kind != sqlTokWord && t.kind != sqlTokIdent {
		return "", 0, Fail(errcode.ErrParam, "FROM/JOIN 后的表引用无法解析，只读 SQL 拒绝执行")
	}
	name := t.text
	i++
	for i+1 < len(toks) && toks[i].kind == sqlTokPunct && toks[i].text == "." &&
		(toks[i+1].kind == sqlTokWord || toks[i+1].kind == sqlTokIdent) {
		name = toks[i+1].text // schema.table → 取末段
		i += 2
	}
	if name == "" {
		return "", 0, Fail(errcode.ErrParam, "FROM/JOIN 后的表引用无法解析，只读 SQL 拒绝执行")
	}
	return name, i, nil
}
