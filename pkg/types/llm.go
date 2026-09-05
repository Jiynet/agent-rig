package types

// GenerateRequest 是发给 LLM 的请求。
type GenerateRequest struct {
	Model       string            // 模型名
	System      string            // 系统提示
	Messages    []*Message        // 对话历史
	Temperature float64           // 采样温度
	MaxTokens   int               // 最大生成 token 数
	Tools       []*ToolDefinition // 可选：供模型选择调用的工具
}

// GenerateResponse 是 LLM 的完整响应。
type GenerateResponse struct {
	Content   string
	ToolCalls []*ToolCall
	Usage     Usage
}

// Usage 记录一次调用的 token 用量。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// StreamChunk 是流式响应的一个分片。
type StreamChunk struct {
	Delta    string
	Done     bool
	ToolCall *ToolCall
}
