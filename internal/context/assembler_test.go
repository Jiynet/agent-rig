package context_test

import (
	"fmt"
	"strings"
	"testing"

	ctxasm "ai-cli-agent/internal/context"
	"ai-cli-agent/pkg/types"
)

func msg(role types.Role, content string) *types.Message {
	return &types.Message{Role: role, Content: content}
}

func assistantWithCalls(content string, calls ...*types.ToolCall) *types.Message {
	return &types.Message{Role: types.RoleAssistant, Content: content, ToolCalls: calls}
}

func toolMsg(id, content string) *types.Message {
	return &types.Message{Role: types.RoleTool, ToolCallID: id, Content: content}
}

// buildRounds 构造 system + user + n 轮工具调用 + 最终回答。
func buildRounds(n, outputSize int) []*types.Message {
	history := []*types.Message{
		msg(types.RoleSystem, "你是一个测试助手，请遵循工具调用协议。"),
		msg(types.RoleUser, "分析这些日志并给出结论。"),
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("call_%d", i)
		history = append(history,
			assistantWithCalls(fmt.Sprintf("第 %d 轮，读取日志", i),
				&types.ToolCall{ID: id, Name: "big_output", Input: map[string]any{"n": outputSize}}),
			toolMsg(id, fmt.Sprintf("observation-%d ", i)+strings.Repeat("日志内容 ", outputSize)),
		)
	}
	history = append(history, msg(types.RoleAssistant, "最终结论：一切正常。"))
	return history
}

func assertPairing(t *testing.T, msgs []*types.Message) {
	t.Helper()
	calls := make(map[string]bool)
	answered := make(map[string]bool)
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role == types.RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc == nil {
					continue
				}
				if tc.ID == "" {
					t.Fatal("tool call with empty id")
				}
				calls[tc.ID] = true
			}
		}
		if m.Role == types.RoleTool {
			if m.ToolCallID == "" {
				t.Fatal("tool message with empty tool_call_id")
			}
			if !calls[m.ToolCallID] {
				t.Fatalf("orphan tool message: %q", m.ToolCallID)
			}
			answered[m.ToolCallID] = true
		}
	}
	for id := range calls {
		if !answered[id] {
			t.Fatalf("unanswered tool call: %q", id)
		}
	}
}

func findRole(msgs []*types.Message, role types.Role) []*types.Message {
	var out []*types.Message
	for _, m := range msgs {
		if m != nil && m.Role == role {
			out = append(out, m)
		}
	}
	return out
}

func TestPassThroughWhenDisabled(t *testing.T) {
	history := buildRounds(3, 10)
	a := ctxasm.NewAssembler(ctxasm.Config{MaxPromptTokens: 0})

	out, stats := a.Build(history)
	if len(out) != len(history) {
		t.Fatalf("expected %d messages, got %d", len(history), len(out))
	}
	if stats.AssembledTokens != stats.OriginalTokens {
		t.Fatalf("expected original == assembled, got %d vs %d", stats.OriginalTokens, stats.AssembledTokens)
	}
}

func TestKeepsSystemAndGoal(t *testing.T) {
	history := buildRounds(8, 50)
	a := ctxasm.NewAssembler(ctxasm.Config{
		MaxPromptTokens:     300,
		RecentMessages:      2,
		MaxToolOutputTokens: 50,
		SummaryMaxTokens:    50,
	})

	out, _ := a.Build(history)
	if len(findRole(out, types.RoleSystem)) == 0 {
		t.Fatal("system message dropped")
	}
	users := findRole(out, types.RoleUser)
	if len(users) == 0 || users[0].Content != history[1].Content {
		t.Fatalf("user goal not preserved: %+v", users)
	}
}

func TestTruncatesToolOutput(t *testing.T) {
	history := buildRounds(2, 200)
	a := ctxasm.NewAssembler(ctxasm.Config{
		MaxPromptTokens:     1000,
		RecentMessages:      8,
		MaxToolOutputTokens: 40,
		SummaryMaxTokens:    50,
	})

	out, stats := a.Build(history)
	if stats.TruncatedToolMessages == 0 {
		t.Fatal("expected tool outputs to be truncated")
	}
	found := false
	for _, m := range findRole(out, types.RoleTool) {
		if strings.Contains(m.Content, "truncated") && len([]rune(m.Content)) < 2000 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected truncated tool marker in output")
	}
}

func TestPreservesToolPairing(t *testing.T) {
	history := buildRounds(10, 80)
	a := ctxasm.NewAssembler(ctxasm.Config{
		MaxPromptTokens:     400,
		RecentMessages:      3,
		MaxToolOutputTokens: 40,
		SummaryMaxTokens:    60,
	})

	out, stats := a.Build(history)
	assertPairing(t, out)
	if stats.SummarizedMessages == 0 {
		t.Fatal("expected old messages to be summarized")
	}
}

func TestSummarizesOldHistory(t *testing.T) {
	history := buildRounds(8, 100)
	a := ctxasm.NewAssembler(ctxasm.Config{
		MaxPromptTokens:     300,
		RecentMessages:      2,
		MaxToolOutputTokens: 30,
		SummaryMaxTokens:    80,
	})

	out, stats := a.Build(history)
	if !stats.SummaryUsed {
		t.Fatal("expected summary to be used")
	}
	if stats.SummarizedMessages == 0 {
		t.Fatal("expected summarized messages > 0")
	}
	found := false
	for _, m := range out {
		if strings.Contains(m.Content, "历史对话摘要") {
			found = true
		}
	}
	if !found {
		t.Fatal("summary message not found")
	}
}

func TestRespectsBudget(t *testing.T) {
	history := buildRounds(12, 150)
	budget := 500
	a := ctxasm.NewAssembler(ctxasm.Config{
		MaxPromptTokens:     budget,
		RecentMessages:      4,
		MaxToolOutputTokens: 50,
		SummaryMaxTokens:    60,
	})

	out, stats := a.Build(history)
	if stats.AssembledTokens > budget {
		t.Fatalf("assembled %d exceeds budget %d", stats.AssembledTokens, budget)
	}
	if stats.AssembledTokens >= stats.OriginalTokens {
		t.Fatalf("expected compression, got %d >= %d", stats.AssembledTokens, stats.OriginalTokens)
	}
	_ = out
}

func TestLatestObservationKept(t *testing.T) {
	// 模拟“工具刚执行完、等待下一次 LLM 推理”的真实状态：
	// 最后一条消息是 tool 结果，而不是最终 assistant 回答。
	history := buildRounds(6, 200)
	history = history[:len(history)-1]

	a := ctxasm.NewAssembler(ctxasm.Config{
		MaxPromptTokens:     80,
		RecentMessages:      1,
		MaxToolOutputTokens: 30,
		SummaryMaxTokens:    20,
	})

	out, stats := a.Build(history)
	if !stats.OverBudget {
		t.Fatal("expected over-budget flag for very small budget")
	}
	// 最新一轮的工具结果（可能被截断）必须还在。
	found := false
	for _, m := range out {
		if m.Role == types.RoleTool && strings.Contains(m.Content, "observation-5") {
			found = true
		}
	}
	if !found {
		t.Fatal("latest observation dropped")
	}
	assertPairing(t, out)
}

func TestEstimateTokens(t *testing.T) {
	if got := ctxasm.EstimateTokens(""); got != 0 {
		t.Fatalf("empty string: got %d", got)
	}
	if got := ctxasm.EstimateTokens("hello world"); got == 0 {
		t.Fatal("english text should have tokens")
	}
	if got := ctxasm.EstimateTokens("你好，世界"); got < 4 {
		t.Fatalf("chinese text token estimate too low: %d", got)
	}
}
