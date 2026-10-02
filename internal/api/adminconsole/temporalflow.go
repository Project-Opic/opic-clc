package adminconsole

// Temporal 流程设计器 API —— 挂 /api/admin/temporalflow（管理控制台）。
//
// 端点:
//   GET  /api/admin/temporalflow/definitions           列表（flow_type=TEMPORAL）
//   GET  /api/admin/temporalflow/definitions/:id       详情（含 node_graph）
//   POST /api/admin/temporalflow/definitions           新建/保存（草稿）
//   POST /api/admin/temporalflow/definitions/:id/publish   发布（status=1）
//   POST /api/admin/temporalflow/definitions/:id/run       试运行（启动 Temporal 解释器工作流）
//
// 存储复用 flow_definition（flow_type='TEMPORAL'，node_graph 存 DSL 图），
// 与 V1 审批流（APPROVAL）同表不同类，互不干扰。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/expr"
	"gitcode.com/opic-ontology/opic-techbase/internal/store"
	"gitcode.com/opic-ontology/opic-techbase/pkg/temporalflow"

	"github.com/gin-gonic/gin"
)

const flowTypeTemporal = "TEMPORAL"

// Register 挂载路由（管理控制台，已过登录+system:manage 权限）。
func RegisterTemporalFlow(g *gin.RouterGroup) {
	g.GET("/temporalflow/definitions", list)
	g.GET("/temporalflow/definitions/:id", get)
	g.POST("/temporalflow/definitions", save)
	g.POST("/temporalflow/definitions/:id/publish", publish)
	g.POST("/temporalflow/definitions/:id/run", run)
	g.POST("/temporalflow/validate", validate) // 设计器即时校验（不落库）
}

func list(c *gin.Context) {
	rows, err := store.List(nil, `SELECT id, code, name, description, version, status, created_at, updated_at
		FROM flow_definition WHERE flow_type=? ORDER BY updated_at DESC`, flowTypeTemporal)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 非法"})
		return
	}
	row, err := store.One(nil, `SELECT id, code, name, description, node_graph, version, status FROM flow_definition
		WHERE id=? AND flow_type=?`, id, flowTypeTemporal)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	c.JSON(http.StatusOK, row)
}

// save 新建或更新（body: {id?, code, name, description, node_graph(JSON 字符串或对象)}）。
func save(c *gin.Context) {
	var body struct {
		ID          int64           `json:"id"`
		Code        string          `json:"code"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		NodeGraph   json.RawMessage `json:"node_graph"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Code == "" || body.Name == "" || len(body.NodeGraph) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code/name/node_graph 必填"})
		return
	}
	// 保存前强校验 DSL
	if _, err := temporalflow.Parse(body.NodeGraph); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if body.ID > 0 {
		if _, err := store.Exec(nil, `UPDATE flow_definition SET code=?, name=?, description=?, node_graph=?, updated_at=? WHERE id=? AND flow_type=?`,
			body.Code, body.Name, body.Description, string(body.NodeGraph), now, body.ID, flowTypeTemporal); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": body.ID})
		return
	}
	row, err := store.One(nil, `INSERT INTO flow_definition (code, name, flow_type, description, node_graph, version, status, created_at, updated_at)
		VALUES (?,?,?,?,?,0,0,?,?) RETURNING id`, body.Code, body.Name, flowTypeTemporal, body.Description, string(body.NodeGraph), now, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": toI64(row["id"])})
}

// publish 发布（status=1；发布前强校验）。
func publish(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 非法"})
		return
	}
	row, err := store.One(nil, `SELECT node_graph FROM flow_definition WHERE id=? AND flow_type=?`, id, flowTypeTemporal)
	if err != nil || row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	graphJSON, _ := row["node_graph"].(string)
	if _, err := temporalflow.Parse([]byte(graphJSON)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "DSL 校验失败: " + err.Error()})
		return
	}
	if _, err := store.Exec(nil, `UPDATE flow_definition SET status=1, updated_at=? WHERE id=?`, time.Now().Format("2006-01-02 15:04:05"), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"published": true})
}

// run 试运行：启动 Temporal 解释器工作流（RUN_ID 用时间戳；变量可选）。
func run(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 非法"})
		return
	}
	var body struct {
		Vars    map[string]any `json:"vars"`
		Address string         `json:"address"`
	}
	_ = c.ShouldBindJSON(&body)
	address := body.Address
	if address == "" {
		address = envDefault("WORKFLOW_ADDRESS", "temporal:7233")
	}
	row, err := store.One(nil, `SELECT id, code, node_graph, status FROM flow_definition WHERE id=? AND flow_type=?`, id, flowTypeTemporal)
	if err != nil || row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	if toI64(row["status"]) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "流程未发布，不能试运行"})
		return
	}
	graphJSON, _ := row["node_graph"].(string)
	in := temporalflow.RunInput{
		DefID: id,
		Code:  fmt.Sprint(row["code"]),
		RunID: fmt.Sprintf("run-%d", time.Now().UnixMilli()),
		Graph: json.RawMessage(graphJSON),
		Vars:  body.Vars,
	}
	if in.Vars == nil {
		in.Vars = map[string]any{}
	}
	runID, err := temporalflow.StartInterpreter(address, envDefault("WORKFLOW_NAMESPACE", "opic"), in)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run_id": runID, "workflow_id": fmt.Sprintf("temporalflow-%s-%s", in.Code, in.RunID)})
}

// validate 设计器即时校验（不落库）：DSL 结构 + 表达式试算。
func validate(c *gin.Context) {
	var body struct {
		NodeGraph json.RawMessage `json:"node_graph"`
		Vars      map[string]any  `json:"vars"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g, err := temporalflow.Parse(body.NodeGraph)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "error": err.Error()})
		return
	}
	// 条件表达式逐一试算（纯函数，无副作用）
	issues := []string{}
	for _, n := range g.Nodes {
		if fmt.Sprint(n["type"]) == temporalflow.NodeCondition {
			e, _ := n["expr"].(string)
			if e == "" {
				issues = append(issues, fmt.Sprintf("节点 %s 缺少 expr", n["id"]))
				continue
			}
			if v, eerr := expr.Eval(e, body.Vars); eerr != nil || v == nil {
				issues = append(issues, fmt.Sprintf("节点 %s 表达式求值失败: %s", n["id"], e))
			}
		}
	}
	if len(issues) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "issues": issues})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true})
}

// ── 内部 ──

func osGetenv(k string) string { return os.Getenv(k) }

func toI64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int32:
		return int64(x)
	case int16:
		return int64(x)
	case int8:
		return int64(x)
	case int:
		return int64(x)
	case uint:
		return int64(x)
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

func envDefault(k, def string) string {
	if v := osGetenv(k); v != "" {
		return v
	}
	return def
}
