package api

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

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

	// 使用较小的缓冲区以减少延迟（4KB）
	bufReader := bufio.NewReaderSize(reader, 4*1024)

	var usage *converter.UsageMetadata
	var toolCalls []converter.OpenAIToolCall

	for {
		// ReadString 会在读到分隔符时立即返回，不会等待缓冲区填满
		line, err := bufReader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return usage, err
		}

		// 去掉末尾的换行符
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

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

	return usage, nil
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

// StreamWriter 流式写入器（带 UTF-8 缓冲，线程安全）
type StreamWriter struct {
	w               http.ResponseWriter
	id              string
	created         int64
	model           string
	sentRole        bool
	contentBuffer   []byte     // 缓冲不完整的 UTF-8 内容字节
	reasoningBuffer []byte     // 缓冲不完整的 UTF-8 思考字节
	mu              sync.Mutex // 保护并发写入
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

// writeRoleLocked 写入角色（内部使用，调用者必须持有锁）
func (sw *StreamWriter) writeRoleLocked() error {
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

// WriteRole 写入角色（首次，线程安全）
func (sw *StreamWriter) WriteRole() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.writeRoleLocked()
}

// extractValidUTF8 从字节切片中提取有效的 UTF-8 字符串，返回有效部分和剩余的不完整字节
func extractValidUTF8(data []byte) (valid string, remaining []byte) {
	if len(data) == 0 {
		return "", nil
	}

	// 检查整个字符串是否是有效的 UTF-8
	if utf8.Valid(data) {
		return string(data), nil
	}

	// 从末尾向前查找不完整的 UTF-8 字符
	// UTF-8 编码规则：
	// - 单字节: 0xxxxxxx (0x00-0x7F)
	// - 多字节起始: 11xxxxxx (0xC0-0xFF)
	// - 多字节后续: 10xxxxxx (0x80-0xBF)

	// 检查末尾最多 4 个字节（UTF-8 最多 4 字节）
	checkLen := 4
	if len(data) < checkLen {
		checkLen = len(data)
	}

	for i := 1; i <= checkLen; i++ {
		idx := len(data) - i
		b := data[idx]

		// 如果是多字节起始字节
		if b >= 0xC0 {
			// 计算这个字符应该有多少字节
			var expectedLen int
			if b >= 0xF0 {
				expectedLen = 4
			} else if b >= 0xE0 {
				expectedLen = 3
			} else {
				expectedLen = 2
			}

			// 检查是否有足够的后续字节
			actualLen := len(data) - idx
			if actualLen < expectedLen {
				// 不完整的字符，需要缓冲
				return string(data[:idx]), data[idx:]
			}
			break
		}
		// 如果是后续字节 (10xxxxxx)，继续向前查找起始字节
		if b >= 0x80 && b < 0xC0 {
			continue
		}
		// 如果是 ASCII 字节，前面的字符应该是完整的
		break
	}

	// 再次验证，移除末尾无效字节
	for len(data) > 0 {
		if utf8.Valid(data) {
			return string(data), nil
		}
		remaining = append([]byte{data[len(data)-1]}, remaining...)
		data = data[:len(data)-1]
	}

	return "", remaining
}

// WriteContent 写入内容（带 UTF-8 缓冲，线程安全）
func (sw *StreamWriter) WriteContent(content string) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.writeRoleLocked()

	// 合并缓冲区和新内容
	data := append(sw.contentBuffer, []byte(content)...)
	sw.contentBuffer = nil

	// 提取有效的 UTF-8 字符串
	validContent, remaining := extractValidUTF8(data)
	sw.contentBuffer = remaining

	// 如果没有有效内容，跳过本次写入
	if validContent == "" {
		return nil
	}

	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{Content: validContent},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}

// WriteReasoning 写入思考内容（带 UTF-8 缓冲，线程安全）
func (sw *StreamWriter) WriteReasoning(reasoning string) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.writeRoleLocked()

	// 合并缓冲区和新内容
	data := append(sw.reasoningBuffer, []byte(reasoning)...)
	sw.reasoningBuffer = nil

	// 提取有效的 UTF-8 字符串
	validReasoning, remaining := extractValidUTF8(data)
	sw.reasoningBuffer = remaining

	// 如果没有有效内容，跳过本次写入
	if validReasoning == "" {
		return nil
	}

	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{Reasoning: validReasoning},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}

// WriteToolCalls 写入工具调用（线程安全）
func (sw *StreamWriter) WriteToolCalls(toolCalls []converter.OpenAIToolCall) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.writeRoleLocked()
	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{ToolCalls: toolCalls},
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}

// flushLocked 刷新缓冲区中剩余的内容（内部使用，调用者必须持有锁）
func (sw *StreamWriter) flushLocked() error {
	// 刷新内容缓冲区
	if len(sw.contentBuffer) > 0 {
		content := string(sw.contentBuffer)
		sw.contentBuffer = nil
		if content != "" {
			chunk := converter.CreateStreamChunk(
				sw.id, sw.created, sw.model,
				&converter.Delta{Content: content},
				nil, nil,
			)
			if err := WriteStreamData(sw.w, chunk); err != nil {
				return err
			}
		}
	}

	// 刷新思考缓冲区
	if len(sw.reasoningBuffer) > 0 {
		reasoning := string(sw.reasoningBuffer)
		sw.reasoningBuffer = nil
		if reasoning != "" {
			chunk := converter.CreateStreamChunk(
				sw.id, sw.created, sw.model,
				&converter.Delta{Reasoning: reasoning},
				nil, nil,
			)
			if err := WriteStreamData(sw.w, chunk); err != nil {
				return err
			}
		}
	}

	return nil
}

// Flush 刷新缓冲区中剩余的内容（线程安全）
func (sw *StreamWriter) Flush() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.flushLocked()
}

// WriteFinish 写入结束（线程安全）
func (sw *StreamWriter) WriteFinish(reason string, usage *converter.Usage) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	// 先刷新缓冲区
	sw.flushLocked()

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

// WriteHeartbeat 写入心跳（发送空 delta 的有效数据包，线程安全）
func (sw *StreamWriter) WriteHeartbeat() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	// 先确保 role 已发送
	sw.writeRoleLocked()

	// 发送空 delta 的数据包（与 hajimi 格式一致）
	// 输出格式：{"id":"...","object":"chat.completion.chunk","created":...,"model":"...","choices":[{"index":0,"delta":{},"finish_reason":null}]}
	chunk := converter.CreateStreamChunk(
		sw.id, sw.created, sw.model,
		&converter.Delta{}, // 空 delta
		nil, nil,
	)
	return WriteStreamData(sw.w, chunk)
}
