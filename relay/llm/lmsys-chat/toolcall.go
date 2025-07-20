package lmsys_chat

import (
	"chatgpt-adapter/core/gin/model"
	"github.com/gin-gonic/gin"
	"github.com/iocgo/sdk/env"
)

func toolChoice(ctx *gin.Context, env *env.Environment, proxies string, completion model.Completion) bool {
	return false
}
