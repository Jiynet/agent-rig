package builtin

import (
	"context"
	"os"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/pkg/types"
)

// NewFileReadTool 创建读取本地文件内容的工具。
func NewFileReadTool() *tools.ToolSpec {
	return &tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "file_read",
			Description: "读取本地文件内容",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "文件路径"},
				},
				"required": []string{"path"},
			},
		},
		Handler: func(_ context.Context, args map[string]any) (*types.ToolResult, error) {
			path, ok := args["path"].(string)
			if !ok || path == "" {
				return &types.ToolResult{OK: false, Output: "缺少 path 参数"}, nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return &types.ToolResult{OK: false, Output: err.Error(), Error: err}, nil
			}
			return &types.ToolResult{OK: true, Output: string(data)}, nil
		},
	}
}
