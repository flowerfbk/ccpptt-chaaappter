package lmsys_chat

import (
	"chatgpt-adapter/core/common"
	"chatgpt-adapter/core/logger"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bincooo/emit.io"
	"github.com/google/uuid"
	"github.com/iocgo/sdk/env"
)

const (
	baseUrl = "https://lmarena.ai/nextjs-api"
)

var (
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0"
	clearance = ""
	lang      = ""

	mu    sync.Mutex
	state int32 = 0 // 0 常态 1 等待中
	
	// 会话缓存
	sessionCache = make(map[string]*SessionCache)
	cacheMutex   sync.RWMutex
	
	// 自动获取的cookie缓存
	autoCookie     string
	cookieMutex    sync.RWMutex
	cookieExpireAt time.Time
)

// 会话缓存结构
type SessionCache struct {
	SessionId       string
	UserMessageId   string
	ModelMessageId  string
	ModelId         string
}

type LmsysChatRequest struct {
	Id              string `json:"id"`
	Mode            string `json:"mode"`
	ModelAId        string `json:"modelAId"`
	UserMessageId   string `json:"userMessageId"`
	ModelAMessageId string `json:"modelAMessageId"`
	Modality        string `json:"modality"`

	Messages []LmsysChatMessage `json:"messages"`
}

type LmsysChatMessage struct {
	Id                      string        `json:"id"`
	Role                    string        `json:"role"`
	Content                 string        `json:"content"`
	ExperimentalAttachments []interface{} `json:"experimental_attachments"`
	ParentMessageIds        []string      `json:"parentMessageIds"`
	ParticipantPosition     string        `json:"participantPosition"`
	ModelId                 *string       `json:"modelId"`
	EvaluationSessionId     string        `json:"evaluationSessionId"`
	Status                  string        `json:"status"`
	FailureReason           interface{}   `json:"failureReason"`
}

// 获取缓存 key
func getCacheKey(cookie string, modelId string) string {
	// 使用 cookie 的一部分 + modelId 作为 key
	authPart := ""
	parts := strings.Split(cookie, "arena-auth-prod-v1.0=")
	if len(parts) > 1 {
		authPart = strings.Split(parts[1], ";")[0]
		if len(authPart) > 50 {
			authPart = authPart[:50]
		}
	} else if len(cookie) > 50 {
		authPart = cookie[:50]
	} else {
		authPart = cookie
	}
	return authPart + "_" + modelId
}

// 获取有效的cookie
func getValidCookie(ctx context.Context) (string, error) {
	cookieMutex.RLock()
	if autoCookie != "" && time.Now().Before(cookieExpireAt) {
		cookie := autoCookie
		cookieMutex.RUnlock()
		return cookie, nil
	}
	cookieMutex.RUnlock()
	
	// 需要获取新的cookie
	return refreshCookie(ctx)
}

// 刷新cookie
func refreshCookie(ctx context.Context) (string, error) {
	cookieMutex.Lock()
	defer cookieMutex.Unlock()
	
	// 双重检查
	if autoCookie != "" && time.Now().Before(cookieExpireAt) {
		return autoCookie, nil
	}
	
	logger.Info("正在通过 browser-less 获取 lmarena.ai cookie...")
	
	baseUrl := env.Env.GetString("browser-less.reversal")
	if !env.Env.GetBool("browser-less.enabled") && baseUrl == "" {
		return "", errors.New("需要启用 browser-less 来自动获取 cookie，请设置 `browser-less.enabled` 或 `browser-less.reversal`")
	}
	
	if baseUrl == "" {
		baseUrl = "http://127.0.0.1:" + env.Env.GetString("browser-less.port")
	}
	
	// 调用 browser-less 获取 cookie
	r, err := emit.ClientBuilder(common.HTTPClient).
		Context(ctx).
		GET(baseUrl+"/v0/clearance").
		Header("x-website", "https://lmarena.ai").
		Header("x-mode", "anonymous"). // 使用匿名模式
		DoC(emit.Status(http.StatusOK), emit.IsJSON)
	if err != nil {
		logger.Error("browser-less 获取 cookie 失败:", err)
		if emit.IsJSON(r) == nil {
			logger.Error(emit.TextResponse(r))
		}
		return "", err
	}
	
	defer r.Body.Close()
	obj, err := emit.ToMap(r)
	if err != nil {
		logger.Error("解析 browser-less 响应失败:", err)
		return "", err
	}
	
	data := obj["data"].(map[string]interface{})
	autoCookie = data["cookie"].(string)
	userAgent = data["userAgent"].(string)
	lang = data["lang"].(string)
	
	// 设置cookie过期时间（30分钟）
	cookieExpireAt = time.Now().Add(30 * time.Minute)
	
	logger.Info("成功获取 lmarena.ai cookie")
	return autoCookie, nil
}

func fetch(ctx context.Context, cookie string, messages, modelId string) (response *http.Response, err error) {
	// 如果没有传入cookie，自动获取
	if cookie == "" {
		cookie, err = getValidCookie(ctx)
		if err != nil {
			return nil, err
		}
	}
	
	cacheKey := getCacheKey(cookie, modelId)
	
	cacheMutex.RLock()
	session, exists := sessionCache[cacheKey]
	cacheMutex.RUnlock()
	
	if exists {
		// 使用重试接口
		return fetchRetry(ctx, cookie, messages, modelId, session)
	}
	
	// 第一次，使用创建接口
	return fetchCreate(ctx, cookie, messages, modelId, cacheKey)
}

// 创建新会话
func fetchCreate(ctx context.Context, cookie string, messages, modelId, cacheKey string) (response *http.Response, err error) {
	sessionId := uuid.NewString()
	messageId := uuid.NewString()
	modelMessageId := uuid.NewString()
	
	// 保存到缓存
	cacheMutex.Lock()
	sessionCache[cacheKey] = &SessionCache{
		SessionId:      sessionId,
		UserMessageId:  messageId,
		ModelMessageId: modelMessageId,
		ModelId:        modelId,
	}
	cacheMutex.Unlock()

	req := LmsysChatRequest{
		Id:              uuid.NewString(),
		Mode:            "direct",
		ModelAId:        modelId,
		UserMessageId:   messageId,
		ModelAMessageId: modelMessageId,
		Messages: []LmsysChatMessage{
			{
				Id:                      messageId,
				Role:                    "user",
				Content:                 messages,
				ExperimentalAttachments: make([]interface{}, 0),
				ParentMessageIds:        make([]string, 0),
				ParticipantPosition:     "a",
				ModelId:                 nil,
				EvaluationSessionId:     sessionId,
				Status:                  "pending",
				FailureReason:           nil,
			},
			{
				Id:                      modelMessageId,
				Role:                    "assistant",
				Content:                 "",
				ExperimentalAttachments: make([]interface{}, 0),
				ParentMessageIds: []string{
					messageId,
				},
				ParticipantPosition: "a",
				ModelId:             &modelId,
				EvaluationSessionId: sessionId,
				Status:              "pending",
				FailureReason:       nil,
			},
		},
		Modality: "chat",
	}

	response, err = emit.ClientBuilder(common.HTTPClient).
		Context(ctx).
		Header("User-Agent", userAgent).
		Header("Accept-Language", lang).
		Header("Cache-Control", "no-cache").
		Header("Accept-Encoding", "gzip, deflate, br, zstd").
		Header("Origin", baseUrl).
		Header("Cookie", cookie).
		Ja3().
		JSONHeader().
		POST(baseUrl+"/stream/create-evaluation").
		Body(req).
		DoC(emit.Status(http.StatusOK), emit.IsSTREAM)
	
	// 如果遇到403错误，刷新cookie重试
	if err != nil {
		var busErr emit.Error
		if errors.As(err, &busErr) && busErr.Code == 403 {
			logger.Info("遇到403错误，尝试刷新cookie...")
			newCookie, refreshErr := refreshCookie(ctx)
			if refreshErr == nil {
				return fetchCreate(ctx, newCookie, messages, modelId, cacheKey)
			}
		}
	}
	
	return
}

// 重试已有会话
func fetchRetry(ctx context.Context, cookie string, messages, modelId string, session *SessionCache) (response *http.Response, err error) {
	// 构建重试请求
	retryReq := map[string]interface{}{
		"messages": []LmsysChatMessage{
			{
				Id:                      session.UserMessageId,
				Role:                    "user",
				Content:                 messages,
				ExperimentalAttachments: make([]interface{}, 0),
				ParentMessageIds:        make([]string, 0),
				ParticipantPosition:     "a",
				ModelId:                 nil,
				EvaluationSessionId:     session.SessionId,
				Status:                  "pending",
				FailureReason:           nil,
			},
		},
		"modelId": modelId,
	}
	
	// 调用重试接口
	url := fmt.Sprintf("%s/stream/retry-evaluation-session-message/%s/messages/%s", 
		baseUrl, session.SessionId, session.ModelMessageId)
	
	response, err = emit.ClientBuilder(common.HTTPClient).
		Context(ctx).
		Header("User-Agent", userAgent).
		Header("Accept-Language", lang).
		Header("Cache-Control", "no-cache").
		Header("Accept-Encoding", "gzip, deflate, br, zstd").
		Header("Origin", baseUrl).
		Header("Cookie", cookie).
		Ja3().
		JSONHeader().
		PUT(url).
		Body(retryReq).
		DoC(emit.Status(http.StatusOK), emit.IsSTREAM)
	
	// 如果遇到403错误，刷新cookie重试
	if err != nil {
		var busErr emit.Error
		if errors.As(err, &busErr) && busErr.Code == 403 {
			logger.Info("遇到403错误，尝试刷新cookie...")
			newCookie, refreshErr := refreshCookie(ctx)
			if refreshErr == nil {
				return fetchRetry(ctx, newCookie, messages, modelId, session)
			}
		}
	}
	
	return
}
