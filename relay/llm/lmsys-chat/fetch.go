package lmsys_chat

import (
	"chatgpt-adapter/core/common"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bincooo/emit.io"
	"github.com/google/uuid"
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

func fetch(ctx context.Context, cookie string, messages, modelId string) (response *http.Response, err error) {
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
		Header("Accept-Language", "en-US,en;q=0.5").
		Header("Cache-Control", "no-cache").
		Header("Accept-Encoding", "gzip, deflate, br, zstd").
		Header("Origin", baseUrl).
		Header("Cookie", cookie).
		Ja3().
		JSONHeader().
		POST(baseUrl+"/stream/create-evaluation").
		Body(req).
		DoC(emit.Status(http.StatusOK), emit.IsSTREAM)
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
		Header("Accept-Language", "en-US,en;q=0.5").
		Header("Cache-Control", "no-cache").
		Header("Accept-Encoding", "gzip, deflate, br, zstd").
		Header("Origin", baseUrl).
		Header("Cookie", cookie).
		Ja3().
		JSONHeader().
		PUT(url).
		Body(retryReq).
		DoC(emit.Status(http.StatusOK), emit.IsSTREAM)
	
	return
}
