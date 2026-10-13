package planning_test

import (
	"context"
	"testing"

	"ai-cli-agent/internal/execution"
	"ai-cli-agent/internal/planning"
	"ai-cli-agent/pkg/types"
)

// scriptedLLM 按调用顺序返回预设响应。
type scriptedLLM struct {
	responses []*types.GenerateResponse
	idx       int
}

func (s *scriptedLLM) Generate(_ context.Context, _ *types.GenerateRequest) (*types.GenerateResponse, error) {
	if s.idx >= len(s.responses) {
		return &types.GenerateResponse{Content: "done"}, nil
	}
	r := s.responses[s.idx]
	s.idx++
	return r, nil
}

func (s *scriptedLLM) GenerateStream(_ context.Context, _ *types.GenerateRequest, _ func(chunk types.StreamChunk) error) error {
	return nil
}

// autoApprove 是自动批准所有确认的感知实现。
type autoApprove struct{}

func (a *autoApprove) ParseInput(_ context.Context, raw string) (*types.Intent, error) {
	return &types.Intent{RawInput: raw, Goal: raw}, nil
}
func (a *autoApprove) PromptForJudgment(_ context.Context, _ string, _ *types.Action) (*types.JudgmentResult, error) {
	return &types.JudgmentResult{Decision: types.DecisionApprove}, nil
}
func (a *autoApprove) FormatOutput(_ context.Context, _ *types.AgentResult) (string, error) {
	return "", nil
}
func (a *autoApprove) RenderChunk(_ context.Context, _ types.StreamChunk) error { return nil }

func TestReActPlannerDirectResponse(t *testing.T) {
	// LLM 直接给出最终回答，不调用工具
	client := &scriptedLLM{
		responses: []*types.GenerateResponse{
			{Content: "42"},
		},
	}
	exec := execution.NewConfirmExecution(&autoApprove{})
	planner := planning.NewReActPlanner(client, exec, "test", 5)

	intent := &types.Intent{Goal: "什么是生命的意义？"}
	plan, err := planner.CreatePlan(context.Background(), intent, nil)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	state := planning.NewTaskState(intent.Goal, plan)

	// 第一次选择动作：LLM 应该返回最终回答
	action, err := planner.SelectNextAction(context.Background(), state)
	if err != nil {
		t.Fatalf("SelectNextAction: %v", err)
	}
	if action == nil {
		t.Fatal("expected action, got nil")
	}
	if action.Type != types.ActionRespond {
		t.Fatalf("expected ActionRespond, got %s", action.Type)
	}
	if action.Reason != "42" {
		t.Fatalf("expected '42', got %q", action.Reason)
	}

	// 更新状态
	state, err = planner.UpdateState(context.Background(), state, &types.ActionResult{
		ActionID: action.ID,
		OK:       true,
		Output:   action.Reason,
	})
	if err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	// 应该判定完成
	done, err := planner.EvaluateCompletion(context.Background(), state)
	if err != nil {
		t.Fatalf("EvaluateCompletion: %v", err)
	}
	if !done {
		t.Fatal("expected task to be done")
	}
}

func TestReActPlannerToolCallLoop(t *testing.T) {
	// LLM 先调用工具，再给出最终回答
	client := &scriptedLLM{
		responses: []*types.GenerateResponse{
			{
				ToolCalls: []*types.ToolCall{
					{ID: "call_1", Name: "shell", Input: map[string]any{"command": "echo hello"}},
				},
			},
			{Content: "工具输出了 hello"},
		},
	}
	exec := execution.NewConfirmExecution(&autoApprove{})
	planner := planning.NewReActPlanner(client, exec, "test", 5)

	intent := &types.Intent{Goal: "执行 echo hello"}
	plan, _ := planner.CreatePlan(context.Background(), intent, nil)
	state := planning.NewTaskState(intent.Goal, plan)

	// 第1轮：LLM 返回工具调用
	action1, err := planner.SelectNextAction(context.Background(), state)
	if err != nil {
		t.Fatalf("SelectNextAction: %v", err)
	}
	if action1.Type != types.ActionToolCall {
		t.Fatalf("expected ActionToolCall, got %s", action1.Type)
	}
	if action1.ToolName != "shell" {
		t.Fatalf("expected shell, got %s", action1.ToolName)
	}

	// 模拟工具执行结果
	state, err = planner.UpdateState(context.Background(), state, &types.ActionResult{
		ActionID: action1.ID,
		OK:       true,
		Output:   "hello\n",
	})
	if err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	// 不应该完成（LLM 还没给出最终回答）
	done, _ := planner.EvaluateCompletion(context.Background(), state)
	if done {
		t.Fatal("should not be done after tool call")
	}

	// 第2轮：LLM 给出最终回答
	action2, err := planner.SelectNextAction(context.Background(), state)
	if err != nil {
		t.Fatalf("SelectNextAction: %v", err)
	}
	if action2.Type != types.ActionRespond {
		t.Fatalf("expected ActionRespond, got %s", action2.Type)
	}

	state, _ = planner.UpdateState(context.Background(), state, &types.ActionResult{
		ActionID: action2.ID,
		OK:       true,
		Output:   action2.Reason,
	})

	done, _ = planner.EvaluateCompletion(context.Background(), state)
	if !done {
		t.Fatal("expected done after final response")
	}

	// 汇总结果
	result, err := planner.SynthesizeResult(context.Background(), state)
	if err != nil {
		t.Fatalf("SynthesizeResult: %v", err)
	}
	if result.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestReActPlannerMaxIterations(t *testing.T) {
	// LLM 一直返回工具调用，直到达到迭代上限
	client := &scriptedLLM{
		responses: []*types.GenerateResponse{
			{ToolCalls: []*types.ToolCall{{ID: "c1", Name: "shell", Input: map[string]any{"command": "echo 1"}}}},
			{ToolCalls: []*types.ToolCall{{ID: "c2", Name: "shell", Input: map[string]any{"command": "echo 2"}}}},
			{ToolCalls: []*types.ToolCall{{ID: "c3", Name: "shell", Input: map[string]any{"command": "echo 3"}}}},
		},
	}
	exec := execution.NewConfirmExecution(&autoApprove{})
	planner := planning.NewReActPlanner(client, exec, "test", 2) // 最多 2 轮

	intent := &types.Intent{Goal: "无限循环测试"}
	plan, _ := planner.CreatePlan(context.Background(), intent, nil)
	state := planning.NewTaskState(intent.Goal, plan)

	// 第1轮
	action, _ := planner.SelectNextAction(context.Background(), state)
	if action == nil {
		t.Fatal("expected action in round 1")
	}
	state, _ = planner.UpdateState(context.Background(), state, &types.ActionResult{
		ActionID: action.ID, OK: true, Output: "ok",
	})

	// 第2轮
	action, _ = planner.SelectNextAction(context.Background(), state)
	if action == nil {
		t.Fatal("expected action in round 2")
	}
	state, _ = planner.UpdateState(context.Background(), state, &types.ActionResult{
		ActionID: action.ID, OK: true, Output: "ok",
	})

	// 第3轮：应该达到上限，返回 nil
	action, _ = planner.SelectNextAction(context.Background(), state)
	if action != nil {
		t.Fatal("expected nil after max iterations")
	}

	done, _ := planner.EvaluateCompletion(context.Background(), state)
	if !done {
		t.Fatal("expected done after max iterations")
	}
}

func TestReActPlannerDrainPendingActions(t *testing.T) {
	client := &scriptedLLM{
		responses: []*types.GenerateResponse{
			{
				ToolCalls: []*types.ToolCall{
					{ID: "c1", Name: "shell", Input: map[string]any{"command": "echo a"}},
					{ID: "c2", Name: "shell", Input: map[string]any{"command": "echo b"}},
					{ID: "c3", Name: "shell", Input: map[string]any{"command": "echo c"}},
				},
			},
			{Content: "done"},
		},
	}
	exec := execution.NewConfirmExecution(&autoApprove{})
	planner := planning.NewReActPlanner(client, exec, "test", 5)

	intent := &types.Intent{Goal: "batch test"}
	plan, _ := planner.CreatePlan(context.Background(), intent, nil)
	state := planning.NewTaskState(intent.Goal, plan)

	// 第一次选择：触发 LLM，返回第一个工具调用
	action, _ := planner.SelectNextAction(context.Background(), state)
	if action == nil {
		t.Fatal("expected first action")
	}

	// 此时应该还有 2 个待执行
	if !planner.HasPendingActions() {
		t.Fatal("expected pending actions")
	}

	// 排空所有待执行动作
	batch := planner.DrainPendingActions()
	if len(batch) != 2 {
		t.Fatalf("expected 2 pending actions, got %d", len(batch))
	}

	// 排空后应该没有待执行了
	if planner.HasPendingActions() {
		t.Fatal("expected no pending actions after drain")
	}
}

func TestTaskStateCreation(t *testing.T) {
	intent := &types.Intent{Goal: "test goal"}
	plan := &types.Plan{ID: "p1", Goal: "test goal"}
	state := planning.NewTaskState(intent.Goal, plan)

	if state.TaskID != "test goal" {
		t.Fatalf("expected 'test goal', got %s", state.TaskID)
	}
	if state.Iteration != 0 {
		t.Fatalf("expected 0 iterations, got %d", state.Iteration)
	}
	if state.Plan != plan {
		t.Fatal("plan mismatch")
	}
	if state.Finished {
		t.Fatal("should not be finished")
	}
}
