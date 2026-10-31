package relay

import (
	"bytes"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// buildMultipart 造一份多段表单正文, 文件部分保留自己的内容类型。
func buildMultipart(t *testing.T, fields map[string]string, fileName, fileType string, fileData []byte) ([]byte, string) {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %q: %v", name, err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+fileName+`"`)
	if fileType != "" {
		header.Set("Content-Type", fileType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(fileData); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return buffer.Bytes(), writer.FormDataContentType()
}

func TestRewriteMediaModelJSON(t *testing.T) {
	body := []byte(`{"model":"group-name","input":"hello","encoding_format":"float","dimensions":256}`)
	rewritten, contentType, err := rewriteMediaModel(body, "application/json", "text-embedding-3-small")
	if err != nil {
		t.Fatalf("rewrite media model: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("content type changed: %q", contentType)
	}
	var got map[string]any
	if err := json.Unmarshal(rewritten, &got); err != nil {
		t.Fatalf("decode rewritten body: %v", err)
	}
	if got["model"] != "text-embedding-3-small" {
		t.Fatalf("model not replaced: %v", got["model"])
	}
	// 模型之外的字段必须原样保留, 否则上游会收到被削过的请求。
	if got["input"] != "hello" || got["encoding_format"] != "float" {
		t.Fatalf("other fields lost: %v", got)
	}
	if got["dimensions"] != float64(256) {
		t.Fatalf("dimensions lost: %v", got["dimensions"])
	}
}

func TestRewriteMediaModelMultipart(t *testing.T) {
	fileData := []byte("ID3\x04\x00fake-audio-bytes")
	body, contentType := buildMultipart(t,
		map[string]string{"model": "group-name", "language": "zh", "response_format": "json"},
		"sample.mp3", "audio/mpeg", fileData)

	rewritten, newContentType, err := rewriteMediaModel(body, contentType, "whisper-1")
	if err != nil {
		t.Fatalf("rewrite media model: %v", err)
	}
	if !strings.HasPrefix(newContentType, "multipart/form-data; boundary=") {
		t.Fatalf("content type not rewritten: %q", newContentType)
	}
	if newContentType == contentType {
		t.Fatalf("boundary not regenerated: %q", newContentType)
	}

	mediaType, params, err := mime.ParseMediaType(newContentType)
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("unexpected media type: %q", mediaType)
	}
	reader := multipart.NewReader(bytes.NewReader(rewritten), params["boundary"])

	fields := map[string]string{}
	var gotFile []byte
	var gotFileType string
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if part.FileName() == "" {
			value := new(bytes.Buffer)
			if _, err := value.ReadFrom(part); err != nil {
				t.Fatalf("read field: %v", err)
			}
			fields[part.FormName()] = value.String()
			continue
		}
		gotFileType = part.Header.Get("Content-Type")
		value := new(bytes.Buffer)
		if _, err := value.ReadFrom(part); err != nil {
			t.Fatalf("read file: %v", err)
		}
		gotFile = value.Bytes()
	}
	if fields["model"] != "whisper-1" {
		t.Fatalf("model not replaced: %q", fields["model"])
	}
	if fields["language"] != "zh" || fields["response_format"] != "json" {
		t.Fatalf("other fields lost: %v", fields)
	}
	if !bytes.Equal(gotFile, fileData) {
		t.Fatalf("file content changed: %q", gotFile)
	}
	if gotFileType != "audio/mpeg" {
		t.Fatalf("file content type lost: %q", gotFileType)
	}
}

func TestRewriteMediaModelMultipartWithoutModel(t *testing.T) {
	body, contentType := buildMultipart(t, map[string]string{"language": "en"}, "a.wav", "", []byte("x"))
	rewritten, newContentType, err := rewriteMediaModel(body, contentType, "whisper-1")
	if err != nil {
		t.Fatalf("rewrite media model: %v", err)
	}
	_, params, err := mime.ParseMediaType(newContentType)
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	reader := multipart.NewReader(bytes.NewReader(rewritten), params["boundary"])
	found := false
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if part.FormName() != "model" {
			continue
		}
		value := new(bytes.Buffer)
		if _, err := value.ReadFrom(part); err != nil {
			t.Fatalf("read model field: %v", err)
		}
		found = true
		if value.String() != "whisper-1" {
			t.Fatalf("model field wrong: %q", value.String())
		}
	}
	if !found {
		t.Fatal("model field missing")
	}
}

func TestMediaUsage(t *testing.T) {
	usage := mediaUsage([]byte(`{"usage":{"prompt_tokens":12,"total_tokens":12}}`))
	if usage == nil || usage.PromptTokens != 12 || usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	// 转写类响应按输入输出计数。
	usage = mediaUsage([]byte(`{"usage":{"input_tokens":7,"output_tokens":3}}`))
	if usage == nil || usage.PromptTokens != 7 || usage.CompletionTokens != 3 {
		t.Fatalf("unexpected transcription usage: %+v", usage)
	}
	if mediaUsage([]byte(`{"object":"list"}`)) != nil {
		t.Fatal("usage expected to be absent")
	}
	if mediaUsage(nil) != nil {
		t.Fatal("usage expected to be absent for empty body")
	}
}

func TestProtocolURL(t *testing.T) {
	channel := model.Channel{ChannelConfig: model.ChannelConfig{
		BaseURL:                  "https://api.openai.com/",
		OpenAIEmbeddingPath:      "/v1/embeddings",
		OpenAISpeechPath:         "/v1/audio/speech",
		OpenAIVideoPath:          "/v1/videos",
		OpenAIChatCompletionPath: "/v1/chat/completions",
	}}
	cases := map[model.Protocol]string{
		model.ProtocolOpenAIEmbedding:      "https://api.openai.com/v1/embeddings",
		model.ProtocolOpenAISpeech:         "https://api.openai.com/v1/audio/speech",
		model.ProtocolOpenAIVideo:          "https://api.openai.com/v1/videos",
		model.ProtocolOpenAIChatCompletion: "https://api.openai.com/v1/chat/completions",
	}
	for protocol, want := range cases {
		if got := protocolURL(channel, protocol); got != want {
			t.Fatalf("protocol %d url = %q, want %q", protocol, got, want)
		}
	}
}

func TestPickGroupItemFiltersByProtocol(t *testing.T) {
	group := model.Group{
		ID:   1,
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ID: 1, Priority: 1, Protocols: model.ProtocolOpenAIChatCompletion},
			{ID: 2, Priority: 2, Protocols: model.ProtocolOpenAIEmbedding},
		},
	}
	// 只支持对话的成员不得承接嵌入请求。
	item := pickGroupItem(group, model.ProtocolOpenAIEmbedding)
	if item.ID != 2 {
		t.Fatalf("picked item %d, want 2", item.ID)
	}
	item = pickGroupItem(group, model.ProtocolOpenAIDialogs)
	if item.ID != 1 {
		t.Fatalf("picked item %d, want 1", item.ID)
	}
	// 没有任何成员支持视频时不得选出目标。
	if item := pickGroupItem(group, model.ProtocolOpenAIVideo); item.ID != 0 {
		t.Fatalf("picked item %d, want none", item.ID)
	}
}
