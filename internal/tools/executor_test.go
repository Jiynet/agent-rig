package tools_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
	"ai-cli-agent/pkg/types"
)

func TestConcurrentExecutorBatch(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewShellTool())
	exec := tools.NewConcurrentExecutor(reg, tools.ExecutorConfig{
		MaxConcurrency: 3,
		ToolTimeout:    10 * time.Second,
		MaxRetries:     0,
	})

	calls := []*types.ToolCall{
		{Name: "shell", Input: map[string]any{"command": "echo a"}},
		{Name: "shell", Input: map[string]any{"command": "echo b"}},
		{Name: "shell", Input: map[string]any{"command": "echo c"}},
	}

	results, err := exec.ExecuteBatch(context.Background(), calls)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	expected := []string{"a\n", "b\n", "c\n"}
	for i, res := range results {
		if !res.OK {
			t.Fatalf("result %d not OK: %s", i, res.Output)
		}
		if res.Output != expected[i] {
			t.Fatalf("result %d: expected %q, got %q", i, expected[i], res.Output)
		}
	}
}

func TestConcurrentExecutorEmpty(t *testing.T) {
	reg := tools.NewRegistry()
	exec := tools.NewConcurrentExecutor(reg, tools.DefaultExecutorConfig())

	results, err := exec.ExecuteBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error for empty batch, got: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for empty batch, got: %v", results)
	}
}

func TestConcurrentExecutorSingle(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewShellTool())
	exec := tools.NewConcurrentExecutor(reg, tools.DefaultExecutorConfig())

	calls := []*types.ToolCall{
		{Name: "shell", Input: map[string]any{"command": "echo single"}},
	}

	results, err := exec.ExecuteBatch(context.Background(), calls)
	if err != nil {
		t.Fatalf("single batch failed: %v", err)
	}
	if len(results) != 1 || !results[0].OK || results[0].Output != "single\n" {
		t.Fatalf("unexpected result: %+v", results)
	}
}

func TestConcurrentExecutorUnknownTool(t *testing.T) {
	reg := tools.NewRegistry()
	exec := tools.NewConcurrentExecutor(reg, tools.ExecutorConfig{
		MaxRetries: 0,
	})

	calls := []*types.ToolCall{
		{Name: "nonexistent", Input: map[string]any{}},
	}

	_, err := exec.ExecuteBatch(context.Background(), calls)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// TestConcurrentExecutorUnlimitedConcurrency 验证 MaxConcurrency <= 0 时
// 不会因为容量为 0 的无缓冲 channel 而永久阻塞。
func TestConcurrentExecutorUnlimitedConcurrency(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewShellTool())

	for _, maxConcurrency := range []int{0, -1} {
		exec := tools.NewConcurrentExecutor(reg, tools.ExecutorConfig{
			MaxConcurrency: maxConcurrency,
			ToolTimeout:    5 * time.Second,
		})
		calls := []*types.ToolCall{
			{Name: "shell", Input: map[string]any{"command": "echo a"}},
			{Name: "shell", Input: map[string]any{"command": "echo b"}},
			{Name: "shell", Input: map[string]any{"command": "echo c"}},
		}

		results, err := exec.ExecuteBatch(context.Background(), calls)
		if err != nil {
			t.Fatalf("MaxConcurrency=%d: batch failed: %v", maxConcurrency, err)
		}
		if len(results) != len(calls) {
			t.Fatalf("MaxConcurrency=%d: expected %d results, got %d", maxConcurrency, len(calls), len(results))
		}
		for i, res := range results {
			if res == nil || !res.OK {
				t.Fatalf("MaxConcurrency=%d: result %d not OK: %+v", maxConcurrency, i, res)
			}
		}
	}
}

func TestConcurrentExecutorRetry(t *testing.T) {
	var attempts atomic.Int32
	reg := tools.NewRegistry()
	reg.Register(&tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "flaky",
			Description: "有时失败的工具",
			Parameters:  map[string]any{"type": "object"},
		},
		Handler: func(_ context.Context, _ map[string]any) (*types.ToolResult, error) {
			n := attempts.Add(1)
			if n < 3 {
				return nil, fmt.Errorf("transient error #%d", n)
			}
			return &types.ToolResult{OK: true, Output: "success"}, nil
		},
	})

	exec := tools.NewConcurrentExecutor(reg, tools.ExecutorConfig{
		MaxRetries:   3,
		RetryBackoff: 10 * time.Millisecond,
	})

	calls := []*types.ToolCall{
		{Name: "flaky", Input: map[string]any{}},
	}

	results, err := exec.ExecuteBatch(context.Background(), calls)
	if err != nil {
		t.Fatalf("retry test failed: %v", err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected success after retries, got: %+v", results)
	}
}

func TestConcurrentExecutorConcurrency(t *testing.T) {
	var maxConcurrent atomic.Int32
	var current atomic.Int32

	reg := tools.NewRegistry()
	reg.Register(&tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:       "slow",
			Parameters: map[string]any{"type": "object"},
		},
		Handler: func(_ context.Context, _ map[string]any) (*types.ToolResult, error) {
			n := current.Add(1)
			// 记录最大并发数
			for {
				old := maxConcurrent.Load()
				if n <= old || maxConcurrent.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			current.Add(-1)
			return &types.ToolResult{OK: true, Output: "done"}, nil
		},
	})

	exec := tools.NewConcurrentExecutor(reg, tools.ExecutorConfig{
		MaxConcurrency: 2,
		ToolTimeout:    5 * time.Second,
	})

	calls := make([]*types.ToolCall, 6)
	for i := range calls {
		calls[i] = &types.ToolCall{Name: "slow", Input: map[string]any{}}
	}

	results, err := exec.ExecuteBatch(context.Background(), calls)
	if err != nil {
		t.Fatalf("concurrency test failed: %v", err)
	}
	if len(results) != 6 {
		t.Fatalf("expected 6 results, got %d", len(results))
	}

	// 最大并发数不应超过配置的 2
	if maxConcurrent.Load() > 2 {
		t.Fatalf("expected max concurrency <= 2, got %d", maxConcurrent.Load())
	}
}
