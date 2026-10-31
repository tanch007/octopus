package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/relay"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/looplj/axonhub/llm"
)

func init() {
	router.NewGroupRouter("/v1").
		Use(middleware.APIKeyAuth()).
		AddRoute(
			router.NewRoute("/chat/completions", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatOpenAIChatCompletion)),
		).
		AddRoute(
			router.NewRoute("/responses", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatOpenAIResponse)),
		).
		AddRoute(
			router.NewRoute("/messages", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatAnthropicMessage)),
		).
		// 嵌入类端点。
		AddRoute(
			router.NewRoute("/embeddings", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatOpenAIEmbedding)),
		).
		// 语音类端点: 语音合成返回音频二进制, 语音转写与翻译接收音频文件。
		AddRoute(
			router.NewRoute("/audio/speech", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatOpenAISpeech)),
		).
		AddRoute(
			router.NewRoute("/audio/transcriptions", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatOpenAITranscription)),
		).
		AddRoute(
			router.NewRoute("/audio/translations", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatOpenAITranslation)),
		).
		// 视频类端点: 创建是异步的, 之后按任务 ID 查询或删除。
		AddRoute(
			router.NewRoute("/videos", http.MethodPost).
				Handle(relay.Forward(llm.APIFormatOpenAIVideo)),
		).
		AddRoute(
			router.NewRoute("/videos/:id", http.MethodGet).
				Handle(relay.VideoTaskRetrieve()),
		).
		AddRoute(
			router.NewRoute("/videos/:id", http.MethodDelete).
				Handle(relay.VideoTaskDelete()),
		)
}
