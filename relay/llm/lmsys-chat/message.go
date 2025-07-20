package lmsys_chat

import (
	"bufio"
	"bytes"
	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/common/vars"
	"chatgpt-adapter/core/gin/model"
	"chatgpt-adapter/core/gin/response"
	"chatgpt-adapter/core/logger"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ginTokens = "__tokens__"

func waitMessage(chatResponse chan string, cancel func(str string) bool) (content string, err error) {

	for {
		message, ok := <-chatResponse
		if !ok {
			break
		}

		if strings.HasPrefix(message, "error: ") {
			return "", errors.New(strings.TrimPrefix(message, "error: "))
		}

		message = strings.TrimPrefix(message, "text: ")
		logger.Debug("----- raw -----")
		logger.Debug(message)
		if len(message) > 0 {
			content += message
			if cancel != nil && cancel(content) {
				return content, nil
			}
		}
	}

	return content, nil
}

func waitResponse(ctx *gin.Context, r *http.Response, sse bool) (content string, err error) {
	created := time.Now().Unix()
	logger.Infof("waitResponse ...")
	tokens := ctx.GetInt(ginTokens)
	reasoningContent := ""

	onceExec := sync.OnceFunc(func() {
		if !sse {
			ctx.Writer.WriteHeader(http.StatusOK)
		}
	})

	var (
		matchers = common.GetGinMatchers(ctx)
	)

	defer r.Body.Close()
	reader := bufio.NewReader(r.Body)
	var chunk []byte

	for {
		chunk, _, err = reader.ReadLine()
		if err == io.EOF {
			raw := response.ExecMatchers(matchers, "", true)
			if raw != "" && sse {
				response.SSEResponse(ctx, Model, raw, created)
			}
			content += raw
			break
		}

		logger.Debug("----- raw -----")
		logger.Debug(string(chunk))
		if len(chunk) == 0 {
			continue
		}

		raw := ""
		if bytes.HasPrefix(chunk, []byte("a0:")) {
			err = json.Unmarshal(chunk[3:], &raw)
			if err != nil {
				logger.Error(err)
				return
			}
		}

		if bytes.HasPrefix(chunk, []byte("ad:")) {
			var obj map[string]interface{}
			err = json.Unmarshal(chunk[3:], &obj)
			if err != nil {
				logger.Error(err)
				return
			}

			finishReason, ok := obj["finishReason"]
			if ok && finishReason == "stop" {
				break
			}
		}

		onceExec()

		raw = response.ExecMatchers(matchers, raw, false)
		if len(raw) == 0 {
			continue
		}

		if raw == response.EOF {
			break
		}

		if sse {
			response.SSEResponse(ctx, Model, raw, created)
		}
		content += raw
	}

	if content == "" && response.NotSSEHeader(ctx) {
		return
	}
	ctx.Set(vars.GinCompletionUsage, response.CalcUsageTokens(reasoningContent+content, tokens))
	if !sse {
		response.ReasonResponse(ctx, Model, content, reasoningContent)
	} else {
		response.SSEResponse(ctx, Model, "[DONE]", created)
	}
	return
}

func mergeMessages(ctx *gin.Context, completion model.Completion) (newMessages string, err error) {
	var (
		messages    = completion.Messages
		specialized = ctx.GetBool("specialized")
		isC         = response.IsClaude(ctx, completion.Model)
	)

	messageL := len(messages)
	if specialized && isC && messageL == 1 {
		newMessages = messages[0].GetString("content")
		return
	}

	var (
		pos      = 0
		contents []string
	)

	for {
		if pos > messageL-1 {
			break
		}

		message := messages[pos]
		role, end := response.ConvertRole(ctx, message.GetString("role"))
		contents = append(contents, role+message.GetString("content")+end)
		pos++
	}

	newMessages = strings.Join(contents, "")
	if strings.HasSuffix(newMessages, "<|end|>\n\n") {
		newMessages = newMessages[:len(newMessages)-9]
	}
	return
}
