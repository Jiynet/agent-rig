package tools_test

import (
	"context"
	"testing"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
	"ai-cli-agent/pkg/types"
)

// TestShellToolExecutesCommand 验证 shell 工具能真实执行命令。
func TestShellToolExecutesCommand(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewShellTool())

	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "shell",
		Input: map[string]any{"command": "echo hello"},
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !res.OK || res.Output != "hello\n" {
		t.Fatalf("unexpected result: ok=%v out=%q", res.OK, res.Output)
	}
}

// TestUnknownToolReturnsError 验证调用未注册工具返回错误。
func TestUnknownToolReturnsError(t *testing.T) {
	reg := tools.NewRegistry()
	_, err := reg.Execute(context.Background(), &types.ToolCall{Name: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
