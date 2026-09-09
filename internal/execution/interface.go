// Package execution 是执行层（用户确认网关）：
// 负责生成调用说明并协调感知层收集用户判断，本身不直接调用工具或 LLM。
package execution

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// Execution 是执行层的抽象接口。
type Execution interface {
	RequestExecution(ctx context.Context, action *types.Action) (*types.JudgmentResult, error)
}
