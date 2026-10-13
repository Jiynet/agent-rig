package planning

import (
	"context"
	"fmt"

	ctxasm "ai-cli-agent/internal/context"
	"ai-cli-agent/internal/execution"
	"ai-cli-agent/internal/llm"
	"ai-cli-agent/internal/tracing"
	"ai-cli-agent/pkg/types"
)

// finalActionID 是 LLM 给出最终回答时的动作 ID 标记，
// UpdateState 据此将回复作为 assistant 消息写回历史。
const finalActionID = "final"

// ReActPlanner 实现 ReAct 循环（推理 → 行动 → 观察 → 再推理）的规划器。
//
// 与骨架阶段的占位实现不同，ReActPlanner 真正驱动 LLM：
//   - 将用户目标注入系统提示，并暴露可用工具定义；
//   - LLM 返回 tool_calls 时，逐个转换为 ActionToolCall 交给执行层确认与执行；
//   - 工具结果通过 tool 消息写回历史，再让 LLM 基于观察继续推理；
//   - 直到 LLM 给出最终回答（ActionRespond），任务结束。
//
// 短期对话历史由规划层自行维护在 TaskState.History 中，不依赖记忆系统。
type ReActPlanner struct {
	llm         llm.LLM
	execution   execution.Execution
	tracer      tracing.Tracer
	model       string // 调用的模型名
	maxIter     int    // 最大循环次数，防止死循环
	tools       []*types.ToolDefinition
	contextHint string // 注入系统提示的附加上下文（如记忆检索结果）
	assembler   *ctxasm.Assembler
	lastContext ctxasm.Stats // 最近一次推理前的上下文组装统计

	// externalHistory 是 CreatePlan 时由上层传入的短期历史，首次推理时并入。
	externalHistory []types.Message
	// pendingCalls 是 LLM 本轮返回、尚未全部转成 Action 分发的工具调用队列。
	pendingCalls []*types.ToolCall
	// lastToolCalls / lastAssistant 暂存最近一次 LLM 的推理记录，
	// 待对应动作执行完成后一并写回历史（assistant → tool 顺序）。
	lastToolCalls []*types.ToolCall
	lastAssistant string
	responded     bool // LLM 是否已给出最终回答
	seeded        bool // 首次推理时是否已注入系统提示与用户目标
	callSeq       int  // 为缺少 ID 的工具调用生成回退 ID
}

// NewReActPlanner 构造 ReAct 规划器，maxIter <= 0 时使用默认值 10。
func NewReActPlanner(l llm.LLM, e execution.Execution, model string, maxIter int) *ReActPlanner {
	if maxIter <= 0 {
		maxIter = 10
	}
	return &ReActPlanner{
		llm:       l,
		execution: e,
		model:     model,
		maxIter:   maxIter,
		tracer:    tracing.NewNoopTracer(),
		assembler: ctxasm.NewAssembler(ctxasm.DefaultConfig()),
	}
}

// SetAssembler 替换上下文组装器；传 nil 表示保持默认。
func (p *ReActPlanner) SetAssembler(a *ctxasm.Assembler) {
	if a != nil {
		p.assembler = a
	}
}

// ContextStats 返回最近一次推理前的上下文组装统计。
func (p *ReActPlanner) ContextStats() ctxasm.Stats {
	return p.lastContext
}

// SetTracer 注入追踪器，用于记录 LLM 调用的 token 用量等指标。
func (p *ReActPlanner) SetTracer(t tracing.Tracer) {
	if t != nil {
		p.tracer = t
	}
}

// SetContextHint 注入附加上下文（如记忆检索结果），会追加到系统提示中。
func (p *ReActPlanner) SetContextHint(hint string) {
	p.contextHint = hint
}

// SetTools 注入可供 LLM 选择调用的工具定义，在组装阶段注册完工具后调用。
func (p *ReActPlanner) SetTools(defs []*types.ToolDefinition) {
	p.tools = defs
}

// CreatePlan 记录任务目标与上层传入的短期历史。
// ReAct 计划是动态推进的：不预先拆解为固定步骤，推理循环逐轮产生动作。
func (p *ReActPlanner) CreatePlan(_ context.Context, intent *types.Intent, history []types.Message) (*types.Plan, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent is nil")
	}
	// 每次 CreatePlan 代表一轮独立任务，必须清空上一轮遗留的推理状态，
	// 否则 responded/seeded 等字段会让后续 Run 直接跳过 LLM。
	p.resetRun()
	p.externalHistory = history
	return &types.Plan{
		ID:   intent.Goal,
		Goal: intent.Goal,
		Steps: []*types.Step{{
			ID:     "react-loop",
			Action: &types.Action{Type: types.ActionRespond, Reason: intent.Goal},
		}},
	}, nil
}

// resetRun 重置一轮任务内的所有瞬时状态。工具定义（p.tools）与模型配置
// 跨任务保留，只有与具体对话相关的推理状态需要清空。
func (p *ReActPlanner) resetRun() {
	p.responded = false
	p.seeded = false
	p.pendingCalls = nil
	p.lastAssistant = ""
	p.lastToolCalls = nil
	p.contextHint = ""
	p.callSeq = 0
	p.lastContext = ctxasm.Stats{}
}

// SelectNextAction 推进一轮 ReAct：优先分发待执行的工具调用，
// 否则调用 LLM 推理，根据返回产出工具调用或最终回答动作。
// 到达迭代上限或已生成最终回答时返回 (nil, nil)，表示循环应结束。
func (p *ReActPlanner) SelectNextAction(ctx context.Context, state *types.TaskState) (*types.Action, error) {
	if state == nil || state.Plan == nil {
		return nil, fmt.Errorf("state or plan is nil")
	}
	if p.responded || state.Iteration >= p.maxIter {
		return nil, nil
	}

	// 优先分发上一轮 LLM 返回的尚未处理完的工具调用。
	if len(p.pendingCalls) > 0 {
		call := p.pendingCalls[0]
		p.pendingCalls = p.pendingCalls[1:]
		return p.toolAction(call), nil
	}

	// 首次推理：注入系统提示、外部短期历史与用户目标。
	if !p.seeded {
		p.seeded = true
		messages := []*types.Message{{Role: types.RoleSystem, Content: p.systemPrompt()}}
		for i := range p.externalHistory {
			m := p.externalHistory[i]
			messages = append(messages, &m)
		}
		messages = append(messages, &types.Message{Role: types.RoleUser, Content: state.Plan.Goal})
		state.History = append(state.History, messages...)
	}

	// 在发送给 LLM 前做预算化上下文组装；state.History 保持完整，
	// 便于最终汇总与下一轮继续推理。
	messages := state.History
	if p.assembler != nil {
		assembled, stats := p.assembler.Build(state.History)
		if len(assembled) > 0 {
			messages = assembled
		}
		p.lastContext = stats
	}

	llmSpan := p.tracer.Start("llm.generate", map[string]any{"model": p.model})
	llmSpan.SetAttr("context_tokens", p.lastContext.AssembledTokens)
	llmSpan.SetAttr("context_original_tokens", p.lastContext.OriginalTokens)
	llmSpan.SetAttr("context_summarized", p.lastContext.SummarizedMessages)
	llmSpan.SetAttr("context_truncated_tools", p.lastContext.TruncatedToolMessages)
	resp, err := p.llm.Generate(ctx, &types.GenerateRequest{
		Model:    p.model,
		Messages: messages,
		Tools:    p.tools,
	})
	if err != nil {
		llmSpan.End(types.SpanError, err)
		return nil, fmt.Errorf("react reasoning: %w", err)
	}
	llmSpan.SetAttr("prompt_tokens", resp.Usage.PromptTokens)
	llmSpan.SetAttr("completion_tokens", resp.Usage.CompletionTokens)
	llmSpan.SetAttr("total_tokens", resp.Usage.TotalTokens)
	llmSpan.SetAttr("tool_calls_count", len(resp.ToolCalls))
	llmSpan.End(types.SpanOK, nil)

	// 暂存本轮推理记录，待动作执行完成后由 UpdateState 写回历史。
	p.lastAssistant = resp.Content
	p.lastToolCalls = resp.ToolCalls

	if len(resp.ToolCalls) > 0 {
		p.pendingCalls = resp.ToolCalls
		call := p.pendingCalls[0]
		p.pendingCalls = p.pendingCalls[1:]
		return p.toolAction(call), nil
	}

	// LLM 直接给出最终回答，任务结束。
	p.responded = true
	return &types.Action{
		ID:     finalActionID,
		Type:   types.ActionRespond,
		Reason: resp.Content,
	}, nil
}

// DispatchAction 将动作交给执行层（用户确认网关）处理。
func (p *ReActPlanner) DispatchAction(ctx context.Context, action *types.Action) (*types.JudgmentResult, error) {
	return p.execution.RequestExecution(ctx, action)
}

// UpdateState 根据一次执行结果推进状态：工具结果写回为 tool 消息，
// 最终回答写回为 assistant 消息，形成可供 LLM 继续推理的完整对话历史。
func (p *ReActPlanner) UpdateState(_ context.Context, state *types.TaskState, result *types.ActionResult) (*types.TaskState, error) {
	if state == nil {
		return nil, fmt.Errorf("state is nil")
	}
	state.LastResult = result
	state.Iteration++

	if result == nil {
		return state, nil
	}

	if result.ActionID == finalActionID {
		// 最终回答：将 assistant 消息写回历史。
		state.History = append(state.History, &types.Message{Role: types.RoleAssistant, Content: result.Output})
		p.lastAssistant = ""
		p.lastToolCalls = nil
		return state, nil
	}

	// 工具执行结果（成功/失败/被用户拒绝）：先写回带工具调用声明的 assistant 消息，
	// 再写回 tool 结果消息，二者通过 ToolCallID 关联，LLM 据此继续推理。
	p.flushAssistant(state)
	state.History = append(state.History, &types.Message{
		Role:       types.RoleTool,
		ToolCallID: result.ActionID,
		Content:    result.Output,
	})
	return state, nil
}

// EvaluateCompletion 判断任务是否终止：LLM 已给出最终回答，或迭代达到上限。
func (p *ReActPlanner) EvaluateCompletion(_ context.Context, state *types.TaskState) (bool, error) {
	if state == nil {
		return true, nil
	}
	return p.responded || state.Iteration >= p.maxIter, nil
}

// SynthesizeResult 汇总执行过程生成最终结果：最终回答作为摘要，
// 并从历史中收集实际执行过的工具调用。
func (p *ReActPlanner) SynthesizeResult(_ context.Context, state *types.TaskState) (*types.AgentResult, error) {
	if state == nil {
		return nil, fmt.Errorf("state is nil")
	}
	out := &types.AgentResult{Summary: "任务完成"}
	if state.LastResult != nil {
		out.Summary = state.LastResult.Output
		out.Detail = state.LastResult.Output
		if state.LastResult.Error != nil {
			out.Err = state.LastResult.Error
		}
	}
	for _, m := range state.History {
		if m == nil || m.Role != types.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc == nil {
				continue
			}
			out.Actions = append(out.Actions, &types.Action{
				Type:      types.ActionToolCall,
				ToolName:  tc.Name,
				ToolInput: tc.Input,
			})
		}
	}
	return out, nil
}

// DrainPendingActions 返回当前所有待执行的工具调用动作并清空队列。
// 当 LLM 一轮返回多个 tool_calls 时，用于批量收集以支持并发执行。
// 返回 nil 表示没有待执行的动作。
func (p *ReActPlanner) DrainPendingActions() []*types.Action {
	if len(p.pendingCalls) == 0 {
		return nil
	}
	actions := make([]*types.Action, 0, len(p.pendingCalls))
	for _, call := range p.pendingCalls {
		actions = append(actions, p.toolAction(call))
	}
	p.pendingCalls = nil
	return actions
}

// HasPendingActions 返回是否有待执行的工具调用。
func (p *ReActPlanner) HasPendingActions() bool {
	return len(p.pendingCalls) > 0
}

// toolAction 将一个工具调用转换为待执行的动作，动作 ID 复用工具调用 ID，
// 便于工具结果写回时通过 ToolCallID 关联；ID 缺失时生成回退值。
func (p *ReActPlanner) toolAction(call *types.ToolCall) *types.Action {
	id := call.ID
	if id == "" {
		p.callSeq++
		id = fmt.Sprintf("call_%d", p.callSeq)
	}
	return &types.Action{
		ID:        id,
		Type:      types.ActionToolCall,
		ToolName:  call.Name,
		ToolInput: call.Input,
		Reason:    fmt.Sprintf("调用工具 %s 以推进任务", call.Name),
	}
}

// flushAssistant 将最近一次 LLM 的推理与工具调用声明写回历史。
func (p *ReActPlanner) flushAssistant(state *types.TaskState) {
	if p.lastAssistant == "" && len(p.lastToolCalls) == 0 {
		return
	}
	state.History = append(state.History, &types.Message{
		Role:      types.RoleAssistant,
		Content:   p.lastAssistant,
		ToolCalls: p.lastToolCalls,
	})
	p.lastAssistant = ""
	p.lastToolCalls = nil
}

// systemPrompt 构造 ReAct 系统提示，指导 LLM 按"推理 → 工具调用 → 观察 → 最终回答"推进。
func (p *ReActPlanner) systemPrompt() string {
	base := `你是运行在终端里的 AI 助手。请遵循"推理 → 调用工具 → 观察结果 → 再推理"的循环完成任务：
需要获取信息或执行操作时调用提供的工具；
当任务已完成、或无需调用工具即可回答时，直接给出最终回答。`
	if p.contextHint != "" {
		base += "\n\n相关历史记忆：\n" + p.contextHint
	}
	return base
}
