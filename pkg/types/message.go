package types

// Role 表示对话消息的角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 是对话历史中的一条消息。
type Message struct {
	Role    Role
	Content string
	// ToolCallID 是工具消息（RoleTool）关联的调用 ID，与 ToolCall.ID 对应。
	// 用于让 LLM 知道这条 tool 消息是对哪个工具调用的返回。
	ToolCallID string
	// ToolCalls 是助手消息（RoleAssistant）发出的工具调用声明。
	// 工具执行完成后，规划层会将带 ToolCalls 的 assistant 消息写回历史。
	ToolCalls []*ToolCall
}
