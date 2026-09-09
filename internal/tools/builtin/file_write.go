package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/pkg/types"
)

// 禁止写入的目录前缀（安全防护）。
var forbiddenPaths = []string{"/etc", "/bin", "/sbin", "/usr", "/var", "/root", "/boot", "/dev", "/proc", "/sys"}

// NewFileWriteTool 创建写入本地文件的工具。
// 包含路径安全校验，禁止写入系统目录。
func NewFileWriteTool() *tools.ToolSpec {
	return &tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "file_write",
			Description: "将内容写入本地文件，自动创建父目录",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "文件路径"},
					"content": map[string]any{"type": "string", "description": "要写入的内容"},
					"append":  map[string]any{"type": "boolean", "description": "是否追加模式（默认覆盖）"},
				},
				"required": []string{"path", "content"},
			},
		},
		Handler: func(_ context.Context, args map[string]any) (*types.ToolResult, error) {
			path, ok := args["path"].(string)
			if !ok || strings.TrimSpace(path) == "" {
				return &types.ToolResult{OK: false, Output: "缺少 path 参数"}, nil
			}
			content, ok := args["content"].(string)
			if !ok {
				return &types.ToolResult{OK: false, Output: "缺少 content 参数"}, nil
			}

			// 路径安全校验
			absPath, err := filepath.Abs(path)
			if err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("路径解析失败: %v", err)}, nil
			}
			for _, forbidden := range forbiddenPaths {
				if strings.HasPrefix(absPath, forbidden) {
					return &types.ToolResult{OK: false, Output: fmt.Sprintf("安全限制: 禁止写入 %s 目录", forbidden)}, nil
				}
			}

			// 创建父目录
			dir := filepath.Dir(absPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("创建目录失败: %v", err)}, nil
			}

			// 写入文件
			appendMode, _ := args["append"].(bool)
			flags := os.O_CREATE | os.O_WRONLY
			if appendMode {
				flags |= os.O_APPEND
			} else {
				flags |= os.O_TRUNC
			}

			f, err := os.OpenFile(absPath, flags, 0o644)
			if err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("打开文件失败: %v", err)}, nil
			}
			defer f.Close()

			n, err := f.WriteString(content)
			if err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("写入失败: %v", err)}, nil
			}

			return &types.ToolResult{
				OK:     true,
				Output: fmt.Sprintf("成功写入 %d 字节到 %s", n, absPath),
			}, nil
		},
	}
}
