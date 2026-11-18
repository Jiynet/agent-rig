package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/pkg/types"
)

const httpRequestTimeout = 15 * time.Second

// NewHTTPRequestTool 创建 HTTP 请求工具。
// 支持 GET/POST 请求，返回响应状态码和内容。
func NewHTTPRequestTool() *tools.ToolSpec {
	client := &http.Client{Timeout: httpRequestTimeout}

	return &tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "http_request",
			Description: "发送 HTTP 请求并返回响应",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":     map[string]any{"type": "string", "description": "请求 URL"},
					"method":  map[string]any{"type": "string", "description": "HTTP 方法（GET/POST，默认 GET）"},
					"body":    map[string]any{"type": "string", "description": "请求体（POST 时使用）"},
					"headers": map[string]any{"type": "object", "description": "请求头"},
				},
				"required": []string{"url"},
			},
		},
		Handler: func(ctx context.Context, args map[string]any) (*types.ToolResult, error) {
			urlStr, ok := args["url"].(string)
			if !ok || strings.TrimSpace(urlStr) == "" {
				return &types.ToolResult{OK: false, Output: "缺少 url 参数"}, nil
			}

			method := "GET"
			if m, ok := args["method"].(string); ok && m != "" {
				method = strings.ToUpper(m)
			}

			var body io.Reader
			if b, ok := args["body"].(string); ok && b != "" {
				body = strings.NewReader(b)
			}

			req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
			if err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("创建请求失败: %v", err)}, nil
			}

			// 设置请求头
			if headers, ok := args["headers"].(map[string]any); ok {
				for k, v := range headers {
					if sv, ok := v.(string); ok {
						req.Header.Set(k, sv)
					}
				}
			}
			if req.Header.Get("User-Agent") == "" {
				req.Header.Set("User-Agent", "agent-rig/0.1")
			}

			resp, err := client.Do(req)
			if err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("请求失败: %v", err)}, nil
			}
			defer func() { _ = resp.Body.Close() }()

			// 限制读取大小（防止内存溢出）
			limitedReader := io.LimitReader(resp.Body, 64*1024) // 64KB
			respBody, err := io.ReadAll(limitedReader)
			if err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("读取响应失败: %v", err)}, nil
			}

			var sb strings.Builder
			_, _ = fmt.Fprintf(&sb, "HTTP %s %s\n", method, urlStr)
			_, _ = fmt.Fprintf(&sb, "Status: %d %s\n", resp.StatusCode, resp.Status)
			_, _ = fmt.Fprintf(&sb, "Content-Type: %s\n", resp.Header.Get("Content-Type"))
			_, _ = fmt.Fprintf(&sb, "\n%s", string(respBody))

			return &types.ToolResult{
				OK:     resp.StatusCode >= 200 && resp.StatusCode < 400,
				Output: sb.String(),
			}, nil
		},
	}
}
