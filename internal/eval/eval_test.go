package eval_test

import (
	"testing"

	"ai-cli-agent/internal/eval"
)

func findByTask(results []eval.Result, task string, mode eval.Mode) *eval.Result {
	for i := range results {
		if results[i].Task == task && results[i].Mode == mode {
			return &results[i]
		}
	}
	return nil
}

func TestDefaultTasksBothModes(t *testing.T) {
	cfg := eval.DefaultConfig()
	results := eval.RunAll(eval.DefaultTasks(), []eval.Mode{eval.ModeFull, eval.ModeAssembled}, cfg)
	if len(results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("%s/%s failed: %s", r.Task, r.Mode, r.Reason)
		}
	}
}

func TestAssembledSavesTokens(t *testing.T) {
	cfg := eval.DefaultConfig()
	full := eval.Run(eval.DefaultTasks(), eval.ModeFull, cfg)
	assembled := eval.Run(eval.DefaultTasks(), eval.ModeAssembled, cfg)

	for _, task := range []string{"long_tool_output", "multi_round_context"} {
		f := findByTask(full, task, eval.ModeFull)
		a := findByTask(assembled, task, eval.ModeAssembled)
		if f == nil || a == nil {
			t.Fatalf("missing result for %s", task)
		}
		if !f.Success || !a.Success {
			t.Fatalf("%s should succeed in both modes", task)
		}
		if a.PromptTokens >= f.PromptTokens {
			t.Fatalf("%s: assembled prompt tokens %d should be less than full %d",
				task, a.PromptTokens, f.PromptTokens)
		}
	}
}

func TestBatchToolMetrics(t *testing.T) {
	cfg := eval.DefaultConfig()
	results := eval.Run(eval.DefaultTasks(), eval.ModeAssembled, cfg)
	r := findByTask(results, "multi_tool_batch", eval.ModeAssembled)
	if r == nil {
		t.Fatal("missing multi_tool_batch result")
	}
	if !r.Success {
		t.Fatalf("multi_tool_batch failed: %s", r.Reason)
	}
	if r.ToolCalls != 2 {
		t.Fatalf("expected 2 tool calls in batch, got %d", r.ToolCalls)
	}
}

func TestMaxIterations(t *testing.T) {
	cfg := eval.DefaultConfig()
	results := eval.Run(eval.DefaultTasks(), eval.ModeAssembled, cfg)
	r := findByTask(results, "max_iterations", eval.ModeAssembled)
	if r == nil {
		t.Fatal("missing max_iterations result")
	}
	if !r.Success {
		t.Fatalf("max_iterations failed: %s", r.Reason)
	}
	if r.Steps != 3 {
		t.Fatalf("expected 3 steps, got %d", r.Steps)
	}
}

func TestSummarizationInLongContext(t *testing.T) {
	cfg := eval.DefaultConfig()
	results := eval.Run(eval.DefaultTasks(), eval.ModeAssembled, cfg)
	r := findByTask(results, "multi_round_context", eval.ModeAssembled)
	if r == nil {
		t.Fatal("missing multi_round_context result")
	}
	if r.SummarizedMessages == 0 {
		t.Fatal("expected old messages to be summarized")
	}
	if r.TruncatedTools == 0 {
		t.Fatal("expected tool outputs to be truncated")
	}
}
