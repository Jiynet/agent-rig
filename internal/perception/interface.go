// Package perception 负责输入解析、用户确认展示与输出格式化，
// 是 Agent 与终端用户之间的界面层。
package perception

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// Perception 是感知层的抽象接口。
type Perception interface {
	// ParseInput 将原始输入解析为意图。
	ParseInput(ctx context.Context, rawInput string) (*types.Intent, error)
	// PromptForJudgment 向用户展示一次调用的说明并收集判断结果。
	PromptForJudgment(ctx context.Context, callDesc string, action *types.Action) (*types.JudgmentResult, error)
	// FormatOutput 将最终结果格式化为可展示文本。
	FormatOutput(ctx context.Context, result *types.AgentResult) (string, error)
	// RenderChunk 渲染流式输出分片。
	RenderChunk(ctx context.Context, chunk types.StreamChunk) error
}
