package system

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	systemsvc "github.com/sharptoolbox/opic-techbase/services/audit/internal/service/system"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/logger"
	"github.com/sharptoolbox/opic-techbase/services/shared/pkg/response"
)

func internalServerError(c *gin.Context, message string, err error) {
	if logger.Logger != nil && err != nil {
		logger.Error(message, logger.Err(err))
	}
	response.InternalServerError(c, message)
}

func writeSystemOperationLogServiceError(c *gin.Context, operation string, err error) {
	switch {
	case errors.Is(err, systemsvc.ErrOperationLogNotFound):
		response.NotFoundWithCode(c, response.ErrorCodeOperationLogNotFound, systemsvc.ErrOperationLogNotFound.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		internalServerError(c, operation, err)
	default:
		internalServerError(c, operation, err)
	}
}
