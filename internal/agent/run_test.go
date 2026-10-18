package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ai-cli-agent/internal/eventbus"
	"ai-cli-agent/internal/execution"
	"ai-cli-agent/internal/llm"
	"ai-cli-agent/internal/memory"
	"ai-cli-agent/internal/planning"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
	"ai-cli-agent/pkg/types"
)

// fakePerception 是测试用感知实现：自动批准所有确认请求，避免阻塞 stdin。
type fakePerception struct{}

func (f *fakePerception) ParseInput(_ context.Context, raw string) (*types.Intent, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("empty input")
	}
	return &types.Intent{RawInput: raw, Goal: raw}, nil
}

func (f *fakePerception) PromptForJudgment(_ context.Context, _ string, _ *types.Action) (*types.JudgmentResult, error) {
	return &types.JudgmentResult{Decision: types.DecisionApprove}, nil
}

func (f *fakePerception) FormatOutput(_ context.Context, _ *types.AgentResult) (string, error) {
	return "", nil
}

func (f *fakePerception) RenderChunk(_ context.Context, _ types.StreamChunk) error { return nil }

// newTestAgent 组装一个可离线运行（无真实 LLM/网络）的 Agent。
func newTestAgent() *Agent {
	perc := &fakePerception{}
	exec := execution.NewConfirmExecution(perc)
	client := &llm.MockLLM{Response: "好的，我来帮你完成这个任务。"}

	planner := planning.NewReActPlanner(client, exec, "mock-model", 5)

	registry := tools.NewRegistry()
	for _, spec := range builtin.All() {
		registry.Register(spec)
	}

	return New(Config{
		Perception: perc,
		Planner:    planner,
		Execution:  exec,
		LLM:        client,
		Tools:      registry,
		Memory:     memory.NewNoop(),
		Bus:        eventbus.NewSimpleBus(),
	})
}

// TestAgentRunFullLoop 验证完整主循环：
// 感知 → 规划(LLM) → 确认 → 执行 → 汇总 → 记忆。
func TestAgentRunFullLoop(t *testing.T) {
	agt := newTestAgent()
	result, err := agt.Run(context.Background(), "帮我总结一下今天的工作")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result == nil || !strings.Contains(result.Summary, "好的") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// TestAgentRunMultipleTurns 验证同一个 Agent 连续执行多轮任务时，
// Planner 的推理状态会被重置，第二轮仍会真正调用 LLM。
func TestAgentRunMultipleTurns(t *testing.T) {
	agt := newTestAgent()

	first, err := agt.Run(context.Background(), "第一个问题")
	if err != nil {
		t.Fatalf("first Run failed: %v", err)
	}
	if first == nil || !strings.Contains(first.Summary, "好的") {
		t.Fatalf("unexpected first result: %+v", first)
	}

	second, err := agt.Run(context.Background(), "第二个问题")
	if err != nil {
		t.Fatalf("second Run failed: %v", err)
	}
	if second == nil || !strings.Contains(second.Summary, "好的") {
		t.Fatalf("second Run did not call LLM again: %+v", second)
	}
}

// TestAgentRunRejectsEmptyInput 验证空输入被感知层拒绝。
func TestAgentRunRejectsEmptyInput(t *testing.T) {
	agt := newTestAgent()
	_, err := agt.Run(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

// scriptedLLM 按调用顺序返回预设响应，用于验证 ReAct 多轮循环的确定性测试。
type scriptedLLM struct {
	responses []*types.GenerateResponse
	idx       int
}

func (s *scriptedLLM) Generate(_ context.Context, _ *types.GenerateRequest) (*types.GenerateResponse, error) {
	if s.idx >= len(s.responses) {
		return &types.GenerateResponse{}, nil
	}
	r := s.responses[s.idx]
	s.idx++
	return r, nil
}

func (s *scriptedLLM) GenerateStream(_ context.Context, _ *types.GenerateRequest, _ func(chunk types.StreamChunk) error) error {
	return nil
}

// TestAgentRunReactToolCallLoop 验证 ReAct 循环完整闭环：
// LLM 先请求调用 shell 工具 → 执行层确认 → 工具真实执行 → 结果写回历史 → LLM 给出最终回答。
func TestAgentRunReactToolCallLoop(t *testing.T) {
	perc := &fakePerception{}
	exec := execution.NewConfirmExecution(perc)
	client := &scriptedLLM{
		responses: []*types.GenerateResponse{
			{ToolCalls: []*types.ToolCall{{ID: "call_1", Name: "shell", Input: map[string]any{"command": "echo react-ok"}}}},
			{Content: "已完成，工具输出为 react-ok"},
		},
	}

	planner := planning.NewReActPlanner(client, exec, "mock-model", 5)
	registry := tools.NewRegistry()
	for _, spec := range builtin.All() {
		registry.Register(spec)
	}
	defs, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	planner.SetTools(defs)

	agt := New(Config{
		Perception: perc,
		Planner:    planner,
		Execution:  exec,
		LLM:        client,
		Tools:      registry,
		Memory:     memory.NewNoop(),
		Bus:        eventbus.NewSimpleBus(),
	})

	result, err := agt.Run(context.Background(), "执行 echo react-ok")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result == nil || !strings.Contains(result.Summary, "已完成") {
		t.Fatalf("unexpected result: %+v", result)
	}
	// 结果应记录实际执行过的工具动作。
	if len(result.Actions) != 1 || result.Actions[0].ToolName != "shell" {
		t.Fatalf("expected one shell action, got %+v", result.Actions)
	}
}
