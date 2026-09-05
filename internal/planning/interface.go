// Package planning 负责任务拆解、状态管理与调度执行（ReAct 循环）。
package planning

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// Planner 是规划层的抽象接口。
type Planner interface {
	// CreatePlan 根据意图与历史消息创建初始计划。
	CreatePlan(ctx context.Context, intent *types.Intent, history []types.Message) (*types.Plan, error)
	// SelectNextAction 从当前状态中选出下一个要执行的动作。
	SelectNextAction(ctx context.Context, state *types.TaskState) (*types.Action, error)
	// DispatchAction 将动作交给执行层（用户确认网关）处理。
	DispatchAction(ctx context.Context, action *types.Action) (*types.JudgmentResult, error)
	// UpdateState 根据一次执行结果更新任务状态。
	UpdateState(ctx context.Context, state *types.TaskState, result *types.ActionResult) (*types.TaskState, error)
	// EvaluateCompletion 判断任务是否已完成。
	EvaluateCompletion(ctx context.Context, state *types.TaskState) (bool, error)
	// SynthesizeResult 汇总执行过程，生成最终结果。
	SynthesizeResult(ctx context.Context, state *types.TaskState) (*types.AgentResult, error)
}
