package monitor

import (
	"github.com/gin-gonic/gin"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/logger"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/response"
)

func internalServerError(c *gin.Context, message string, err error) {
	if logger.Logger != nil && err != nil {
		logger.Error(message, logger.Err(err))
	}
	response.InternalServerError(c, message)
}
