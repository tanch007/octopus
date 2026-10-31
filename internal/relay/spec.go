package relay

import (
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// protocolSpec 描述一个对外协议在转发链路上的差异: 入站转换器, 请求协议位与选路所需的协议位集合。
// 对话类协议之间可以经转换互相承接, 故选路时只要求成员声明了其中任意一个对话协议;
// 媒体类协议各自独立, 没有转换伙伴, 选路时必须精确声明该协议位。
type protocolSpec struct {
	format   llm.APIFormat       // 客户端协议格式。
	inbound  transformer.Inbound // 入站转换器, 兼作请求解析与校验器。
	protocol model.Protocol      // 该协议对应的请求协议位, 随请求状态推给界面。
	mask     model.Protocol      // 选路时成员授权需声明的协议位集合。
	media    bool                // 是否为媒体类协议: 请求体与响应体按原样透传, 不做跨协议转换。
}

// protocolSpecOf 返回客户端协议对应的转发规格。
func protocolSpecOf(format llm.APIFormat) protocolSpec {
	switch format {
	case llm.APIFormatOpenAIResponse:
		return protocolSpec{format: format, inbound: responses.NewInboundTransformer(), protocol: model.ProtocolOpenAIResponse, mask: model.ProtocolOpenAIDialogs}
	case llm.APIFormatAnthropicMessage:
		return protocolSpec{format: format, inbound: anthropic.NewInboundTransformer(), protocol: model.ProtocolAnthropicMessage, mask: model.ProtocolOpenAIDialogs}
	case llm.APIFormatOpenAIEmbedding:
		return protocolSpec{format: format, inbound: openai.NewEmbeddingInboundTransformer(), protocol: model.ProtocolOpenAIEmbedding, mask: model.ProtocolOpenAIEmbedding, media: true}
	case llm.APIFormatOpenAISpeech:
		return protocolSpec{format: format, inbound: openai.NewSpeechInboundTransformer(), protocol: model.ProtocolOpenAISpeech, mask: model.ProtocolOpenAISpeech, media: true}
	case llm.APIFormatOpenAITranscription:
		return protocolSpec{format: format, inbound: openai.NewTranscriptionInboundTransformer(), protocol: model.ProtocolOpenAITranscription, mask: model.ProtocolOpenAITranscription, media: true}
	case llm.APIFormatOpenAITranslation:
		return protocolSpec{format: format, inbound: openai.NewTranslationInboundTransformer(), protocol: model.ProtocolOpenAITranslation, mask: model.ProtocolOpenAITranslation, media: true}
	case llm.APIFormatOpenAIVideo:
		return protocolSpec{format: format, inbound: openai.NewVideoInboundTransformer(), protocol: model.ProtocolOpenAIVideo, mask: model.ProtocolOpenAIVideo, media: true}
	default:
		return protocolSpec{format: llm.APIFormatOpenAIChatCompletion, inbound: openai.NewInboundTransformer(), protocol: model.ProtocolOpenAIChatCompletion, mask: model.ProtocolOpenAIDialogs}
	}
}

// protocolPath 返回渠道为该协议配置的请求路径。
func protocolPath(channel model.Channel, protocol model.Protocol) string {
	switch protocol {
	case model.ProtocolOpenAIChatCompletion:
		return channel.OpenAIChatCompletionPath
	case model.ProtocolOpenAIResponse:
		return channel.OpenAIResponsePath
	case model.ProtocolAnthropicMessage:
		return channel.AnthropicMessagePath
	case model.ProtocolOpenAIEmbedding:
		return channel.OpenAIEmbeddingPath
	case model.ProtocolOpenAISpeech:
		return channel.OpenAISpeechPath
	case model.ProtocolOpenAITranscription:
		return channel.OpenAITranscriptionPath
	case model.ProtocolOpenAITranslation:
		return channel.OpenAITranslationPath
	case model.ProtocolOpenAIVideo:
		return channel.OpenAIVideoPath
	default:
		return ""
	}
}

// protocolURL 按渠道配置拼出该协议的上游地址; 路径已含版本前缀, 故地址不再追加版本。
func protocolURL(channel model.Channel, protocol model.Protocol) string {
	return strings.TrimRight(channel.BaseURL, "/") + protocolPath(channel, protocol)
}
