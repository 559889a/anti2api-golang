package converter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"anti2api-golang/internal/config"
	"anti2api-golang/internal/store"
	"anti2api-golang/internal/utils"
)

// ConvertOpenAIToAntigravity 将 OpenAI 请求转换为 Antigravity 格式
func ConvertOpenAIToAntigravity(req *OpenAIChatRequest, account *store.Account) *AntigravityRequest {
	modelName := ResolveModelName(req.Model)

	antigravityReq := &AntigravityRequest{
		Project:   getProjectID(account),
		RequestID: utils.GenerateRequestID(),
		Model:     modelName,
		UserAgent: config.Get().UserAgent,
	}

	// 检查是否有历史函数调用（需要禁用 thinking 模式以避免 thought_signature 问题）
	hasHistoryFunctionCalls := hasToolCallsInHistory(req.Messages)

	// 转换消息
	contents := convertMessages(req.Messages)

	// 构建内部请求
	innerReq := AntigravityInnerReq{
		Contents:  contents,
		SessionID: account.SessionID,
	}

	// 提取系统消息
	systemText := extractSystemInstruction(req.Messages)
	if systemText != "" {
		innerReq.SystemInstruction = &SystemInstruction{
			Parts: []Part{{Text: systemText}},
		}
	}

	// 转换工具
	if len(req.Tools) > 0 {
		innerReq.Tools = convertTools(req.Tools)
		innerReq.ToolConfig = &ToolConfig{
			FunctionCallingConfig: &FunctionCallingConfig{
				Mode: "AUTO",
			},
		}
	}

	// 构建生成配置（如果有历史函数调用，禁用 thinking 模式）
	innerReq.GenerationConfig = buildGenerationConfig(req, modelName, hasHistoryFunctionCalls)

	antigravityReq.Request = innerReq
	return antigravityReq
}

// hasToolCallsInHistory 检查历史消息中是否有函数调用
func hasToolCallsInHistory(messages []OpenAIMessage) bool {
	for _, msg := range messages {
		if len(msg.ToolCalls) > 0 || msg.Role == "tool" {
			return true
		}
	}
	return false
}

func getProjectID(account *store.Account) string {
	if account.ProjectID != "" {
		return account.ProjectID
	}
	return utils.GenerateProjectID()
}

func convertMessages(messages []OpenAIMessage) []Content {
	var result []Content

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// 跳过，单独处理到 systemInstruction
			continue

		case "user":
			parts := extractParts(msg.Content)
			result = append(result, Content{Role: "user", Parts: parts})

		case "assistant":
			parts := []Part{}
			if text := getTextContent(msg.Content); text != "" {
				parts = append(parts, Part{Text: text})
			}
			// 转换工具调用
			for _, tc := range msg.ToolCalls {
				args := parseArgs(tc.Function.Arguments)
				parts = append(parts, Part{
					FunctionCall: &FunctionCall{
						ID:   tc.ID,
						Name: tc.Function.Name,
						Args: args,
					},
					ThoughtSignature: tc.ThoughtSignature, // 回传签名（API必需）
				})
			}
			if len(parts) > 0 {
				result = append(result, Content{Role: "model", Parts: parts})
			}

		case "tool":
			// 查找对应的 function name
			funcName := findFunctionName(result, msg.ToolCallID)
			part := Part{
				FunctionResponse: &FunctionResponse{
					ID:   msg.ToolCallID,
					Name: funcName,
					Response: map[string]interface{}{
						"output": getTextContent(msg.Content),
					},
				},
			}
			// 合并到上一个 user 消息或新建
			appendFunctionResponse(&result, part)
		}
	}

	return result
}

func extractSystemInstruction(messages []OpenAIMessage) string {
	var texts []string
	for _, msg := range messages {
		if msg.Role == "system" {
			texts = append(texts, getTextContent(msg.Content))
		}
	}
	return strings.Join(texts, "\n\n")
}

func extractParts(content interface{}) []Part {
	var parts []Part

	switch v := content.(type) {
	case string:
		parts = append(parts, Part{Text: v})
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				switch m["type"] {
				case "text":
					if text, ok := m["text"].(string); ok {
						parts = append(parts, Part{Text: text})
					}
				case "image_url":
					if imgURL, ok := m["image_url"].(map[string]interface{}); ok {
						if url, ok := imgURL["url"].(string); ok {
							if inlineData := parseImageURL(url); inlineData != nil {
								parts = append(parts, Part{InlineData: inlineData})
							}
						}
					}
				}
			}
		}
	}

	return parts
}

func parseImageURL(url string) *InlineData {
	// 解析 data:image/{format};base64,{data}
	re := regexp.MustCompile(`^data:image/(\w+);base64,(.+)$`)
	if matches := re.FindStringSubmatch(url); len(matches) == 3 {
		return &InlineData{
			MimeType: "image/" + matches[1],
			Data:     matches[2],
		}
	}
	return nil
}

func getTextContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

func parseArgs(argsStr string) map[string]interface{} {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return map[string]interface{}{}
	}
	return args
}

func findFunctionName(contents []Content, toolCallID string) string {
	for i := len(contents) - 1; i >= 0; i-- {
		for _, part := range contents[i].Parts {
			if part.FunctionCall != nil && part.FunctionCall.ID == toolCallID {
				return part.FunctionCall.Name
			}
		}
	}
	return ""
}

func appendFunctionResponse(contents *[]Content, part Part) {
	// functionResponse 应该在 functionCall 之后的新 user turn 中
	// 检查最后一个消息是否是 model 消息（包含 functionCall）
	if len(*contents) > 0 && (*contents)[len(*contents)-1].Role == "model" {
		// 在 model 消息后添加新的 user 消息来包含 functionResponse
		*contents = append(*contents, Content{
			Role:  "user",
			Parts: []Part{part},
		})
		return
	}
	// 如果最后已经是 user 消息，合并 functionResponse（多个 tool 响应的情况）
	if len(*contents) > 0 && (*contents)[len(*contents)-1].Role == "user" {
		(*contents)[len(*contents)-1].Parts = append((*contents)[len(*contents)-1].Parts, part)
		return
	}
	// 新建 user 消息
	*contents = append(*contents, Content{
		Role:  "user",
		Parts: []Part{part},
	})
}

func convertTools(tools []OpenAITool) []Tool {
	var result []Tool

	for _, tool := range tools {
		params := tool.Function.Parameters
		// 移除 $schema 字段
		delete(params, "$schema")

		result = append(result, Tool{
			FunctionDeclarations: []FunctionDeclaration{{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  params,
			}},
		})
	}

	return result
}

func buildGenerationConfig(req *OpenAIChatRequest, modelName string, hasHistoryFunctionCalls bool) *GenerationConfig {
	config := &GenerationConfig{
		CandidateCount: 1,
		StopSequences:  DefaultStopSequences,
	}

	// 添加自定义停止序列
	if len(req.Stop) > 0 {
		config.StopSequences = append(config.StopSequences, req.Stop...)
	}

	// Claude 模型特殊处理
	if IsClaudeModel(modelName) {
		config.MaxOutputTokens = GetClaudeMaxOutputTokens(modelName)
		// Claude thinking 模式不支持 topP
		// 如果有历史函数调用，禁用 thinking 模式以避免 thought_signature 问题
		if !hasHistoryFunctionCalls && ShouldEnableThinking(modelName, nil) {
			config.ThinkingConfig = BuildThinkingConfig(modelName)
		}
		return config
	}

	// 其他模型
	if req.Temperature != nil {
		config.Temperature = req.Temperature
	}
	if req.TopP != nil {
		config.TopP = req.TopP
	}
	if req.MaxTokens > 0 {
		config.MaxOutputTokens = req.MaxTokens
	}

	// 思考模式（如果有历史函数调用，禁用以避免 thought_signature 问题）
	if !hasHistoryFunctionCalls && ShouldEnableThinking(modelName, nil) {
		config.ThinkingConfig = BuildThinkingConfig(modelName)
	}

	return config
}

// ConvertToOpenAIResponse 将 Antigravity 响应转换为 OpenAI 格式
func ConvertToOpenAIResponse(antigravityResp *AntigravityResponse, model string) *OpenAIChatCompletion {
	parts := antigravityResp.Response.Candidates[0].Content.Parts

	var content, thinkingContent string
	var toolCalls []OpenAIToolCall
	var imageURLs []string

	for _, part := range parts {
		if part.Thought {
			thinkingContent += part.Text
		} else if part.Text != "" {
			content += part.Text
		} else if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			id := part.FunctionCall.ID
			if id == "" {
				id = utils.GenerateToolCallID()
			}
			toolCalls = append(toolCalls, OpenAIToolCall{
				ID:   id,
				Type: "function",
				Function: OpenAIFunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
				ThoughtSignature: part.ThoughtSignature, // 保存签名用于后续请求
			})
		} else if part.InlineData != nil {
			dataURL := fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
			imageURLs = append(imageURLs, dataURL)
		}
	}

	// 处理图片输出
	if len(imageURLs) > 0 {
		var md strings.Builder
		if content != "" {
			md.WriteString(content + "\n\n")
		}
		for _, url := range imageURLs {
			md.WriteString(fmt.Sprintf("![image](%s)\n\n", url))
		}
		content = md.String()
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return &OpenAIChatCompletion{
		ID:      utils.GenerateChatCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
				Reasoning: thinkingContent,
			},
			FinishReason: &finishReason,
		}},
		Usage: ConvertUsage(antigravityResp.Response.UsageMetadata),
	}
}

// ConvertUsage 转换使用统计
func ConvertUsage(metadata *UsageMetadata) *Usage {
	if metadata == nil {
		return nil
	}
	return &Usage{
		PromptTokens:     metadata.PromptTokenCount,
		CompletionTokens: metadata.CandidatesTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
	}
}

// CreateStreamChunk 创建流式 Chunk
func CreateStreamChunk(id string, created int64, model string, delta *Delta, finishReason *string, usage *Usage) *OpenAIStreamChunk {
	return &OpenAIStreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []Choice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		}},
		Usage: usage,
	}
}
