package relay

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/looplj/axonhub/llm"
)

// TestProbeSpec 探测的协议名与客户端协议形态的对应关系。
func TestProbeSpec(t *testing.T) {
	cases := []struct {
		protocol string
		wantName string
		wantFmt  llm.APIFormat
		media    bool
	}{
		{model.ProbeProtocolChat, model.ProbeProtocolChat, llm.APIFormatOpenAIChatCompletion, false},
		{model.ProbeProtocolResponse, model.ProbeProtocolResponse, llm.APIFormatOpenAIResponse, false},
		{model.ProbeProtocolAnthropic, model.ProbeProtocolAnthropic, llm.APIFormatAnthropicMessage, false},
		{model.ProbeProtocolEmbedding, model.ProbeProtocolEmbedding, llm.APIFormatOpenAIEmbedding, true},
		{"CHAT", model.ProbeProtocolChat, llm.APIFormatOpenAIChatCompletion, false},
	}
	for _, c := range cases {
		spec, err := probeSpec(c.protocol)
		if err != nil {
			t.Fatalf("probeSpec(%q): %v", c.protocol, err)
		}
		if spec.name != c.wantName {
			t.Errorf("probeSpec(%q) 协议名 = %q, 期望 %q", c.protocol, spec.name, c.wantName)
		}
		if spec.format != c.wantFmt {
			t.Errorf("probeSpec(%q) 格式 = %v, 期望 %v", c.protocol, spec.format, c.wantFmt)
		}
		if spec.media != c.media {
			t.Errorf("probeSpec(%q) 媒体类 = %v, 期望 %v", c.protocol, spec.media, c.media)
		}
	}

	if _, err := probeSpec("nope"); err == nil {
		t.Error("未知协议名应当报错")
	}
	if _, err := probeSpec(""); err == nil {
		t.Error("空协议名应当由 probeSpecFor 按授权推定, 不该直接解析成功")
	}
}

// TestProbeSpecFor 协议留空时按授权声明的协议推定客户端协议。
func TestProbeSpecFor(t *testing.T) {
	cases := []struct {
		bits model.Protocol
		want string
	}{
		{model.ProtocolOpenAIChatCompletion, model.ProbeProtocolChat},
		{model.ProtocolOpenAIResponse, model.ProbeProtocolResponse},
		{model.ProtocolAnthropicMessage, model.ProbeProtocolAnthropic},
		{model.ProtocolOpenAIEmbedding, model.ProbeProtocolEmbedding},
		// 同时声明对话与嵌入时, 对话类优先: 对话的端点更常用, 且嵌入请求需要显式选择。
		{model.ProtocolOpenAIChatCompletion | model.ProtocolOpenAIEmbedding, model.ProbeProtocolChat},
		{model.ProtocolOpenAIResponse | model.ProtocolOpenAIEmbedding, model.ProbeProtocolResponse},
	}
	for _, c := range cases {
		spec, err := probeSpecFor("", c.bits)
		if err != nil {
			t.Fatalf("probeSpecFor(\"\", %d): %v", c.bits, err)
		}
		if spec.name != c.want {
			t.Errorf("probeSpecFor(\"\", %d) 协议名 = %q, 期望 %q", c.bits, spec.name, c.want)
		}
	}

	// 明确的协议名优先于推定。
	spec, err := probeSpecFor(model.ProbeProtocolAnthropic, model.ProtocolOpenAIChatCompletion)
	if err != nil {
		t.Fatalf("probeSpecFor: %v", err)
	}
	if spec.name != model.ProbeProtocolAnthropic {
		t.Errorf("probeSpecFor 应尊重明确给出的协议, 实际 %q", spec.name)
	}

	if _, err := probeSpecFor("", 0); err == nil {
		t.Error("授权没有任何协议时应当报错")
	}
}

// TestProbeRequestBody 探测请求的正文形状: 对话类给 messages, 嵌入给 input。
func TestProbeRequestBody(t *testing.T) {
	chat, err := probeRequestBody(probeSpecOf(llm.APIFormatOpenAIChatCompletion), "gpt-4o", "hi")
	if err != nil {
		t.Fatalf("chat body: %v", err)
	}
	var chatBody map[string]any
	if err := json.Unmarshal(chat, &chatBody); err != nil {
		t.Fatalf("unmarshal chat body: %v", err)
	}
	if chatBody["model"] != "gpt-4o" {
		t.Errorf("chat body model = %v", chatBody["model"])
	}
	messages, ok := chatBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("chat body 应带一条 messages: %v", chatBody)
	}
	message := messages[0].(map[string]any)
	if message["role"] != "user" || message["content"] != "hi" {
		t.Errorf("chat message = %v", message)
	}

	embedding, err := probeRequestBody(probeSpecOf(llm.APIFormatOpenAIEmbedding), "bge-m3", "hi")
	if err != nil {
		t.Fatalf("embedding body: %v", err)
	}
	var embedBody map[string]any
	if err := json.Unmarshal(embedding, &embedBody); err != nil {
		t.Fatalf("unmarshal embedding body: %v", err)
	}
	if embedBody["input"] != "hi" {
		t.Errorf("embedding body input = %v", embedBody["input"])
	}
	if _, ok := embedBody["messages"]; ok {
		t.Error("embedding body 不应带 messages")
	}

	anthropic, err := probeRequestBody(probeSpecOf(llm.APIFormatAnthropicMessage), "claude", "hi")
	if err != nil {
		t.Fatalf("anthropic body: %v", err)
	}
	var anthropicBody map[string]any
	if err := json.Unmarshal(anthropic, &anthropicBody); err != nil {
		t.Fatalf("unmarshal anthropic body: %v", err)
	}
	if _, ok := anthropicBody["max_tokens"]; !ok {
		t.Error("anthropic body 缺少 max_tokens")
	}
}

// TestProbeContent 从各协议的响应里取可读正文。
func TestProbeContent(t *testing.T) {
	cases := []struct {
		format llm.APIFormat
		body   string
		want   string
	}{
		{llm.APIFormatOpenAIChatCompletion, `{"choices":[{"message":{"content":"hello"}}]}`, "hello"},
		{llm.APIFormatOpenAIResponse, `{"output":[{"content":[{"type":"output_text","text":"hello"}]}]}`, "hello"},
		{llm.APIFormatOpenAIResponse, `{"output_text":"hello"}`, "hello"},
		{llm.APIFormatAnthropicMessage, `{"content":[{"type":"text","text":"hello"}]}`, "hello"},
	}
	for _, c := range cases {
		if got := probeContent(c.format, []byte(c.body)); got != c.want {
			t.Errorf("probeContent(%v) = %q, 期望 %q", c.format, got, c.want)
		}
	}

	embedding := probeContent(llm.APIFormatOpenAIEmbedding, []byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	if embedding == "" {
		t.Error("嵌入响应应给出维度说明")
	}
}

// TestPickGroupItemPreview 只读选路: 手动模式取当前成员, 故障转移模式按优先级取, 且不改任何状态。
func TestPickGroupItemPreview(t *testing.T) {
	items := []model.GroupItem{
		{ID: 1, ChannelGrantID: 11, Protocols: model.ProtocolOpenAIChatCompletion, Available: true},
		{ID: 2, ChannelGrantID: 12, Protocols: model.ProtocolOpenAIChatCompletion, Available: true},
	}
	manual := model.Group{ID: 1, Mode: model.GroupModeManual, ActiveItemID: 2, Items: items}
	got := pickGroupItemPreview(manual, model.ProtocolOpenAIDialogs)
	if got.ID != 2 {
		t.Errorf("手动模式应取当前成员 2, 实际 %d", got.ID)
	}

	// 当前成员未指定时手动模式不选任何成员。
	manual.ActiveItemID = 0
	if got := pickGroupItemPreview(manual, model.ProtocolOpenAIDialogs); got.ID != 0 {
		t.Errorf("手动模式未指定当前成员时应取零值, 实际 %d", got.ID)
	}

	failover := model.Group{ID: 2, Mode: model.GroupModeFailover, Items: items}
	if got := pickGroupItemPreview(failover, model.ProtocolOpenAIDialogs); got.ID != 1 {
		t.Errorf("故障转移模式应取优先级最高的成员 1, 实际 %d", got.ID)
	}

	// 协议不匹配的成员不参与选路。
	if got := pickGroupItemPreview(failover, model.ProtocolOpenAIEmbedding); got.ID != 0 {
		t.Errorf("无成员支持嵌入协议时应取零值, 实际 %d", got.ID)
	}

	// 不可用的成员不参与选路。
	unavailable := model.Group{ID: 3, Mode: model.GroupModeFailover, Items: []model.GroupItem{
		{ID: 1, Protocols: model.ProtocolOpenAIChatCompletion, Available: false},
		{ID: 2, Protocols: model.ProtocolOpenAIChatCompletion, Available: true},
	}}
	if got := pickGroupItemPreview(unavailable, model.ProtocolOpenAIDialogs); got.ID != 2 {
		t.Errorf("不可用成员应被跳过, 实际取 %d", got.ID)
	}

	// 预览不得写回任何进程内状态。
	if _, ok := routes[failover.ID]; ok {
		t.Error("只读预览不应创建路由状态")
	}
}
