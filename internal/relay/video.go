package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/gin-gonic/gin"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/tidwall/gjson"
)

// 视频生成是异步的: 创建只拿到任务 ID, 之后查询与删除都要回到当初承接该任务的渠道与凭据。
// 故创建时登记任务与承接方, 查询与删除按任务 ID 取回承接方, 直接向同一个上游发起请求。

// recordVideoTask 在视频任务创建成功后登记任务与承接它的上游成员。
// 登记失败不影响本次响应: 客户端已经拿到任务 ID, 之后的查询与删除只是无法定位上游。
func recordVideoTask(groupName string, channel model.Channel, channelModel *model.ChannelModel, channelKey model.ChannelKey, apiKeyID int, body []byte, ctx context.Context) {
	taskID := gjson.GetBytes(body, "id").String()
	if taskID == "" {
		return
	}
	task := model.VideoTask{
		TaskID:   taskID,
		Model:    groupName,
		Status:   gjson.GetBytes(body, "status").String(),
		APIKeyID: apiKeyID,
	}
	if channelModel != nil {
		task.ChannelModelID = channelModel.ID
		task.ChannelModel = channelModel.Name
	}
	task.ChannelID = channel.ID
	task.ChannelKeyID = channelKey.ID
	_ = op.VideoTaskCreate(task, ctx)
}

// VideoTaskRetrieve 按任务 ID 查询上游的视频任务。
func VideoTaskRetrieve() gin.HandlerFunc {
	return func(c *gin.Context) {
		task, ok := resolveVideoTask(c)
		if !ok {
			return
		}
		response, err := sendVideoTaskRequest(c.Request.Context(), task, http.MethodGet)
		if err != nil {
			rejectVideoTask(c, err)
			return
		}
		// 上游回报的状态随任务一并记下, 便于后续查看任务进展而无需再问上游。
		if status := gjson.GetBytes(response.Body, "status").String(); status != "" {
			_ = op.VideoTaskUpdateStatus(task.TaskID, status, c.Request.Context())
		}
		writeVideoTaskResponse(c, response)
	}
}

// VideoTaskDelete 按任务 ID 删除上游的视频任务。
func VideoTaskDelete() gin.HandlerFunc {
	return func(c *gin.Context) {
		task, ok := resolveVideoTask(c)
		if !ok {
			return
		}
		response, err := sendVideoTaskRequest(c.Request.Context(), task, http.MethodDelete)
		if err != nil {
			rejectVideoTask(c, err)
			return
		}
		// 上游已确认删除后一并清掉本地登记, 避免留下指向已删任务的记录。
		_ = op.VideoTaskDelete(task.TaskID, c.Request.Context())
		writeVideoTaskResponse(c, response)
	}
}

// resolveVideoTask 取出路径上的任务 ID 并定位承接它的上游成员; 失败时已向客户端给出错误。
func resolveVideoTask(c *gin.Context) (model.VideoTask, bool) {
	taskID := c.Param("id")
	if taskID == "" {
		rejectVideoTask(c, errors.New("video task id is required"))
		return model.VideoTask{}, false
	}
	task, err := op.VideoTaskGet(taskID, c.Request.Context())
	if err != nil {
		rejectVideoTask(c, fmt.Errorf("video task %s not found", taskID))
		return model.VideoTask{}, false
	}
	// API Key 限定了模型范围时, 该任务所属的分组也须在范围内。
	if allowed, ok := c.Get("supported_models"); ok {
		if names, _ := allowed.([]string); len(names) > 0 && !slices.Contains(names, task.Model) {
			rejectVideoTask(c, errors.New("model not supported by this api key"))
			return model.VideoTask{}, false
		}
	}
	return task, true
}

// sendVideoTaskRequest 向承接该任务的渠道发起查询或删除请求。
func sendVideoTaskRequest(ctx context.Context, task model.VideoTask, method string) (*httpclient.Response, error) {
	channel, err := op.ChannelGet(task.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("channel %d not found", task.ChannelID)
	}
	channelKey, err := op.ChannelKeyGet(task.ChannelKeyID)
	if err != nil {
		return nil, fmt.Errorf("channel key %d not found", task.ChannelKeyID)
	}
	if !channelKey.Enabled {
		return nil, fmt.Errorf("channel key %d is disabled", channelKey.ID)
	}

	request := &httpclient.Request{
		Method:    method,
		URL:       protocolURL(channel, model.ProtocolOpenAIVideo) + "/" + task.TaskID,
		Headers:   http.Header{"Accept": []string{"application/json"}},
		Auth:      &httpclient.AuthConfig{Type: httpclient.AuthTypeBearer, APIKey: channelKey.Key},
		APIFormat: llm.APIFormatOpenAIVideo.String(),
	}
	request.Headers.Set("Content-Type", "application/json")

	httpClient, closeIdle, err := resolveUpstreamClient(channel)
	if err != nil {
		return nil, err
	}
	// 非流式响应在 Do 返回时正文已读完, 连接可以立即归还。
	if closeIdle != nil {
		defer closeIdle()
	}

	response, err := httpclient.NewHttpClientWithClient(httpClient).Do(ctx, request)
	if err != nil {
		var failure *httpclient.Error
		if errors.As(err, &failure) && len(failure.Body) > 0 {
			return nil, fmt.Errorf("%w: %s", err, failure.Body)
		}
		return nil, err
	}
	return response, nil
}

// writeVideoTaskResponse 把上游的任务响应原样写回客户端。
func writeVideoTaskResponse(c *gin.Context, response *httpclient.Response) {
	for key, values := range response.Headers {
		if key == "Content-Length" {
			continue
		}
		c.Writer.Header()[key] = values
	}
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Header("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(response.StatusCode)
	_, _ = c.Writer.Write(response.Body)
}

// rejectVideoTask 以 OpenAI 错误格式返回视频任务的失败。
func rejectVideoTask(c *gin.Context, err error) {
	c.Data(http.StatusBadRequest, "application/json", videoTaskErrorBody(err))
	c.Abort()
}

// videoTaskErrorBody 生成 OpenAI 格式的错误正文。
func videoTaskErrorBody(err error) []byte {
	return []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"invalid_request_error"}}`, err.Error()))
}
