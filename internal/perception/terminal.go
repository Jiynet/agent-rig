package perception

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-cli-agent/pkg/types"
)

// TerminalPerception 是基于终端交互的感知实现。
type TerminalPerception struct {
	in  *bufio.Reader
	out io.Writer
}

// NewTerminalPerception 创建终端感知实例，从 stdin 读取、向 stdout 输出。
func NewTerminalPerception() *TerminalPerception {
	return &TerminalPerception{in: bufio.NewReader(os.Stdin), out: os.Stdout}
}

// ParseInput 将终端输入的原始字符串解析为意图。
func (p *TerminalPerception) ParseInput(_ context.Context, rawInput string) (*types.Intent, error) {
	rawInput = strings.TrimSpace(rawInput)
	if rawInput == "" {
		return nil, fmt.Errorf("empty input")
	}
	// 骨架阶段：原始输入即目标，后续可扩展意图识别。
	return &types.Intent{RawInput: rawInput, Goal: rawInput}, nil
}

// PromptForJudgment 向用户展示调用说明并收集判断结果。
func (p *TerminalPerception) PromptForJudgment(_ context.Context, callDesc string, action *types.Action) (*types.JudgmentResult, error) {
	fmt.Fprintf(p.out, "\n[执行确认] %s\n  - 说明: %s\n  批准? [回车=批准, n=拒绝, m=修改] ", action.Type, callDesc)

	line, err := p.in.ReadString('\n')
	if err != nil && line == "" {
		return nil, err
	}
	line = strings.ToLower(strings.TrimSpace(line))

	switch line {
	case "", "y", "yes", "ok", "approve":
		return &types.JudgmentResult{Decision: types.DecisionApprove}, nil
	case "n", "no", "q", "reject":
		return &types.JudgmentResult{Decision: types.DecisionReject, Feedback: "user rejected"}, nil
	case "m", "modify":
		return &types.JudgmentResult{Decision: types.DecisionModify, Feedback: "user will modify input"}, nil
	default:
		return &types.JudgmentResult{Decision: types.DecisionUnknown, Feedback: line}, nil
	}
}

// FormatOutput 将 Agent 结果格式化为终端展示文本。
func (p *TerminalPerception) FormatOutput(_ context.Context, result *types.AgentResult) (string, error) {
	var sb strings.Builder
	sb.WriteString("\n================ Agent Result ================\n")
	sb.WriteString("摘要: " + result.Summary + "\n")
	if result.Detail != "" {
		sb.WriteString("详情:\n" + result.Detail + "\n")
	}
	sb.WriteString("=============================================\n")
	return sb.String(), nil
}

// RenderChunk 渲染流式输出分片，直接写向终端。
func (p *TerminalPerception) RenderChunk(_ context.Context, chunk types.StreamChunk) error {
	fmt.Fprint(p.out, chunk.Delta)
	return nil
}
