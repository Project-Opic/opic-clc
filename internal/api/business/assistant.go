// AI 智能助理端点(登录即可;只读)。
package business

import (
	"strings"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/service"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

type chatReq struct {
	Question string `json:"question"`
}

// AssistantChat POST /api/assistant/chat
func AssistantChat(c *gin.Context) {
	var body chatReq
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Question) == "" {
		c.JSON(200, errcode.Error(errcode.ErrParam, "question 不能为空"))
		return
	}
	uid := service.FromCtx(c.Request.Context()).ID
	reply, err := service.AssistantChat(uid, body.Question)
	if err != nil {
		c.JSON(200, errcode.Error(errcode.ErrAuthServer, "%s", err.Error()))
		return
	}
	c.JSON(200, errcode.OK(reply))
}
