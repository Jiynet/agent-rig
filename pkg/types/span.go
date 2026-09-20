package types

import "time"

// Span 表示一次 Agent 执行链路中的一个追踪跨度。
type Span struct {
	ID         string         // 跨度唯一 ID
	ParentID   string         // 父跨度 ID（顶层为空）
	Name       string         // 跨度名称，如 "plan.create" / "llm.generate" / "tool.execute"
	StartTime  time.Time      // 开始时间
	EndTime    time.Time      // 结束时间
	Duration   time.Duration  // 耗时（EndTime - StartTime）
	Attributes map[string]any // 附加属性（模型名、token 数、工具名等）
	Status     SpanStatus     // 执行状态
	Error      string         // 错误信息（失败时）
	Children   []*Span        // 子跨度
}

// SpanStatus 表示跨度的执行状态。
type SpanStatus string

const (
	SpanOK    SpanStatus = "ok"
	SpanError SpanStatus = "error"
	SpanSkip  SpanStatus = "skipped"
)

// AgentMetrics 记录一次 Agent 执行的累计指标。
type AgentMetrics struct {
	TotalDuration    time.Duration // 总耗时
	LLMCalls         int           // LLM 调用次数
	LLMRetries       int           // LLM 重试次数
	PromptTokens     int           // 累计 prompt token
	CompletionTokens int           // 累计 completion token
	TotalTokens      int           // 累计总 token
	ToolCalls        int           // 工具调用次数
	ToolErrors       int           // 工具调用失败次数
	LoopIterations   int           // ReAct 循环轮次
	UserRejects      int           // 用户拒绝次数

	// 上下文组装指标（估算值）。
	ContextTokens         int // 实际发送给 LLM 的上下文 token
	ContextOriginalTokens int // 未压缩时的历史 token
	ContextSummarized     int // 被压缩进摘要的消息数
	ContextTruncatedTools int // 被截断的工具观察数
}
