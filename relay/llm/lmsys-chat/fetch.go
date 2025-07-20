package lmsys_chat

import (
	"chatgpt-adapter/core/common"
	"context"
	"github.com/bincooo/emit.io"
	"github.com/google/uuid"
	"net/http"
	"sync"
)

const (
	baseUrl = "https://lmarena.ai"
)

var (
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0"
	clearance = ""
	lang      = ""

	mu    sync.Mutex
	state int32 = 0 // 0 常态 1 等待中
)

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

func fetch(ctx context.Context, cookie string, messages, modelId string) (response *http.Response, err error) {

	sessionId := uuid.NewString()
	messageId := uuid.NewString()
	modelMessageId := uuid.NewString()

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
		POST(baseUrl+"/api/stream/create-evaluation").
		Body(req).
		DoC(emit.Status(http.StatusOK), emit.IsSTREAM)
	return
}
