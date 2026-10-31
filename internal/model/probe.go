package model

// 探测请求使用的客户端协议; 取值与前端协议选择一致。
// 客户端协议只决定请求的形态, 实际发往上游的协议由渠道授权自己声明的协议集合决定:
// 对话类协议之间可互相转换, 媒体类协议各自独立, 没有转换伙伴。
const (
	ProbeProtocolChat      = "chat"      // OpenAI Chat Completions。
	ProbeProtocolResponse  = "response"  // OpenAI Responses。
	ProbeProtocolAnthropic = "anthropic" // Anthropic Messages。
	ProbeProtocolEmbedding = "embedding" // OpenAI Embeddings。
)

// ProbeModelRequest 是模型探测请求: 指定渠道下的某个模型与凭据, 直连该授权测试上游。
// 模型与凭据按名称定位, 与界面上的读法一致, 前端无需持有渠道模型与凭据的主键。
type ProbeModelRequest struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name"`
	KeyName   string `json:"key_name"`
	Protocol  string `json:"protocol"` // 留空则由后端按授权支持的协议推定。
	Prompt    string `json:"prompt"`
}

// ProbeGroupRequest 是分组探测请求: 按分组的路由规则挑一个成员来测, 看真实请求此刻会落到谁身上。
type ProbeGroupRequest struct {
	GroupID  int    `json:"group_id"`
	Protocol string `json:"protocol"` // 留空则由后端按成员授权支持的协议推定。
	Prompt   string `json:"prompt"`
}

// ProbeResult 是探测结果。
// 探测不计入渠道与凭据的统计, 也不写入转发日志: 它是诊断动作, 不能污染真实流量的账本。
// 故此处只给本次调用的即时结果, 失败时错误与上游原文一并带出, 便于直接看出原因。
type ProbeResult struct {
	OK             bool   `json:"ok"`
	ChannelID      int    `json:"channel_id"`
	ChannelName    string `json:"channel_name"`
	ModelName      string `json:"model_name"`
	KeyName        string `json:"key_name"`
	Protocol       string `json:"protocol"`        // 实际发往上游的协议。
	ClientProtocol string `json:"client_protocol"` // 探测请求使用的客户端协议。
	StatusCode     int    `json:"status_code"`
	DurationMS     int64  `json:"duration_ms"`
	InputTokens    int64  `json:"input_tokens"`
	OutputTokens   int64  `json:"output_tokens"`
	Content        string `json:"content"`      // 上游响应里取出的正文。
	RawResponse    string `json:"raw_response"` // 上游原始响应, 失败时是错误正文。
	Error          string `json:"error"`
}
