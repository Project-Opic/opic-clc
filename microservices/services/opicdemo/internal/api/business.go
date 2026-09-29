package api

import (
	"github.com/gin-gonic/gin"

	"github.com/sharptoolbox/opic-techbase/services/opicdemo/internal/service"
)

// ---------- 流程管理(flow.py) ----------

// FlowDefinitionList GET /api/flow/definitions
func FlowDefinitionList(c *gin.Context) {
	page, size := pageOf(c)
	p, err := service.ListDefinitions(page, size, c.Query("keyword"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// FlowDefinitionGet GET /api/flow/definitions/:id
func FlowDefinitionGet(c *gin.Context) {
	row, err := service.GetDefinition(pathID(c, "id"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, row, "")
}

// FlowDefinitionGraph GET /api/flow/definitions/:id/graph
func FlowDefinitionGraph(c *gin.Context) {
	g, err := service.GetDefinitionGraph(pathID(c, "id"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, g, "")
}

// FlowDefinitionCreate POST /api/flow/definitions
func FlowDefinitionCreate(c *gin.Context) {
	id, err := service.CreateDefinition(bindBody(c), userOf(c).ID)
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, gin.H{"id": id}, "创建成功")
}

// FlowDefinitionUpdate PUT /api/flow/definitions/:id
func FlowDefinitionUpdate(c *gin.Context) {
	if err := service.UpdateDefinition(pathID(c, "id"), bindBody(c)); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "更新成功")
}

// FlowDefinitionPublish POST /api/flow/definitions/:id/publish
func FlowDefinitionPublish(c *gin.Context) {
	if err := service.PublishDefinition(pathID(c, "id")); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "发布成功")
}

// FlowInstanceList GET /api/flow/instances
func FlowInstanceList(c *gin.Context) {
	page, size := pageOf(c)
	p, err := service.ListInstances(page, size, map[string]any{
		"status":  c.Query("status"),
		"keyword": c.Query("keyword"),
	})
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// FlowInstanceGet GET /api/flow/instances/:id
func FlowInstanceGet(c *gin.Context) {
	row, err := service.GetInstanceDetail(pathID(c, "id"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, row, "")
}

// FlowInstanceTerminate PUT /api/flow/instances/:id/terminate
func FlowInstanceTerminate(c *gin.Context) {
	if err := service.TerminateInstance(pathID(c, "id"), userOf(c)); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "已终止")
}

// FlowTaskList GET /api/flow/tasks
func FlowTaskList(c *gin.Context) {
	page, size := pageOf(c)
	p, err := service.ListTasks(page, size, map[string]any{
		"status": c.Query("status"),
	})
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// FlowTaskTransfer PUT /api/flow/tasks/:id/transfer
func FlowTaskTransfer(c *gin.Context) {
	body := bindBody(c)
	assigneeID := service.Int(body["assignee_id"])
	if err := service.TransferTask(pathID(c, "id"), assigneeID); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "转办成功")
}

// FlowTaskUrge PUT /api/flow/tasks/:id/urge
func FlowTaskUrge(c *gin.Context) {
	if err := service.UrgeTask(pathID(c, "id"), userOf(c)); err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, nil, "催办成功")
}

// ---------- 客户申请(customer.py) ----------

// CustomerList GET /api/customer
func CustomerList(c *gin.Context) {
	page, size := pageOf(c)
	p, err := service.ListCustomers(page, size, map[string]any{
		"customer_no":   c.Query("customer_no"),
		"customer_name": c.Query("customer_name"),
		"status":        c.Query("status"),
	})
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// CustomerGet GET /api/customer/:id
func CustomerGet(c *gin.Context) {
	row, err := service.GetCustomer(pathID(c, "id"))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, row, "")
}

// CustomerCreateDraft POST /api/customer/draft
func CustomerCreateDraft(c *gin.Context) {
	row, err := service.CreateCustomerDraft(bindBody(c), userOf(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, row, "暂存成功")
}

// CustomerUpdateDraft PUT /api/customer/:id/draft
func CustomerUpdateDraft(c *gin.Context) {
	row, err := service.UpdateCustomerDraft(pathID(c, "id"), bindBody(c), userOf(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, row, "暂存成功")
}

// CustomerSubmit POST /api/customer/:id/submit
func CustomerSubmit(c *gin.Context) {
	row, err := service.SubmitCustomer(pathID(c, "id"), userOf(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, row, "提交成功")
}

// CustomerWithdraw POST /api/customer/:id/withdraw
func CustomerWithdraw(c *gin.Context) {
	row, err := service.WithdrawCustomer(pathID(c, "id"), userOf(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, row, "已撤回")
}

// ---------- 审批中心(workbench.py) ----------

// WorkbenchTodo GET /api/workbench/todo
func WorkbenchTodo(c *gin.Context) {
	page, size := pageOf(c)
	p, err := service.WorkbenchTodo(userOf(c).ID, page, size)
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// WorkbenchDone GET /api/workbench/done
func WorkbenchDone(c *gin.Context) {
	page, size := pageOf(c)
	p, err := service.WorkbenchDone(userOf(c).ID, page, size)
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// WorkbenchRequested GET /api/workbench/requested
func WorkbenchRequested(c *gin.Context) {
	page, size := pageOf(c)
	p, err := service.WorkbenchRequested(userOf(c).ID, page, size)
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, p, "")
}

// WorkbenchApprove POST /api/workbench/todo/:id/approve
func WorkbenchApprove(c *gin.Context) {
	body := bindBody(c)
	out, err := service.ApproveTask(pathID(c, "id"), service.Str(body["comment"]), userOf(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, out, "审批通过")
}

// WorkbenchReject POST /api/workbench/todo/:id/reject
func WorkbenchReject(c *gin.Context) {
	body := bindBody(c)
	out, err := service.RejectTask(pathID(c, "id"), service.Str(body["comment"]), userOf(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, out, "已驳回")
}

// WorkbenchReturn POST /api/workbench/todo/:id/return
func WorkbenchReturn(c *gin.Context) {
	body := bindBody(c)
	out, err := service.ReturnTask(pathID(c, "id"), service.Str(body["target_activity_id"]), service.Str(body["comment"]), userOf(c))
	if err != nil {
		failJSON(c, err)
		return
	}
	okJSON(c, out, "已退回")
}
