package types

// ToolCall 表示对某个工具的调用请求。
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolResult 是工具执行的返回结果。
type ToolResult struct {
	CallID string
	OK     bool
	Output string
	Error  error
}

// ToolDefinition 描述一个工具的元信息，用于 LLM 感知可用工具。
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema 形式的入参定义
}
