package types

// ActionType 表示动作的类型。
type ActionType string

const (
	ActionThink    ActionType = "think"     // 内部推理
	ActionToolCall ActionType = "tool_call" // 调用工具
	ActionRespond  ActionType = "respond"   // 生成回复
	ActionUnknown  ActionType = "unknown"
)

// RiskLevel 表示动作的风险等级，决定用户确认策略。
type RiskLevel int

const (
	RiskNone   RiskLevel = iota // 无风险，无需确认
	RiskLow                     // 低风险，直接放行
	RiskMedium                  // 中风险，需要确认
	RiskHigh                    // 高风险，拒绝或强制确认
)

// Action 是规划层输出的单个可执行动作。
type Action struct {
	ID        string         // 动作 ID
	Type      ActionType     // 动作类型
	ToolName  string         // 目标工具名（ToolCall 类型时有效）
	ToolInput map[string]any // 工具入参
	Reason    string         // 执行理由（用于向用户展示）
	CallDesc  string         // 人类可读的调用说明
	Risk      RiskLevel      // 风险等级
}
