package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"anti2api-golang/internal/api"
	"anti2api-golang/internal/converter"
	"anti2api-golang/internal/logger"
	"anti2api-golang/internal/store"
	"anti2api-golang/internal/utils"
)

// recordLog 记录 API 调用日志
func recordLog(method, path string, req *converter.OpenAIChatRequest, token *store.Account, status int, success bool, duration time.Duration, errMsg string, responseContent string) {
	entry := store.LogEntry{
		ID:         utils.GenerateRequestID(),
		Timestamp:  time.Now(),
		Status:     status,
		Success:    success,
		Model:      req.Model,
		Method:     method,
		Path:       path,
		DurationMs: duration.Milliseconds(),
		Message:    errMsg,
		HasDetail:  true,
		Detail: &store.LogDetail{
			Request: &store.RequestSnapshot{
				Body: req,
			},
			Response: &store.ResponseSnapshot{
				StatusCode:  status,
				ModelOutput: responseContent,
			},
		},
	}

	if token != nil {
		entry.ProjectID = token.ProjectID
		entry.Email = token.Email
	}

	store.GetLogStore().Add(entry)
}

// HandleGetModels 获取模型列表
func HandleGetModels(w http.ResponseWriter, r *http.Request) {
	models := converter.ModelsResponse{
		Object: "list",
		Data:   converter.SupportedModels,
	}
	WriteJSON(w, http.StatusOK, models)
}

// HandleChatCompletions 处理聊天完成请求
func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req converter.OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	// 记录客户端请求
	logger.ClientRequest(r.Method, r.URL.Path, req)

	// 获取 token
	token, err := store.GetAccountStore().GetToken()
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	// 处理请求
	if req.Stream {
		handleStreamRequest(w, r, &req, token)
	} else {
		handleNonStreamRequest(w, r, &req, token)
	}
}

// HandleChatCompletionsWithCredential 使用指定凭证处理聊天完成请求
func HandleChatCompletionsWithCredential(w http.ResponseWriter, r *http.Request) {
	credential := r.PathValue("credential")

	var req converter.OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	logger.ClientRequest(r.Method, r.URL.Path, req)

	// 按凭证获取 token
	var token *store.Account
	var err error

	accountStore := store.GetAccountStore()
	if strings.Contains(credential, "@") {
		token, err = accountStore.GetTokenByEmail(credential)
	} else {
		token, err = accountStore.GetTokenByProjectID(credential)
	}

	if err != nil {
		WriteError(w, http.StatusNotFound, "Credential not found: "+credential)
		return
	}

	// 处理请求
	if req.Stream {
		handleStreamRequest(w, r, &req, token)
	} else {
		handleNonStreamRequest(w, r, &req, token)
	}
}

func handleNonStreamRequest(w http.ResponseWriter, r *http.Request, req *converter.OpenAIChatRequest, token *store.Account) {
	startTime := time.Now()

	// 转换请求
	antigravityReq := converter.ConvertOpenAIToAntigravity(req, token)

	// 图片模型特殊处理
	if converter.IsImageModel(req.Model) {
		antigravityReq.RequestType = "image_gen"
		if antigravityReq.Request.SystemInstruction != nil && len(antigravityReq.Request.SystemInstruction.Parts) > 0 {
			antigravityReq.Request.SystemInstruction.Parts[0].Text += "（当前作为图像生成模型使用，请根据描述生成图片）"
		}
		antigravityReq.Request.Tools = nil
		antigravityReq.Request.ToolConfig = nil
	}

	// 发送请求
	ctx := r.Context()
	resp, err := api.GenerateContent(ctx, antigravityReq, token)
	if err != nil {
		duration := time.Since(startTime)
		logger.ClientResponse(getErrorStatus(err), duration, err.Error())
		// 记录失败日志
		recordLog(r.Method, r.URL.Path, req, token, getErrorStatus(err), false, duration, err.Error(), "")
		WriteError(w, getErrorStatus(err), err.Error())
		return
	}

	// 转换响应
	openAIResp := converter.ConvertToOpenAIResponse(resp, req.Model)

	duration := time.Since(startTime)
	logger.ClientResponse(http.StatusOK, duration, openAIResp)

	// 记录成功日志
	responseContent := ""
	if len(openAIResp.Choices) > 0 {
		responseContent = openAIResp.Choices[0].Message.Content
	}
	recordLog(r.Method, r.URL.Path, req, token, http.StatusOK, true, duration, "", responseContent)

	WriteJSON(w, http.StatusOK, openAIResp)
}

func handleStreamRequest(w http.ResponseWriter, r *http.Request, req *converter.OpenAIChatRequest, token *store.Account) {
	startTime := time.Now()

	// 检查是否为 bypass 模式
	if converter.IsBypassModel(req.Model) {
		handleBypassStream(w, r, req, token)
		return
	}

	// 转换请求
	antigravityReq := converter.ConvertOpenAIToAntigravity(req, token)

	// 发送流式请求
	ctx := r.Context()
	resp, err := api.GenerateContentStream(ctx, antigravityReq, token)
	if err != nil {
		duration := time.Since(startTime)
		api.SetStreamHeaders(w)
		api.WriteStreamError(w, err.Error())
		// 记录失败日志
		recordLog(r.Method, r.URL.Path, req, token, getErrorStatus(err), false, duration, err.Error(), "")
		return
	}

	// 设置流式响应头
	api.SetStreamHeaders(w)

	id := utils.GenerateChatCompletionID()
	created := time.Now().Unix()
	model := req.Model

	streamWriter := api.NewStreamWriter(w, id, created, model)

	var usage *converter.UsageMetadata
	var toolCalls []converter.OpenAIToolCall
	var contentBuilder strings.Builder

	// 处理流式响应
	usage, err = api.ProcessStreamResponse(resp, func(chunk api.StreamChunk) {
		switch chunk.Type {
		case "thinking":
			streamWriter.WriteReasoning(chunk.Content)
		case "text":
			streamWriter.WriteContent(chunk.Content)
			contentBuilder.WriteString(chunk.Content)
		case "tool_calls":
			toolCalls = chunk.ToolCalls
			streamWriter.WriteToolCalls(chunk.ToolCalls)
		case "done":
			// 处理完成
		}
	})

	duration := time.Since(startTime)

	if err != nil {
		logger.Error("Stream processing error: %v", err)
		// 记录失败日志
		recordLog(r.Method, r.URL.Path, req, token, http.StatusInternalServerError, false, duration, err.Error(), contentBuilder.String())
	} else {
		// 记录成功日志
		recordLog(r.Method, r.URL.Path, req, token, http.StatusOK, true, duration, "", contentBuilder.String())
	}

	// 发送结束
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	var usageData *converter.Usage
	if usage != nil {
		usageData = converter.ConvertUsage(usage)
	}

	streamWriter.WriteFinish(finishReason, usageData)
}

func handleBypassStream(w http.ResponseWriter, r *http.Request, req *converter.OpenAIChatRequest, token *store.Account) {
	startTime := time.Now()
	id := utils.GenerateChatCompletionID()
	created := time.Now().Unix()
	model := req.Model

	// NewStreamWriter 内部会设置响应头
	streamWriter := api.NewStreamWriter(w, id, created, model)

	// 立即发送第一个心跳，确保客户端计时器启动
	if err := streamWriter.WriteHeartbeat(); err != nil {
		return
	}

	// 启动心跳 goroutine
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := streamWriter.WriteHeartbeat(); err != nil {
					return
				}
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// 转换请求（使用真实模型名）
	actualModel := converter.ResolveModelName(req.Model)
	modifiedReq := *req
	modifiedReq.Model = actualModel

	antigravityReq := converter.ConvertOpenAIToAntigravity(&modifiedReq, token)

	// 执行非流式请求
	resp, err := api.GenerateContent(ctx, antigravityReq, token)
	close(done)

	if err != nil {
		duration := time.Since(startTime)
		streamWriter.WriteContent("Error: " + err.Error())
		streamWriter.WriteFinish("stop", nil)
		// 记录失败日志
		recordLog(r.Method, r.URL.Path, req, token, getErrorStatus(err), false, duration, err.Error(), "")
		return
	}

	// 转换响应
	openAIResp := converter.ConvertToOpenAIResponse(resp, model)

	duration := time.Since(startTime)

	// 发送完整内容
	if len(openAIResp.Choices) > 0 {
		msg := openAIResp.Choices[0].Message

		if msg.Reasoning != "" {
			streamWriter.WriteReasoning(msg.Reasoning)
		}
		if len(msg.ToolCalls) > 0 {
			streamWriter.WriteToolCalls(msg.ToolCalls)
		}
		if msg.Content != "" {
			streamWriter.WriteContent(msg.Content)
		}

		finishReason := "stop"
		if openAIResp.Choices[0].FinishReason != nil {
			finishReason = *openAIResp.Choices[0].FinishReason
		}

		streamWriter.WriteFinish(finishReason, openAIResp.Usage)

		// 记录成功日志
		recordLog(r.Method, r.URL.Path, req, token, http.StatusOK, true, duration, "", msg.Content)
	} else {
		streamWriter.WriteFinish("stop", nil)
		// 记录成功但无内容的日志
		recordLog(r.Method, r.URL.Path, req, token, http.StatusOK, true, duration, "", "")
	}
}

func getErrorStatus(err error) int {
	if apiErr, ok := err.(*api.APIError); ok {
		return apiErr.Status
	}
	return http.StatusInternalServerError
}
