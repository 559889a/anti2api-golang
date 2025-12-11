package converter

// ==================== Antigravity 内部格式 ====================

// AntigravityRequest Antigravity 内部请求格式
type AntigravityRequest struct {
	Project     string              `json:"project"`
	RequestID   string              `json:"requestId"`
	Request     AntigravityInnerReq `json:"request"`
	Model       string              `json:"model"`
	UserAgent   string              `json:"userAgent"`
	RequestType string              `json:"requestType,omitempty"` // image_gen 等
}

// AntigravityInnerReq 内部请求体
type AntigravityInnerReq struct {
	SystemInstruction *SystemInstruction `json:"systemInstruction,omitempty"`
	Contents          []Content          `json:"contents"`
	Tools             []Tool             `json:"tools,omitempty"`
	ToolConfig        *ToolConfig        `json:"toolConfig,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
	SessionID         string             `json:"sessionId"`
}

// Content 消息内容
type Content struct {
	Role  string `json:"role"` // "user" 或 "model"
	Parts []Part `json:"parts"`
}

// Part 消息部分
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	InlineData       *InlineData       `json:"inlineData,omitempty"`
	Thought          bool              `json:"thought,omitempty"` // 思维链标记
}

// FunctionCall 函数调用
type FunctionCall struct {
	ID   string                 `json:"id,omitempty"`
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// FunctionResponse 函数响应
type FunctionResponse struct {
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// InlineData 内联数据（图片等）
type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// SystemInstruction 系统指令
type SystemInstruction struct {
	Parts []Part `json:"parts"`
}

// Tool 工具定义
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// FunctionDeclaration 函数声明
type FunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ToolConfig 工具配置
type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// FunctionCallingConfig 函数调用配置
type FunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"` // AUTO, ANY, NONE
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

// GenerationConfig 生成配置
type GenerationConfig struct {
	CandidateCount  int             `json:"candidateCount,omitempty"`
	StopSequences   []string        `json:"stopSequences,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"topP,omitempty"`
	TopK            int             `json:"topK,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

// ThinkingConfig 思考配置
type ThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts"`
	ThinkingBudget  int  `json:"thinkingBudget,omitempty"`
}

// ==================== Antigravity 响应格式 ====================

// AntigravityResponse Antigravity 响应
type AntigravityResponse struct {
	Response struct {
		Candidates    []Candidate    `json:"candidates"`
		UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	} `json:"response"`
}

// Candidate 候选响应
type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
	Index        int     `json:"index"`
}

// UsageMetadata 使用统计
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount,omitempty"`
}

// ==================== OpenAI 格式 ====================

// OpenAIChatRequest OpenAI 聊天请求
type OpenAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
	Tools       []OpenAITool    `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
}

// OpenAIMessage OpenAI 消息格式
type OpenAIMessage struct {
	Role       string           `json:"role"`    // system/user/assistant/tool
	Content    interface{}      `json:"content"` // string 或 []OpenAIContentPart
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

// OpenAIContentPart OpenAI 内容部分
type OpenAIContentPart struct {
	Type     string    `json:"type"` // text/image_url
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 图片 URL
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// OpenAITool OpenAI 工具定义
type OpenAITool struct {
	Type     string         `json:"type"` // function
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction OpenAI 函数定义
type OpenAIFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// OpenAIToolCall OpenAI 工具调用
type OpenAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // function
	Function OpenAIFunctionCall `json:"function"`
}

// OpenAIFunctionCall OpenAI 函数调用
type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// OpenAIChatCompletion OpenAI 聊天完成响应
type OpenAIChatCompletion struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice 选择
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message,omitempty"`
	Delta        *Delta  `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason"`
}

// Message 消息
type Message struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`
	Reasoning string           `json:"reasoning,omitempty"` // 思考内容
}

// Delta 流式增量
type Delta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`
	Reasoning string           `json:"reasoning,omitempty"` // 思考内容
}

// Usage 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIStreamChunk 流式 Chunk
type OpenAIStreamChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// ModelsResponse 模型列表响应
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// ==================== Gemini 格式 ====================

// GeminiRequest 标准 Gemini 请求
type GeminiRequest struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *SystemInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
	Tools             []Tool             `json:"tools,omitempty"`
	ToolConfig        *ToolConfig        `json:"toolConfig,omitempty"`
}

// GeminiResponse 标准 Gemini 响应
type GeminiResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// ==================== 错误格式 ====================

// APIError API 错误
type APIError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}
