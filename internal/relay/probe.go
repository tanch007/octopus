package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/tidwall/gjson"
)

// 探测是诊断动作, 不是真实流量:
// 它不计渠道与凭据的统计, 不写转发日志, 也不改分组的路由状态(当前成员, 冷却与亲和都不动)。
// 否则一次排查就会污染账本, 并把真实请求的选路带偏。

// defaultProbePrompt 是探测请求未给出提示词时使用的默认文本。
const defaultProbePrompt = "Hello"

// probeTimeoutSeconds 是单次探测的上游等待上限; 超时按失败返回, 不再重试。
const probeTimeoutSeconds = 60

// ProbeModel 直连指定渠道下的某个模型与凭据测试上游。
// 模型与凭据按名称定位, 与界面上的读法一致, 调用方无需持有它们的主键。
func ProbeModel(ctx context.Context, req model.ProbeModelRequest) model.ProbeResult {
	result := model.ProbeResult{ModelName: req.ModelName, KeyName: req.KeyName}

	channel, err := op.ChannelGet(req.ChannelID)
	if err != nil {
		return probeFailure(result, err)
	}
	result.ChannelID = channel.ID
	result.ChannelName = channel.Name

	grant, err := op.ChannelGrantGetByNames(channel.ID, req.ModelName, req.KeyName)
	if err != nil {
		return probeFailure(result, err)
	}
	result.ModelName = grant.ChannelModel.Name
	result.KeyName = grant.ChannelKey.Name

	return probeGrant(ctx, result, channel, grant, req.Protocol, req.Prompt)
}

// ProbeGroup 按分组的路由规则挑一个成员来测, 看真实请求此刻会落到谁身上。
// 选路只读: 探测不占用恢复探测位, 不改当前成员, 失败的成员也不会因此进入冷却。
func ProbeGroup(ctx context.Context, req model.ProbeGroupRequest) model.ProbeResult {
	result := model.ProbeResult{}

	group, err := op.GroupGet(req.GroupID)
	if err != nil {
		return probeFailure(result, err)
	}
	result.ModelName = group.Name

	// 协议留空时先不限定, 按成员各自声明的协议挑一个出来; 用哪个客户端协议由选中成员的授权推定。
	// 协议明确时只挑能承接该协议的成员, 与真实选路同一口径。
	mask := model.ProtocolOpenAIDialogs | model.ProtocolOpenAIEmbedding
	if strings.TrimSpace(req.Protocol) != "" {
		spec, err := probeSpec(req.Protocol)
		if err != nil {
			return probeFailure(result, err)
		}
		mask = spec.mask
	}

	item := pickGroupItemPreview(group, mask)
	if item.ID == 0 {
		return probeFailure(result, errors.New("no group member available for this protocol"))
	}
	result.ChannelID = item.ChannelID
	result.ChannelName = item.ChannelName
	result.ModelName = item.ModelName
	result.KeyName = item.KeyName

	grant, err := op.ChannelGrantGet(item.ChannelGrantID)
	if err != nil {
		return probeFailure(result, err)
	}
	channel, err := op.ChannelGet(grant.ChannelModel.ChannelID)
	if err != nil {
		return probeFailure(result, err)
	}
	result.ChannelID = channel.ID
	result.ChannelName = channel.Name

	return probeGrant(ctx, result, channel, grant, req.Protocol, req.Prompt)
}

// probeGrant 按授权构造出站请求并执行一次探测。
// 实际发往上游的协议由授权声明的协议集合决定: 对话类之间可互相转换, 媒体类必须精确匹配。
func probeGrant(ctx context.Context, result model.ProbeResult, channel model.Channel, grant model.ChannelGrant, protocol, prompt string) model.ProbeResult {
	spec, err := probeSpecFor(protocol, grant.Protocols)
	if err != nil {
		return probeFailure(result, err)
	}
	result.ClientProtocol = spec.name

	// 授权支持的协议必须能承接本次请求: 媒体类协议没有转换伙伴, 必须精确声明该协议位;
	// 对话类成员只要有任一对话协议即可承接。
	if spec.media {
		if grant.Protocols&spec.protocol == 0 {
			return probeFailure(result, fmt.Errorf("grant does not support %s protocol", spec.name))
		}
	} else if grant.Protocols&model.ProtocolOpenAIDialogs == 0 {
		return probeFailure(result, errors.New("grant supports no dialog protocol"))
	}

	outbound, targetProtocol, passthrough, err := buildOutbound(channel, grant, *grant.ChannelKey, spec.protocol)
	if err != nil {
		return probeFailure(result, err)
	}
	result.Protocol = protocolName(targetProtocol)

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = defaultProbePrompt
	}
	clientBody, err := probeRequestBody(spec, grant.ChannelModel.Name, prompt)
	if err != nil {
		return probeFailure(result, err)
	}
	raw := &httpclient.Request{
		Method:  http.MethodPost,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		Body:    clientBody,
		// URL 与认证由出站转换器按渠道配置决定, 此处只声明客户端协议形态。
		APIFormat: spec.format.String(),
	}
	upstreamBody, upstreamContentType, err := buildUpstreamBody(spec, raw, clientBody, grant.ChannelModel.Name)
	if err != nil {
		return probeFailure(result, err)
	}
	raw.Body = upstreamBody

	ctx, cancel := context.WithTimeout(ctx, probeTimeoutSeconds*time.Second)
	defer cancel()

	startedAt := time.Now()
	var response *upstreamResponse
	switch {
	case spec.media:
		response, err = sendMedia(ctx, spec, channel, *grant.ChannelKey, upstreamBody, upstreamContentType, "application/json", false)
	case passthrough:
		response, err = sendPassthrough(ctx, spec.format, raw, channel, outbound, false, grant.ChannelModel.Name)
	default:
		response, err = sendConverted(ctx, spec.format, raw, channel, outbound, false)
	}
	result.DurationMS = time.Since(startedAt).Milliseconds()

	if err != nil {
		result.Error = err.Error()
		result.StatusCode = probeStatusCode(err)
		result.RawResponse = probeErrorBody(err)
		return result
	}
	if response != nil && response.closeIdle != nil {
		response.closeIdle()
	}

	result.OK = true
	result.StatusCode = http.StatusOK
	result.RawResponse = string(response.body)
	result.Content = probeContent(spec.format, response.body)
	if response.usage != nil {
		result.InputTokens = response.usage.PromptTokens
		result.OutputTokens = response.usage.CompletionTokens
	}
	return result
}

// probeSpecFor 解析探测使用的客户端协议。
// 留空时按授权声明的协议推定: 对话类优先 Chat, 其次 Responses, 再次 Anthropic; 媒体类只认嵌入。
// 推定只影响请求的形态, 实际发往上游的协议仍由授权决定。
func probeSpecFor(protocol string, bits model.Protocol) (protocolSpec, error) {
	if strings.TrimSpace(protocol) != "" {
		return probeSpec(protocol)
	}
	switch {
	case bits&model.ProtocolOpenAIChatCompletion != 0:
		return probeSpecOf(llm.APIFormatOpenAIChatCompletion), nil
	case bits&model.ProtocolOpenAIResponse != 0:
		return probeSpecOf(llm.APIFormatOpenAIResponse), nil
	case bits&model.ProtocolAnthropicMessage != 0:
		return probeSpecOf(llm.APIFormatAnthropicMessage), nil
	case bits&model.ProtocolOpenAIEmbedding != 0:
		return probeSpecOf(llm.APIFormatOpenAIEmbedding), nil
	default:
		return protocolSpec{}, errors.New("grant supports no known protocol")
	}
}

// probeSpec 把请求给出的客户端协议名解析成转发规格。
func probeSpec(protocol string) (protocolSpec, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case model.ProbeProtocolChat:
		return probeSpecOf(llm.APIFormatOpenAIChatCompletion), nil
	case model.ProbeProtocolResponse:
		return probeSpecOf(llm.APIFormatOpenAIResponse), nil
	case model.ProbeProtocolAnthropic:
		return probeSpecOf(llm.APIFormatAnthropicMessage), nil
	case model.ProbeProtocolEmbedding:
		return probeSpecOf(llm.APIFormatOpenAIEmbedding), nil
	default:
		return protocolSpec{}, fmt.Errorf("unknown probe protocol: %s", protocol)
	}
}

// probeSpecOf 取指定客户端协议的转发规格, 并补上协议名供界面展示。
func probeSpecOf(format llm.APIFormat) protocolSpec {
	spec := protocolSpecOf(format)
	spec.name = probeProtocolName(format)
	return spec
}

// probeProtocolName 把客户端协议格式映射成协议名。
func probeProtocolName(format llm.APIFormat) string {
	switch format {
	case llm.APIFormatOpenAIResponse:
		return model.ProbeProtocolResponse
	case llm.APIFormatAnthropicMessage:
		return model.ProbeProtocolAnthropic
	case llm.APIFormatOpenAIEmbedding:
		return model.ProbeProtocolEmbedding
	default:
		return model.ProbeProtocolChat
	}
}

// protocolName 把上游协议位映射成协议名。
func protocolName(protocol model.Protocol) string {
	switch protocol {
	case model.ProtocolOpenAIChatCompletion:
		return model.ProbeProtocolChat
	case model.ProtocolOpenAIResponse:
		return model.ProbeProtocolResponse
	case model.ProtocolAnthropicMessage:
		return model.ProbeProtocolAnthropic
	case model.ProtocolOpenAIEmbedding:
		return model.ProbeProtocolEmbedding
	default:
		return ""
	}
}

// probeRequestBody 按客户端协议构造探测请求的正文。
// 对话类给一条用户消息, 媒体类给一段输入文本; 嵌入协议需要 input 字段, 不走 messages。
func probeRequestBody(spec protocolSpec, modelName, prompt string) ([]byte, error) {
	switch spec.format {
	case llm.APIFormatOpenAIResponse:
		return json.Marshal(map[string]any{"model": modelName, "input": prompt})
	case llm.APIFormatAnthropicMessage:
		return json.Marshal(map[string]any{
			"model":      modelName,
			"max_tokens": 1024,
			"messages":   []map[string]any{{"role": "user", "content": prompt}},
		})
	case llm.APIFormatOpenAIEmbedding:
		return json.Marshal(map[string]any{"model": modelName, "input": prompt})
	default:
		return json.Marshal(map[string]any{
			"model":    modelName,
			"messages": []map[string]any{{"role": "user", "content": prompt}},
		})
	}
}

// probeContent 从上游响应里取出可读正文。
// 各协议的正文位置不同, 逐个按其形状取; 取不到时留空, 原始响应仍一并给出。
func probeContent(format llm.APIFormat, body []byte) string {
	switch format {
	case llm.APIFormatOpenAIResponse:
		if text := gjson.GetBytes(body, "output_text").String(); text != "" {
			return text
		}
		var parts []string
		for _, output := range gjson.GetBytes(body, "output").Array() {
			for _, content := range output.Get("content").Array() {
				if text := content.Get("text").String(); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case llm.APIFormatAnthropicMessage:
		var parts []string
		for _, content := range gjson.GetBytes(body, "content").Array() {
			if text := content.Get("text").String(); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case llm.APIFormatOpenAIEmbedding:
		// 嵌入结果是向量, 全文没有可读意义, 只给维度与前几维, 足以确认返回的是向量而非报错。
		embedding := gjson.GetBytes(body, "data.0.embedding")
		if !embedding.Exists() {
			return ""
		}
		values := embedding.Array()
		preview := make([]string, 0, 4)
		for i := 0; i < len(values) && i < 4; i++ {
			preview = append(preview, values[i].String())
		}
		return fmt.Sprintf("dimension %d, first values: %s", len(values), strings.Join(preview, ", "))
	default:
		return gjson.GetBytes(body, "choices.0.message.content").String()
	}
}

// probeStatusCode 从错误里取出上游的状态码; 取不到时返回 0。
func probeStatusCode(err error) int {
	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode
	}
	var responseErr *llm.ResponseError
	if errors.As(err, &responseErr) {
		return responseErr.StatusCode
	}
	return 0
}

// probeErrorBody 从错误里取出上游原文; 取不到时返回空串。
func probeErrorBody(err error) string {
	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) {
		return strings.TrimSpace(string(httpErr.Body))
	}
	return ""
}

// probeFailure 记下失败原因并返回结果。失败不是异常: 界面要展示的是失败详情, 而不是一个错误响应。
func probeFailure(result model.ProbeResult, err error) model.ProbeResult {
	result.OK = false
	result.Error = err.Error()
	return result
}
