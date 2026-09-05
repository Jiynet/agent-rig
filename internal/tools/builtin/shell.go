package builtin

import (
	"bytes"
	"context"
	"os/exec"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/pkg/types"
)

// NewShellTool 创建执行本地 shell 命令的工具。
// 注意：shell 工具风险较高，注册时应配合执行层的风险确认策略。
func NewShellTool() *tools.ToolSpec {
	return &tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "shell",
			Description: "在本地终端执行 shell 命令并返回输出",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "要执行的命令"},
				},
				"required": []string{"command"},
			},
		},
		Handler: func(ctx context.Context, args map[string]any) (*types.ToolResult, error) {
			cmdStr, ok := args["command"].(string)
			if !ok || cmdStr == "" {
				return &types.ToolResult{OK: false, Output: "缺少 command 参数"}, nil
			}

			cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			res := &types.ToolResult{OK: err == nil, Output: stdout.String()}
			if err != nil {
				res.Error = err
				if stderr.Len() > 0 {
					res.Output += "\n[stderr] " + stderr.String()
				}
			}
			return res, nil
		},
	}
}
