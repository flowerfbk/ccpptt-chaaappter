package lmsys_chat

import (
	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/gin/inter"
	"chatgpt-adapter/core/gin/model"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"fmt"
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
		
		// 新增模型
		"gpt-5-chat":                              "4b11c78c-08c8-461c-938e-5fc97d56a40d",
		"gpt-5-high":                              "983bc566-b783-4d28-b24c-3c8b08eb1086",
		"claude-opus-4-1-20250805":                "96ae95fd-b70d-49c3-91cc-b58c7da1090b",
		"gpt-5-high-new-system-prompt":            "19ad5f04-38c6-48ae-b826-f7d5bbfd79f7",
		"claude-opus-4-1-20250805-thinking-16k":   "f1a2eb6f-fc30-4806-9e00-1efd0d73cbc4",
		"claude-opus-4-20250514-thinking-16k":     "3b5e9593-3dc0-4492-a3da-19784c4bde75",
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

		// 如果设置了密码，检查token是否匹配
		// 注意：现在token可以为空（使用自动获取的cookie）
		password := api.env.GetString("server.password")
		if password != "" && token != "" && password != token {
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
	
	// 使用 fmt.Printf 打印调试信息，确保输出到标准输出
	fmt.Printf("\n=== lmsys-chat Models() 调试信息 ===\n")
	fmt.Printf("代码中定义的模型数量: %d\n", len(modelSlice))
	fmt.Printf("代码中定义的模型列表: %v\n", modelSlice)
	fmt.Printf("配置文件中的自定义模型数量: %d\n", len(slice))
	fmt.Printf("配置文件中的自定义模型列表: %v\n", slice)
	
	// 检查 gpt-5-chat 是否在 modelMap 中
	if _, exists := modelMap["gpt-5-chat"]; exists {
		fmt.Printf("✓ gpt-5-chat 存在于 modelMap 中\n")
	} else {
		fmt.Printf("✗ gpt-5-chat 不存在于 modelMap 中\n")
	}
	
	allModels := append(slice, modelSlice...)
	fmt.Printf("合并后的模型总数: %d\n", len(allModels))
	fmt.Printf("合并后的完整模型列表: %v\n", allModels)

	for _, mod := range allModels {
		modelId := "lmsys-chat/" + mod
		result = append(result, model.Model{
			Id:      modelId,
			Object:  "model",
			Created: 1686935002,
			By:      "lmsys-chat-adapter",
		})
	}
	
	fmt.Printf("最终返回的模型数量: %d\n", len(result))
	fmt.Printf("=== lmsys-chat Models() 调试结束 ===\n\n")
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
	
	// 获取用户传递的cookie（如果有的话）
	// 如果没有传递cookie，fetch函数会自动获取
	cookie := ctx.GetString("token")
	
	resp, err := fetch(ctx.Request.Context(), cookie, newMessages, GetModelId(completion.Model))
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
