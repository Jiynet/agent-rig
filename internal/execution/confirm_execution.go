package execution

import (
	"context"
	"fmt"

	"ai-cli-agent/internal/perception"
	"ai-cli-agent/pkg/types"
)

// ConfirmExecution 是默认执行实现：
// 生成调用说明 → 根据风险等级决定是否请求用户确认。
type ConfirmExecution struct {
	perception perception.Perception
}

// NewConfirmExecution 构造确认式执行器。
func NewConfirmExecution(p perception.Perception) *ConfirmExecution {
	return &ConfirmExecution{perception: p}
}

// RequestExecution 根据动作风险等级返回判断结果。
// 低风险动作直接放行；中高风险交由感知层请求用户确认。
func (e *ConfirmExecution) RequestExecution(ctx context.Context, action *types.Action) (*types.JudgmentResult, error) {
	callDesc := BuildCallDescription(action)

	// 兜底：若动作未标注风险，按默认规则推导。
	if action.Risk <= types.RiskNone {
		action.Risk = DefaultRiskFor(action)
	}

	if action.Risk <= types.RiskLow {
		return &types.JudgmentResult{Decision: types.DecisionApprove}, nil
	}
	return e.perception.PromptForJudgment(ctx, callDesc, action)
}

// BuildCallDescription 生成人类可读的调用说明，供用户确认时展示。
func BuildCallDescription(action *types.Action) string {
	if action.CallDesc != "" {
		return action.CallDesc
	}
	switch action.Type {
	case types.ActionToolCall:
		return fmt.Sprintf("调用工具 %s，参数: %v", action.ToolName, action.ToolInput)
	case types.ActionRespond:
		return "生成回复"
	default:
		return action.Reason
	}
}
