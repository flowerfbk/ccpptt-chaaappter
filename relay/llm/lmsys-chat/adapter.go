package lmsys_chat

import (
	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/gin/inter"
	"chatgpt-adapter/core/gin/model"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"github.com/gin-gonic/gin"
	"github.com/iocgo/sdk/env"
	"golang.org/x/exp/maps"
)

var (
	Model = "lmsys-chat"

	modelMap = map[string]string{
		"gpt-4.1-2025-04-14":                      "14e9311c-94d2-40c2-8c54-273947e208b0",
		"gemini-2.5-pro":                          "e2d9d353-6dbe-4414-bf87-bd289d523726",
		"claude-opus-4-20250514":                  "ee116d12-64d6-48a8-88e5-b2d06325cdd2",
		"claude-3-7-sonnet-20250219-thinking-32k": "be98fcfd-345c-4ae1-9a82-a19123ebf1d2",
	}
)

type api struct {
	inter.BaseAdapter

	env *env.Environment
}

func (api *api) Match(ctx *gin.Context, model string) (ok bool, err error) {
	token := ctx.GetString("token")
	if len(model) <= 11 || model[:11] != Model+"/" {
		return
	}

	customMap := api.env.GetStringMapString("lmsys-chat.model")
	slice := maps.Keys(customMap)
	modelSlice := maps.Keys(modelMap)
	for _, mod := range append(slice, modelSlice...) {
		if model[11:] != mod {
			continue
		}

		password := api.env.GetString("server.password")
		if password != "" && password != token {
			err = response.UnauthorizedError
			return
		}

		ok = true
	}
	return
}

func (api *api) Models() (result []model.Model) {
	customMap := api.env.GetStringMapString("lmsys-chat.model")
	slice := maps.Keys(customMap)
	modelSlice := maps.Keys(modelMap)

	for _, mod := range append(slice, modelSlice...) {
		result = append(result, model.Model{
			Id:      "lmsys-chat/" + mod,
			Object:  "model",
			Created: 1686935002,
			By:      "lmsys-chat-adapter",
		})
	}
	return
}

func (api *api) ToolChoice(ctx *gin.Context) (ok bool, err error) {
	var (
		completion = common.GetGinCompletion(ctx)
	)

	if toolChoice(ctx, completion) {
		ok = true
	}
	return
}

func (api *api) Completion(ctx *gin.Context) (err error) {
	var (
		completion = common.GetGinCompletion(ctx)
	)

	completion.Model = completion.Model[11:]
	newMessages, err := mergeMessages(ctx, completion)
	if err != nil {
		response.Error(ctx, -1, err)
		return
	}
	ctx.Set(ginTokens, response.CalcTokens(newMessages))
	resp, err := fetch(ctx.Request.Context(), ctx.GetString("token"), newMessages, GetModelId(completion.Model))
	if err != nil {
		logger.Error(err)
		return
	}

	content, err := waitResponse(ctx, resp, completion.Stream)
	if content == "" && response.NotResponse(ctx) {
		response.Error(ctx, -1, "EMPTY RESPONSE")
	}
	return
}

func GetModelId(model string) string {
	customMap := env.Env.GetStringMapString("lmsys-chat.model")
	mod, ok := customMap[model]
	if ok {
		return mod
	}

	mod, ok = modelMap[model]
	if ok {
		return mod
	}

	return modelMap["gpt-4.1-2025-04-14"]
}
