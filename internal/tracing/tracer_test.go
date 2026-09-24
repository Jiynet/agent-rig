package tracing

import (
	"bytes"
	"strings"
	"testing"

	"ai-cli-agent/pkg/types"
)

func TestConsoleTracerRecordsMetrics(t *testing.T) {
	var buf bytes.Buffer
	tracer := NewConsoleTracer(&buf)

	// 模拟一次 LLM 调用
	span := tracer.Start("llm.generate", map[string]any{"model": "gpt-4o"})
	span.SetAttr("prompt_tokens", 100)
	span.SetAttr("completion_tokens", 50)
	span.SetAttr("total_tokens", 150)
	span.End(types.SpanOK, nil)

	// 模拟一次工具调用
	toolSpan := tracer.Start("tool.execute", map[string]any{"tool_name": "shell"})
	toolSpan.End(types.SpanOK, nil)

	// 模拟一次 ReAct 迭代
	iterSpan := tracer.Start("react.iteration", nil)
	iterSpan.End(types.SpanOK, nil)

	m := tracer.Metrics()
	if m.LLMCalls != 1 {
		t.Fatalf("expected 1 LLM call, got %d", m.LLMCalls)
	}
	if m.PromptTokens != 100 {
		t.Fatalf("expected 100 prompt tokens, got %d", m.PromptTokens)
	}
	if m.CompletionTokens != 50 {
		t.Fatalf("expected 50 completion tokens, got %d", m.CompletionTokens)
	}
	if m.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", m.ToolCalls)
	}
	if m.LoopIterations != 1 {
		t.Fatalf("expected 1 loop iteration, got %d", m.LoopIterations)
	}

	// 验证输出包含追踪信息
	output := buf.String()
	if !strings.Contains(output, "llm.generate") {
		t.Fatal("expected output to contain llm.generate")
	}
	if !strings.Contains(output, "tool.execute") {
		t.Fatal("expected output to contain tool.execute")
	}
}

func TestConsoleTracerChildSpan(t *testing.T) {
	var buf bytes.Buffer
	tracer := NewConsoleTracer(&buf)

	parent := tracer.Start("agent.run", nil)
	child := parent.StartChild("plan.create", nil)
	child.End(types.SpanOK, nil)
	parent.End(types.SpanOK, nil)

	m := tracer.Metrics()
	// 子跨度不直接增加指标，但应该正确输出
	_ = m

	output := buf.String()
	if !strings.Contains(output, "agent.run") {
		t.Fatal("expected output to contain agent.run")
	}
	if !strings.Contains(output, "plan.create") {
		t.Fatal("expected output to contain plan.create")
	}
}

func TestConsoleTracerReset(t *testing.T) {
	tracer := NewConsoleTracer(&bytes.Buffer{})

	span := tracer.Start("llm.generate", nil)
	span.SetAttr("prompt_tokens", 100)
	span.End(types.SpanOK, nil)

	if tracer.Metrics().LLMCalls != 1 {
		t.Fatal("expected 1 LLM call before reset")
	}

	tracer.Reset()
	if tracer.Metrics().LLMCalls != 0 {
		t.Fatal("expected 0 LLM calls after reset")
	}
}

func TestNoopTracer(t *testing.T) {
	tracer := NewNoopTracer()
	span := tracer.Start("test", nil)
	span.SetAttr("key", "value")
	child := span.StartChild("child", nil)
	child.End(types.SpanOK, nil)
	span.End(types.SpanOK, nil)

	if tracer.Metrics().LLMCalls != 0 {
		t.Fatal("noop tracer should return zero metrics")
	}
}

func TestConsoleTracerErrorSpan(t *testing.T) {
	var buf bytes.Buffer
	tracer := NewConsoleTracer(&buf)

	span := tracer.Start("tool.execute", nil)
	span.End(types.SpanError, nil)

	m := tracer.Metrics()
	if m.ToolErrors != 1 {
		t.Fatalf("expected 1 tool error, got %d", m.ToolErrors)
	}

	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Fatal("expected error icon in output")
	}
}
