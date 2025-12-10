package api

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"anti2api-golang/internal/converter"
	"anti2api-golang/internal/utils"
)

// StreamChunk 流式数据块
type StreamChunk struct {
	Type      string                     // thinking, text, tool_calls, done
	Content   string                     // 文本内容
	ToolCalls []converter.OpenAIToolCall // 工具调用
	Usage     *converter.UsageMetadata   // 使用统计
}

// StreamData 原始流式数据
type StreamData struct {
	Response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string                  `json:"text,omitempty"`
					FunctionCall *converter.FunctionCall `json:"functionCall,omitempty"`
					Thought      bool                    `json:"thought,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason,omitempty"`
		} `json:"candidates"`
		UsageMetadata *converter.UsageMetadata `json:"usageMetadata,omitempty"`
	} `json:"response"`
}

// ProcessStreamResponse 处理流式响应
func ProcessStreamResponse(resp *http.Response, callback func(chunk StreamChunk)) (*converter.UsageMetadata, error) {
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	scanner := bufio.NewScanner(reader)
	// 增大缓冲区以处理大的响应（16MB，支持图像生成等大数据）
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	var usage *converter.UsageMetadata
	var toolCalls []converter.OpenAIToolCall

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonData := line[6:]
		if jsonData == "[DONE]" {
			callback(StreamChunk{Type: "done"})
			break
		}

		var data StreamData
		if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
			continue
		}

		// 提取 usage
		if data.Response.UsageMetadata != nil {
			usage = data.Response.UsageMetadata
		}

		// 检查是否有候选响应
		if len(data.Response.Candidates) == 0 {
			continue
		}

		candidate := data.Response.Candidates[0]

		// 处理 parts
		for _, part := range candidate.Content.Parts {
			if part.Thought {
				// 思维链内容
				callback(StreamChunk{Type: "thinking", Content: part.Text})
			} else if part.Text != "" {
				// 普通文本
				callback(StreamChunk{Type: "text", Content: part.Text})
			} else if part.FunctionCall != nil {
				// 工具调用（累积）
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				id := part.FunctionCall.ID
				if id == "" {
					id = utils.GenerateToolCallID()
				}
				toolCalls = append(toolCalls, converter.OpenAIToolCall{
					ID:   id,
					Type: "function",
					Function: converter.OpenAIFunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}

		// 响应结束时发送工具调用
		if candidate.FinishReason != "" && len(toolCalls) > 0 {
			callback(StreamChunk{Type: "tool_calls", ToolCalls: toolCalls})
			toolCalls = nil
		}
	}

	// 如果有未发送的工具调用
	if len(toolCalls) > 0 {
		callback(StreamChunk{Type: "tool_calls", ToolCalls: toolCalls})
	}

	return usage, scanner.Err()
}

// SetStreamHeaders 设置流式响应头
func SetStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

// WriteStreamData 写入流式数据
func WriteStreamData(w http.ResponseWriter, data interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", jsonBytes)
	if err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// WriteStreamDone 写入流结束标记
func WriteStreamDone(w http.ResponseWriter) {
	w.Write([]byte("data: [DONE]\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// WriteStreamError 写入流错误
func WriteStreamError(w http.ResponseWriter, errMsg string) {
	errResp := map[string]interface{}{
		"error": map[string]interface{}{
			"message": errMsg,
			"type":    "server_error",
		},
	}
	WriteStreamData(w, errResp)
	WriteStreamDone(w)
}

// StreamWriter 流式写入器
type StreamWriter struct {
	w        http.ResponseWriter
	id       string
	created  int64
	model    string
	sentRole bool
}

// NewStreamWriter 创建流式写入器
func NewStreamWriter(w http.ResponseWriter, id string, created int64, model string) *StreamWriter {
	SetStreamHeaders(w)
	return &StreamWriter{
		w:       w,
		id:      id,
		created: created,
		model:   model,
	}
}

// WriteRole 写入角色（首次）
func (sw *StreamWriter) WriteRole() error {
	if sw.sentRole {
		return nil
	}
	sw.sentRole = true

	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{Role: "assistant"},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}

// WriteContent 写入内容
func (sw *StreamWriter) WriteContent(content string) error {
	sw.WriteRole()
	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{Content: content},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}

// WriteReasoning 写入思考内容
func (sw *StreamWriter) WriteReasoning(reasoning string) error {
	sw.WriteRole()
	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{Reasoning: reasoning},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}

// WriteToolCalls 写入工具调用
func (sw *StreamWriter) WriteToolCalls(toolCalls []converter.OpenAIToolCall) error {
	sw.WriteRole()
	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{ToolCalls: toolCalls},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}

// WriteFinish 写入结束
func (sw *StreamWriter) WriteFinish(reason string, usage *converter.Usage) error {
	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{},
		&reason, usage,
	)
	if err := WriteStreamData(sw.w, chunk); err != nil {
		return err
	}
	WriteStreamDone(sw.w)
	return nil
}

// WriteHeartbeat 写入心跳
func (sw *StreamWriter) WriteHeartbeat() error {
	sw.WriteRole()
	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{Content: ""},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}
