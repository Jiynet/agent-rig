package execution

import "ai-cli-agent/pkg/types"

// DefaultRiskFor 根据动作类型返回默认风险等级。
func DefaultRiskFor(action *types.Action) types.RiskLevel {
	switch action.Type {
	case types.ActionToolCall:
		return RiskForTool(action.ToolName)
	case types.ActionRespond:
		return types.RiskNone
	default:
		return types.RiskLow
	}
}

// RiskForTool 根据工具名返回风险等级。
func RiskForTool(name string) types.RiskLevel {
	switch name {
	case "shell":
		return types.RiskMedium // shell 命令可能有副作用
	case "file_write":
		return types.RiskMedium // 文件写入需要确认
	case "http_request":
		return types.RiskLow // HTTP GET 是只读操作
	case "file_read", "web_search":
		return types.RiskLow // 读取操作，低风险
	default:
		return types.RiskLow
	}
}
