package agent

import (
	"context"
	"fmt"
	"strings"

	"ai-cli-agent/internal/planning"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/pkg/types"
)

// batchPlanner 是支持批量获取待执行动作的规划器扩展接口。
type batchPlanner interface {
	planning.Planner
	DrainPendingActions() []*types.Action
	HasPendingActions() bool
}

// contextPlanner 是支持注入附加上下文的规划器扩展接口。
type contextPlanner interface {
	planning.Planner
	SetContextHint(hint string)
}

// Run 执行 Agent 主控制循环：
// 解析输入 → 创建计划 → ReAct 循环（选择/确认/执行/更新/判定）→ 汇总结果。
func (a *Agent) Run(ctx context.Context, input string) (*types.AgentResult, error) {
	a.tracer.Reset()

	root := a.tracer.Start("agent.run", map[string]any{"input": input})
	defer root.End(types.SpanOK, nil)

	// 1. 感知：解析输入为意图。
	parseSpan := a.tracer.Start("perception.parse_input", nil)
	intent, err := a.perception.ParseInput(ctx, input)
	if err != nil {
		parseSpan.End(types.SpanError, err)
		return nil, fmt.Errorf("parse input: %w", err)
	}
	parseSpan.SetAttr("goal", intent.Goal)
	parseSpan.End(types.SpanOK, nil)

	// 2. 规划：创建初始计划。
	planSpan := a.tracer.Start("plan.create", nil)
	plan, err := a.planner.CreatePlan(ctx, intent, nil)
	if err != nil {
		planSpan.End(types.SpanError, err)
		return nil, fmt.Errorf("create plan: %w", err)
	}
	planSpan.SetAttr("plan_id", plan.ID)
	planSpan.End(types.SpanOK, nil)
	a.bus.Publish("plan.created", plan)

	// 2.5 记忆检索：根据目标检索相关历史记忆，注入规划器上下文。
	if cp, ok := a.planner.(contextPlanner); ok {
		memSpan := a.tracer.Start("memory.retrieve", nil)
		memories, err := a.memory.Retrieve(ctx, intent.Goal, 5)
		if err == nil && len(memories) > 0 {
			var hint strings.Builder
			for i, m := range memories {
				fmt.Fprintf(&hint, "%d. [%s] %s\n", i+1, m.Kind, m.Content)
			}
			cp.SetContextHint(hint.String())
			memSpan.SetAttr("count", len(memories))
		}
		memSpan.End(types.SpanOK, nil)
	}

	// 3. ReAct 循环。
	state := planning.NewTaskState(intent.Goal, plan)
	bp, canBatch := a.planner.(batchPlanner)

	for {
		iterSpan := a.tracer.Start("react.iteration", map[string]any{
			"iteration": state.Iteration,
		})

		action, err := a.planner.SelectNextAction(ctx, state)
		if err != nil {
			iterSpan.End(types.SpanError, err)
			return nil, fmt.Errorf("select next action: %w", err)
		}
		if action == nil {
			iterSpan.End(types.SpanOK, nil)
			break // 无更多动作，结束循环
		}
		iterSpan.SetAttr("action_type", string(action.Type))
		if action.ToolName != "" {
			iterSpan.SetAttr("tool_name", action.ToolName)
		}

		// 如果规划器支持批量获取且有多个待执行动作，并发执行。
		if canBatch && bp.HasPendingActions() {
			batchActions := bp.DrainPendingActions()
			// 将当前 action 也加入批次
			allActions := append([]*types.Action{action}, batchActions...)
			iterSpan.SetAttr("batch_size", len(allActions))

			results, batchErr := a.executeBatch(ctx, allActions)
			if batchErr != nil {
				iterSpan.End(types.SpanError, batchErr)
				return nil, batchErr
			}

			// 依次更新状态
			for _, result := range results {
				state, err = a.planner.UpdateState(ctx, state, result)
				if err != nil {
					iterSpan.End(types.SpanError, err)
					return nil, err
				}
			}
			done, err := a.planner.EvaluateCompletion(ctx, state)
			if err != nil {
				iterSpan.End(types.SpanError, err)
				return nil, err
			}
			iterSpan.End(types.SpanOK, nil)
			if done {
				break
			}
			continue
		}

		// 单个动作执行路径
		judgmentSpan := a.tracer.Start("execution.confirm", nil)
		judgment, err := a.planner.DispatchAction(ctx, action)
		if err != nil {
			judgmentSpan.End(types.SpanError, err)
			iterSpan.End(types.SpanError, err)
			return nil, fmt.Errorf("dispatch action: %w", err)
		}
		judgmentSpan.End(types.SpanOK, nil)

		if judgment != nil && judgment.Decision == types.DecisionReject {
			iterSpan.SetAttr("user_reject", true)
			rejectResult := &types.ActionResult{
				ActionID: action.ID,
				OK:       false,
				Output:   fmt.Sprintf("用户拒绝执行，类型=%s 工具=%s 反馈: %s", action.Type, action.ToolName, judgment.Feedback),
			}
			state, err = a.planner.UpdateState(ctx, state, rejectResult)
			if err != nil {
				iterSpan.End(types.SpanError, err)
				return nil, err
			}
			done, err := a.planner.EvaluateCompletion(ctx, state)
			if err != nil {
				iterSpan.End(types.SpanError, err)
				return nil, err
			}
			iterSpan.End(types.SpanOK, nil)
			if done {
				break
			}
			continue
		}

		var result *types.ActionResult
		switch action.Type {
		case types.ActionToolCall:
			call := &types.ToolCall{ID: action.ID, Name: action.ToolName, Input: action.ToolInput}
			if judgment != nil && judgment.ModifiedInput != nil {
				call.Input = judgment.ModifiedInput
			}
			toolSpan := a.tracer.Start("tool.execute", map[string]any{
				"tool_name": action.ToolName,
			})
			tr, err := a.tools.Execute(ctx, call)
			if err != nil {
				toolSpan.End(types.SpanError, err)
				iterSpan.End(types.SpanError, err)
				return nil, fmt.Errorf("execute tool %s: %w", action.ToolName, err)
			}
			toolSpan.SetAttr("ok", tr.OK)
			toolSpan.End(types.SpanOK, nil)
			result = &types.ActionResult{ActionID: action.ID, OK: tr.OK, Output: tr.Output, Error: tr.Error}
		default:
			result = &types.ActionResult{ActionID: action.ID, OK: true, Output: action.Reason}
		}
		a.bus.Publish("action.executed", result)

		state, err = a.planner.UpdateState(ctx, state, result)
		if err != nil {
			iterSpan.End(types.SpanError, err)
			return nil, err
		}
		done, err := a.planner.EvaluateCompletion(ctx, state)
		if err != nil {
			iterSpan.End(types.SpanError, err)
			return nil, err
		}
		iterSpan.End(types.SpanOK, nil)
		if done {
			break
		}
	}

	// 4. 汇总结果。
	synthSpan := a.tracer.Start("plan.synthesize", nil)
	agentResult, err := a.planner.SynthesizeResult(ctx, state)
	if err != nil {
		synthSpan.End(types.SpanError, err)
		return nil, fmt.Errorf("synthesize result: %w", err)
	}
	synthSpan.End(types.SpanOK, nil)

	// 5. 记忆：记录本次会话（失败不影响主流程）。
	memSpan := a.tracer.Start("memory.store", nil)
	if err := a.memory.Store(ctx, &types.MemoryEntry{
		Key:     intent.Goal,
		Kind:    "short_term",
		Content: agentResult.Summary,
	}); err != nil {
		memSpan.End(types.SpanError, err)
		return nil, fmt.Errorf("store memory: %w", err)
	}
	memSpan.End(types.SpanOK, nil)

	// 输出汇总指标
	metrics := a.tracer.Metrics()
	root.SetAttr("llm_calls", metrics.LLMCalls)
	root.SetAttr("tool_calls", metrics.ToolCalls)
	root.SetAttr("total_tokens", metrics.TotalTokens)
	root.SetAttr("loop_iterations", metrics.LoopIterations)

	return agentResult, nil
}

// executeBatch 并发执行一批工具动作，用户确认通过 DispatchAction 逐个处理。
func (a *Agent) executeBatch(ctx context.Context, actions []*types.Action) ([]*types.ActionResult, error) {
	// 先逐个请求用户确认（批量确认模式：展示所有待执行动作，用户一次性批准/拒绝）
	confirmed := make([]*types.Action, 0, len(actions))
	for _, action := range actions {
		judgment, err := a.planner.DispatchAction(ctx, action)
		if err != nil {
			return nil, fmt.Errorf("dispatch action %s: %w", action.ToolName, err)
		}
		if judgment != nil && judgment.Decision == types.DecisionReject {
			// 拒绝的动作生成拒绝结果
			confirmed = append(confirmed, nil) // 占位
			continue
		}
		if judgment != nil && judgment.ModifiedInput != nil {
			action.ToolInput = judgment.ModifiedInput
		}
		confirmed = append(confirmed, action)
	}

	// 构造工具调用列表（跳过被拒绝的）
	calls := make([]*types.ToolCall, 0, len(confirmed))
	callIndex := make([]int, 0, len(confirmed)) // 记录在 confirmed 中的位置
	for i, action := range confirmed {
		if action == nil {
			continue // 被拒绝
		}
		calls = append(calls, &types.ToolCall{
			ID:    action.ID,
			Name:  action.ToolName,
			Input: action.ToolInput,
		})
		callIndex = append(callIndex, i)
	}

	// 使用 ConcurrentExecutor 并发执行
	batchSpan := a.tracer.Start("tool.execute_batch", map[string]any{"batch_size": len(calls)})
	executor := tools.NewConcurrentExecutor(a.tools, tools.DefaultExecutorConfig())
	toolResults, err := executor.ExecuteBatch(ctx, calls)
	if err != nil {
		batchSpan.End(types.SpanError, err)
		return nil, fmt.Errorf("batch execute: %w", err)
	}
	toolErrors := 0
	for _, tr := range toolResults {
		if tr != nil && !tr.OK {
			toolErrors++
		}
	}
	batchSpan.SetAttr("tool_calls", len(calls))
	batchSpan.SetAttr("tool_errors", toolErrors)
	batchSpan.End(types.SpanOK, nil)

	// 组装结果（包含被拒绝的动作）
	results := make([]*types.ActionResult, len(actions))
	toolIdx := 0
	for i, action := range confirmed {
		if action == nil {
			// 被拒绝的动作
			results[i] = &types.ActionResult{
				ActionID: actions[i].ID,
				OK:       false,
				Output:   fmt.Sprintf("用户拒绝执行工具 %s", actions[i].ToolName),
			}
		} else {
			tr := toolResults[toolIdx]
			results[i] = &types.ActionResult{
				ActionID: action.ID,
				OK:       tr.OK,
				Output:   tr.Output,
				Error:    tr.Error,
			}
			toolIdx++
		}
	}

	return results, nil
}

// Metrics 返回最近一次 Run 的累计指标。
func (a *Agent) Metrics() types.AgentMetrics {
	return a.tracer.Metrics()
}
