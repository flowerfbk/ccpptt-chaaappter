package lmsys_chat

import (
	"chatgpt-adapter/core/common/toolcall"
	"chatgpt-adapter/core/common/vars"
	"chatgpt-adapter/core/gin/model"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"github.com/gin-gonic/gin"
)

func toolChoice(ctx *gin.Context, completion model.Completion) bool {
	logger.Info("completeTools ...")
	echo := ctx.GetBool(vars.GinEcho)

	exec, err := toolcall.ToolChoice(ctx, completion, func(message string) (string, error) {
		if echo {
			logger.Infof("toolCall message: \n%s", message)
			return "", nil
		}
		completion.Model = completion.Model[11:]
		completion.Messages = []model.Keyv[interface{}]{
			{
				"role":    "user",
				"content": message,
			},
		}

		newMessages, err := mergeMessages(ctx, completion)
		if err != nil {
			return "", err
		}

		r, err := fetch(ctx.Request.Context(), ctx.GetString("token"), newMessages, GetModelId(completion.Model))
		if err != nil {
			return "", err
		}

		return waitMessage(r, toolcall.Cancel)
	})

	if err != nil {
		logger.Error(err)
		response.Error(ctx, -1, err)
		return true
	}

	return exec
}
