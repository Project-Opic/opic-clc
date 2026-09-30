// Package service —— AI 智能助理（v3 补齐：工作台业务问答与操作指引）。
//
// 口径：基于登录用户可见业务数据的规则式应答（待办/客户/流程/指引），全部走既有
// 只读查询；无外部 LLM 依赖（第 3 层可后接，请求/响应形态不变）。
package service

import (
	"fmt"
	"strings"
)

// AssistantReply 助理应答。
type AssistantReply struct {
	Answer string   `json:"answer"`
	Hints  []string `json:"hints"`  // 建议追问
	Action string   `json:"action"` // 建议跳转路径(可空)
}

// AssistantChat 助理问答入口。
func AssistantChat(uid int64, question string) (*AssistantReply, error) {
	q := strings.TrimSpace(question)
	lq := strings.ToLower(q)
	switch {
	case strings.Contains(q, "待办") || strings.Contains(lq, "todo"):
		tasks, err := WorkbenchTodo(uid, 1, 10)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "您当前有 %d 条待办任务。", tasks.Total)
		for i, t := range tasks.List {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&b, "\n%d. %s（%s，来自 %s）", i+1, t["activity_name"], t["customer_name"], t["applicant_name"])
		}
		if tasks.Total > 5 {
			fmt.Fprintf(&b, "\n……其余 %d 条请在「审批中心-我的待办」查看。", tasks.Total-5)
		}
		return &AssistantReply{Answer: b.String(), Action: "/workbench/todo",
			Hints: []string{"如何审批任务？", "我的客户申请进展？"}}, nil
	case strings.Contains(q, "客户") || strings.Contains(lq, "customer"):
		row, err := qOne(`SELECT COUNT(*) AS n, SUM(CASE WHEN status = '已通过' THEN 1 ELSE 0 END) AS passed
			FROM customer_application WHERE applicant_id = ?`, uid)
		if err != nil {
			return nil, err
		}
		return &AssistantReply{
			Answer: fmt.Sprintf("您累计提交 %d 条客户申请，其中 %d 条已通过。",
				Int(row["n"]), Int(row["passed"])),
			Action: "/customer/query", Hints: []string{"我有哪些待办？", "如何发起新申请？"},
		}, nil
	case strings.Contains(q, "审批") || strings.Contains(q, "怎么操作") || strings.Contains(q, "如何"):
		return &AssistantReply{
			Answer: "审批操作指引：① 进入「审批中心-我的待办」；② 查看申请详情与流转意见；" +
				"③ 通过/驳回/退回并填写意见。经理审批后自动流转部门总经理终审，全程留痕可查。" +
				"系统管理人员可从右上角进入「管理控制台」维护用户、角色与权限。",
			Action: "/workbench/todo", Hints: []string{"我有哪些待办？", "流程有哪些节点？"},
		}, nil
	case strings.Contains(q, "流程") || strings.Contains(q, "节点"):
		return &AssistantReply{
			Answer: "客户申请流程为三级流转：提交 → 客户经理审批 → 部门总经理终审 → 归档。" +
				"驳回后申请人可修改重新提交；流程定义可在「流程管理-流程定义」可视化调整。",
			Action: "/flow/definitions", Hints: []string{"我有哪些待办？", "审批怎么操作？"},
		}, nil
	case strings.Contains(q, "管理台") || strings.Contains(q, "管理控制台") || strings.Contains(q, "admin"):
		return &AssistantReply{
			Answer: "管理控制台（/admin）面向系统管理员：仪表盘、用户/角色/权限/资源管理、流程管理。" +
				"工作台右上角头像菜单可直达。gopherforge 视觉体系，深空玻璃双主题。",
			Action: "/admin/dashboard", Hints: []string{"我有哪些待办？"},
		}, nil
	default:
		return &AssistantReply{
			Answer: "我是 OPIC 工作台助理，可以帮您查询待办任务、客户申请进展、解释审批流程、" +
				"指引管理控制台使用。试试下方建议问题。",
			Hints:  []string{"我有哪些待办？", "我的客户申请进展？", "审批怎么操作？", "流程有哪些节点？"},
		}, nil
	}
}
