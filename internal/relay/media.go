package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 媒体类协议没有转换伙伴, 请求体与响应体都不经统一模型重建: 客户端正文原样送往上游,
// 只把其中的模型名称换成上游真实模型。这样上游才能收到自己认识的全部字段,
// 嵌入的多种输入形态, 语音合成的参数与视频的附加字段都不会在转发中丢失。

// buildUpstreamBody 给出本轮上游请求的正文与内容类型。
// 对话类只改写 JSON 正文里的模型名称, 内容类型沿用客户端声明;
// 媒体类沿用客户端原文并只替换其中的模型字段, 内容类型在表单重编码后随新边界变化。
func buildUpstreamBody(spec protocolSpec, raw *httpclient.Request, clientBody []byte, modelName string) ([]byte, string, error) {
	contentType := mediaRequestHeader(raw)
	if !spec.media {
		body, err := sjson.SetBytes(raw.Body, "model", modelName)
		if err != nil {
			return nil, "", fmt.Errorf("rewrite model: %w", err)
		}
		return body, contentType, nil
	}
	return rewriteMediaModel(clientBody, contentType, modelName)
}

// rewriteMediaModel 把媒体类请求体中的模型名称替换为上游真实模型, 返回新正文与新的内容类型。
// JSON 正文按 JSON 路径改写, 内容类型不变; 多段表单按原字段顺序重编码, 内容类型随新的边界变化。
func rewriteMediaModel(body []byte, contentType, modelName string) ([]byte, string, error) {
	if isMultipartContentType(contentType) {
		return rewriteMultipartModel(body, contentType, modelName)
	}
	rewritten, err := sjson.SetBytes(body, "model", modelName)
	if err != nil {
		return nil, "", fmt.Errorf("rewrite model: %w", err)
	}
	return rewritten, contentType, nil
}

// rewriteMultipartModel 重写多段表单中的模型字段, 其余字段与文件按原顺序重编码。
// 文件部分连同其内容类型一并保留, 使上游收到的附件与客户端发出的一致。
func rewriteMultipartModel(body []byte, contentType, modelName string) ([]byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", fmt.Errorf("parse multipart content type: %w", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, "", errors.New("multipart content type has no boundary")
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	written := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read multipart: %w", err)
		}
		// 模型字段只保留一份: 重复出现时以后遇到的为准。
		if part.FormName() == "model" {
			if err := writer.WriteField("model", modelName); err != nil {
				return nil, "", fmt.Errorf("write model field: %w", err)
			}
			written = true
			continue
		}
		target, err := copyMultipartPart(writer, part)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.Copy(target, part); err != nil {
			return nil, "", fmt.Errorf("copy multipart part: %w", err)
		}
	}
	if !written {
		if err := writer.WriteField("model", modelName); err != nil {
			return nil, "", fmt.Errorf("write model field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

// copyMultipartPart 在新的表单中重建一个分段, 保留其字段名, 文件名与内容类型。
func copyMultipartPart(writer *multipart.Writer, part *multipart.Part) (io.Writer, error) {
	header := make(textproto.MIMEHeader)
	if part.FileName() != "" {
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeMultipartValue(part.FormName()), escapeMultipartValue(part.FileName())))
		if contentType := part.Header.Get("Content-Type"); contentType != "" {
			header.Set("Content-Type", contentType)
		}
	} else {
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"`, escapeMultipartValue(part.FormName())))
	}
	target, err := writer.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("create multipart part: %w", err)
	}
	return target, nil
}

// escapeMultipartValue 去掉分段头里的控制字符并转义引号与反斜杠, 免得客户端给出的文件名破坏头部结构。
func escapeMultipartValue(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	return strings.NewReplacer("\\", "\\\\", `"`, `\"`).Replace(cleaned)
}

// isMultipartContentType 判断内容类型是否为多段表单。
func isMultipartContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.HasPrefix(strings.ToLower(contentType), "multipart/")
	}
	return strings.HasPrefix(strings.ToLower(mediaType), "multipart/")
}

// isJSONContentType 判断内容类型是否为 JSON 正文; 未声明内容类型时按 JSON 处理, 与对话类请求一致。
func isJSONContentType(contentType string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	return trimmed == "" || strings.Contains(trimmed, "application/json")
}

// isBinaryStreamContentType 判断响应的内容类型是否为二进制分块流。
func isBinaryStreamContentType(contentType string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(trimmed, "audio/") || trimmed == "application/octet-stream"
}

// mediaUsage 从媒体类响应正文中取出用量: 嵌入与转写类响应带 usage 字段, 语音与视频不带。
func mediaUsage(body []byte) *llm.Usage {
	if len(body) == 0 {
		return nil
	}
	usage := gjson.ParseBytes(body).Get("usage")
	if !usage.Exists() {
		return nil
	}
	result := &llm.Usage{
		PromptTokens:     usage.Get("prompt_tokens").Int(),
		CompletionTokens: usage.Get("completion_tokens").Int(),
		TotalTokens:      usage.Get("total_tokens").Int(),
	}
	// 转写类响应按输入输出计数, 与嵌入类的提示词计数不同名。
	if result.PromptTokens == 0 {
		result.PromptTokens = usage.Get("input_tokens").Int()
	}
	if result.CompletionTokens == 0 {
		result.CompletionTokens = usage.Get("output_tokens").Int()
	}
	if result.TotalTokens == 0 {
		result.TotalTokens = result.PromptTokens + result.CompletionTokens
	}
	if result.PromptTokens == 0 && result.CompletionTokens == 0 && result.TotalTokens == 0 {
		return nil
	}
	return result
}

// mediaRequestHeader 取出媒体类请求的内容类型; 未声明时按 JSON 处理。
func mediaRequestHeader(raw *httpclient.Request) string {
	contentType := raw.Headers.Get("Content-Type")
	if contentType == "" {
		return "application/json"
	}
	return contentType
}
