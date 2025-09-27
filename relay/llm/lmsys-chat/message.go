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
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ginTokens = "__tokens__"

func waitMessage(r *http.Response, cancel func(str string) bool) (content string, err error) {

	defer r.Body.Close()
	reader := bufio.NewReader(r.Body)
	var chunk []byte

	for {
		chunk, _, err = reader.ReadLine()
		if err == io.EOF {
			break
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

		logger.Debug("----- raw -----")
		logger.Debug(raw)
		if len(raw) > 0 {
			content += raw
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
	
	// 添加超时控制
	startTime := time.Now()
	maxDuration := 10 * time.Minute // 最长等待10分钟

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
		// 检查是否超时
		if time.Since(startTime) > maxDuration {
			logger.Warn("等待响应超时，强制结束")
			break
		}
		
		chunk, _, err = reader.ReadLine()
		if err == io.EOF {
			logger.Debug("读取到 EOF，继续等待...")
			// 不要 break，继续等待
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			logger.Debugf("读取错误: %v，继续等待...", err)
			time.Sleep(100 * time.Millisecond)
			continue
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
				logger.Errorf("解析 a0 数据失败: %v, 原始数据: %s", err, string(chunk))
				continue // 改为 continue，不要 return
			}
		}

		if bytes.HasPrefix(chunk, []byte("ad:")) {
			var obj map[string]interface{}
			err = json.Unmarshal(chunk[3:], &obj)
			if err != nil {
				logger.Errorf("解析 ad 数据失败: %v, 原始数据: %s", err, string(chunk))
				continue // 改为 continue，不要 return
			}

			finishReason, ok := obj["finishReason"]
			if ok && finishReason == "stop" {
				logger.Info("收到 stop 信号，准备结束")
				break
			}
		}
		
		// 记录其他类型的数据
		if !bytes.HasPrefix(chunk, []byte("a0:")) && !bytes.HasPrefix(chunk, []byte("ad:")) && len(chunk) > 0 {
			logger.Debugf("收到未知类型数据: %s", string(chunk))
		}

		onceExec()

		raw = response.ExecMatchers(matchers, raw, false)
		if len(raw) == 0 {
			continue
		}

		if raw == response.EOF {
			logger.Info("收到 EOF 标记，但继续等待 stop 信号")
			// 不要 break，继续等待 stop
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
		messages = completion.Messages
	)

	messageL := len(messages)
	if messageL == 1 {
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

func waitImageResponse(ctx *gin.Context, r *http.Response) (imageUrl string, err error) {
	defer r.Body.Close()
	reader := bufio.NewReader(r.Body)
	var chunk []byte

	logger.Info("等待图片生成响应...")
	
	for {
		chunk, _, err = reader.ReadLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error(err)
			return
		}

		logger.Debug("----- raw chunk -----")
		logger.Debug(string(chunk))
		
		// 查找包含图片URL的响应
		if bytes.HasPrefix(chunk, []byte("a2:")) {
			var raw string
			err = json.Unmarshal(chunk[3:], &raw)
			if err != nil {
				logger.Error(err)
				continue
			}
			
			// 检查是否是图片URL
			if strings.Contains(raw, "https://") && (strings.Contains(raw, ".png") || strings.Contains(raw, ".jpg") || strings.Contains(raw, ".jpeg") || strings.Contains(raw, ".webp")) {
				imageUrl = raw
				logger.Infof("找到图片URL: %s", imageUrl)
			}
		}

		// 检查是否完成
		if bytes.HasPrefix(chunk, []byte("ad:")) {
			var obj map[string]interface{}
			err = json.Unmarshal(chunk[3:], &obj)
			if err != nil {
				logger.Error(err)
				continue
			}

			finishReason, ok := obj["finishReason"]
			if ok && finishReason == "stop" {
				logger.Info("图片生成完成")
				break
			}
		}
	}

	if imageUrl == "" {
		err = fmt.Errorf("未能获取到生成的图片URL")
		logger.Error(err)
	}
	
	return imageUrl, err
}
