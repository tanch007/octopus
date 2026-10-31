package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

// 端到端用例共用一套环境: 数据库, 模拟上游, 渠道, 分组与 API Key 都是进程内单例,
// 建一次即可; 各用例只替换模拟上游的处理逻辑, 由它按路径给出各自的响应。

var (
	upstreamMu      sync.Mutex
	upstreamHandler http.Handler
	upstreamCalls   []string
	upstreamServer  *httptest.Server
)

// TestMain 准备测试用的数据库与模拟上游; 整轮用例共用它们, 故在跑用例之前建好, 跑完之后再收。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "octopus-relay-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	if err := db.InitDB("sqlite", filepath.Join(dir, "octopus.db"), false); err != nil {
		panic(err)
	}
	upstreamServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamMu.Lock()
		upstreamCalls = append(upstreamCalls, r.Method+" "+r.URL.Path)
		handler := upstreamHandler
		upstreamMu.Unlock()
		if handler == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	code := m.Run()
	upstreamServer.Close()
	os.Exit(code)
}

// setUpstream 换掉模拟上游的处理逻辑, 并清空此前记录的上游调用。
func setUpstream(handler http.Handler) {
	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	upstreamHandler = handler
	upstreamCalls = nil
}

// upstreamCalled 取回模拟上游收到的调用序列。
func upstreamCalled() []string {
	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	return append([]string(nil), upstreamCalls...)
}

// newTestEngine 起一个指向模拟上游的渠道与分组, 并按真实的路由表挂上处理器。
func newTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	server := upstreamServer

	ctx := t.Context()
	detail := &model.ChannelDetail{
		ChannelConfig: model.ChannelConfig{
			Name:                     "test",
			Dialect:                  model.DialectGeneric,
			Enabled:                  true,
			BaseURL:                  server.URL,
			OpenAIChatCompletionPath: "/v1/chat/completions",
			OpenAIResponsePath:       "/v1/responses",
			AnthropicMessagePath:     "/v1/messages",
			OpenAIEmbeddingPath:      "/v1/embeddings",
			OpenAISpeechPath:         "/v1/audio/speech",
			OpenAITranscriptionPath:  "/v1/audio/transcriptions",
			OpenAITranslationPath:    "/v1/audio/translations",
			OpenAIVideoPath:          "/v1/videos",
		},
		Keys:   []model.ChannelKeyConfig{{Name: "default", Key: "sk-test", Enabled: true}},
		Models: []string{"upstream-model"},
		Grants: []model.ChannelGrantConfig{{
			ModelName: "upstream-model",
			KeyName:   "default",
			Protocols: model.ProtocolOpenAIDialogs |
				model.ProtocolOpenAIEmbedding |
				model.ProtocolOpenAISpeech |
				model.ProtocolOpenAITranscription |
				model.ProtocolOpenAITranslation |
				model.ProtocolOpenAIVideo,
		}},
	}
	if _, err := op.ChannelCreate(detail, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	// 授权主键由渠道分配, 组员按它引用。
	var grantID int
	for _, candidate := range op.ChannelGrantCandidates() {
		if candidate.ModelName == "upstream-model" {
			grantID = candidate.ID
		}
	}
	if grantID == 0 {
		t.Fatal("channel grant not found")
	}
	if _, err := op.GroupCreate(&model.GroupCreateRequest{
		Name:  "test-group",
		Mode:  model.GroupModeManual,
		Items: []model.GroupItemInput{{ChannelGrantID: grantID}},
	}, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	group, err := op.GroupGetByName("test-group")
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	// 手动模式要指明当前成员, 否则请求找不到承载它的成员。
	itemID := group.Items[0].ID
	if _, err := op.GroupUpdate(group.ID, &model.GroupUpdateRequest{ActiveItemID: &itemID}, ctx); err != nil {
		t.Fatalf("set active item: %v", err)
	}
	if err := op.APIKeyCreate(&model.APIKey{
		Name:    "test",
		APIKey:  "sk-octopus-test",
		Enabled: true,
	}, ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	engine := gin.New()
	if err := router.RegisterAll(engine); err != nil {
		t.Fatalf("register routes: %v", err)
	}
	return engine
}

// testEngine 是共用引擎: 路由表只注册一次, 故引擎也只能有一个。
var testEngine *gin.Engine

// engine 取共用引擎, 首次调用时建好整套环境。
func engine(t *testing.T) *gin.Engine {
	t.Helper()
	if testEngine == nil {
		testEngine = newTestEngine(t)
	}
	return testEngine
}

// do 发起一次带鉴权的请求。
func do(t *testing.T, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Authorization", "Bearer sk-octopus-test")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	engine(t).ServeHTTP(recorder, request)
	return recorder
}

func TestEmbeddingEndpoint(t *testing.T) {
	var gotBody []byte
	var gotAuth string
	setUpstream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"upstream-model","usage":{"prompt_tokens":7,"total_tokens":7}}`))
	}))

	response := do(t, http.MethodPost, "/v1/embeddings", "application/json",
		[]byte(`{"model":"test-group","input":"hello","encoding_format":"float"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	// 上游拿到的必须是渠道绑定的凭据, 而不是客户端带来的那把。
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("upstream authorization = %q", gotAuth)
	}
	// 客户端给的分组名必须换成上游真实模型名, 其余字段原样送达。
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	if sent["model"] != "upstream-model" {
		t.Fatalf("upstream model = %v", sent["model"])
	}
	if sent["input"] != "hello" || sent["encoding_format"] != "float" {
		t.Fatalf("upstream body = %s", gotBody)
	}
	if !strings.Contains(response.Body.String(), `"prompt_tokens":7`) {
		t.Fatalf("response body = %s", response.Body.String())
	}
	if calls := upstreamCalled(); len(calls) != 1 || calls[0] != "POST /v1/embeddings" {
		t.Fatalf("upstream calls = %v", calls)
	}
}

func TestSpeechEndpoint(t *testing.T) {
	var gotBody []byte
	setUpstream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte{0xff, 0xfb, 0x90, 0x00})
	}))

	response := do(t, http.MethodPost, "/v1/audio/speech", "application/json",
		[]byte(`{"model":"test-group","input":"hello","voice":"alloy","response_format":"mp3"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "audio/mpeg") {
		t.Fatalf("response content type = %q", contentType)
	}
	if !bytes.Equal(response.Body.Bytes(), []byte{0xff, 0xfb, 0x90, 0x00}) {
		t.Fatalf("response body = %v", response.Body.Bytes())
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	if sent["model"] != "upstream-model" || sent["voice"] != "alloy" {
		t.Fatalf("upstream body = %s", gotBody)
	}
}

func TestTranscriptionEndpoint(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	setUpstream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"这是一段中文"}`))
	}))

	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	writer.WriteField("model", "test-group")
	writer.WriteField("language", "zh")
	part, _ := writer.CreateFormFile("file", "sample.mp3")
	part.Write([]byte("fake-audio"))
	writer.Close()

	response := do(t, http.MethodPost, "/v1/audio/transcriptions", writer.FormDataContentType(), buffer.Bytes())
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Fatalf("upstream content type = %q", gotContentType)
	}
	// 上游收到的是重建后的表单: 模型名换成上游真实模型, 其余字段与文件内容不变。
	_, params, err := mime.ParseMediaType(gotContentType)
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	reader := multipart.NewReader(bytes.NewReader(gotBody), params["boundary"])
	fields := map[string]string{}
	fileData := []byte{}
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		value := new(bytes.Buffer)
		value.ReadFrom(part)
		if part.FileName() == "" {
			fields[part.FormName()] = value.String()
			continue
		}
		fileData = value.Bytes()
	}
	if fields["model"] != "upstream-model" {
		t.Fatalf("upstream model field = %q", fields["model"])
	}
	if fields["language"] != "zh" {
		t.Fatalf("upstream language field = %q", fields["language"])
	}
	if string(fileData) != "fake-audio" {
		t.Fatalf("upstream file = %q", fileData)
	}
	if !strings.Contains(response.Body.String(), "这是一段中文") {
		t.Fatalf("response body = %s", response.Body.String())
	}
}

func TestVideoEndpointLifecycle(t *testing.T) {
	setUpstream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			w.Write([]byte(`{"id":"video_123","object":"video","status":"queued"}`))
		case http.MethodGet:
			w.Write([]byte(`{"id":"video_123","object":"video","status":"in_progress","progress":0.4}`))
		default:
			w.Write([]byte(`{"id":"video_123","object":"video","status":"cancelled"}`))
		}
	}))

	response := do(t, http.MethodPost, "/v1/videos", "application/json",
		[]byte(`{"model":"test-group","prompt":"a cat","seconds":"4","size":"1024x1024"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "video_123") {
		t.Fatalf("create body = %s", response.Body.String())
	}

	// 任务与承接它的渠道已登记, 之后的查询与删除要回到同一个上游。
	response = do(t, http.MethodGet, "/v1/videos/video_123", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "in_progress") {
		t.Fatalf("get body = %s", response.Body.String())
	}

	response = do(t, http.MethodDelete, "/v1/videos/video_123", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", response.Code, response.Body.String())
	}
	want := []string{
		"POST /v1/videos",
		"GET /v1/videos/video_123",
		"DELETE /v1/videos/video_123",
	}
	calls := upstreamCalled()
	if len(calls) != len(want) {
		t.Fatalf("upstream calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("upstream calls = %v, want %v", calls, want)
		}
	}
}

func TestVideoTaskNotFound(t *testing.T) {
	setUpstream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	response := do(t, http.MethodGet, "/v1/videos/video_missing", "", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if calls := upstreamCalled(); len(calls) != 0 {
		t.Fatalf("upstream should not be called: %v", calls)
	}
}

func TestModelNotInGroup(t *testing.T) {
	setUpstream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	response := do(t, http.MethodPost, "/v1/embeddings", "application/json",
		[]byte(`{"model":"no-such-group","input":"hello"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
