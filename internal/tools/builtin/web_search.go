package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/pkg/types"
)

const (
	searchTimeout = 10 * time.Second
	searchAPIURL  = "https://api.duckduckgo.com/"
)

// NewWebSearchTool 创建网络搜索工具。
// 使用 DuckDuckGo Instant Answer API（免费，无需 API Key）。
func NewWebSearchTool() *tools.ToolSpec {
	client := &http.Client{Timeout: searchTimeout}

	return &tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "web_search",
			Description: "搜索网络获取信息，返回摘要和相关链接",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "搜索关键词"},
				},
				"required": []string{"query"},
			},
		},
		Handler: func(ctx context.Context, args map[string]any) (*types.ToolResult, error) {
			query, ok := args["query"].(string)
			if !ok || strings.TrimSpace(query) == "" {
				return &types.ToolResult{OK: false, Output: "缺少 query 参数"}, nil
			}

			result, err := search(ctx, client, query)
			if err != nil {
				return &types.ToolResult{OK: false, Output: fmt.Sprintf("搜索失败: %v", err)}, nil
			}
			return &types.ToolResult{OK: true, Output: result}, nil
		},
	}
}

// search 执行 DuckDuckGo Instant Answer API 搜索。
func search(ctx context.Context, client *http.Client, query string) (string, error) {
	params := url.Values{
		"q":             {query},
		"format":        {"json"},
		"no_html":       {"1"},
		"skip_disambig": {"1"},
	}

	reqURL := searchAPIURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "agent-rig/0.1")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("api error status=%d body=%s", resp.StatusCode, string(body))
	}

	var result duckDuckGoResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return formatSearchResult(query, &result), nil
}

// duckDuckGoResult 是 DuckDuckGo API 的响应结构。
type duckDuckGoResult struct {
	Abstract      string `json:"Abstract"`
	AbstractURL   string `json:"AbstractURL"`
	Heading       string `json:"Heading"`
	RelatedTopics []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"RelatedTopics"`
	Answer     string `json:"Answer"`
	AnswerType string `json:"AnswerType"`
}

// formatSearchResult 格式化搜索结果。
func formatSearchResult(query string, r *duckDuckGoResult) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "搜索: %s\n", query)

	if r.Heading != "" {
		_, _ = fmt.Fprintf(&sb, "\n## %s\n", r.Heading)
	}

	if r.Abstract != "" {
		_, _ = fmt.Fprintf(&sb, "\n%s\n", r.Abstract)
		if r.AbstractURL != "" {
			_, _ = fmt.Fprintf(&sb, "来源: %s\n", r.AbstractURL)
		}
	}

	if r.Answer != "" {
		_, _ = fmt.Fprintf(&sb, "\n答案: %s\n", r.Answer)
	}

	if len(r.RelatedTopics) > 0 {
		sb.WriteString("\n相关主题:\n")
		count := 5
		if count > len(r.RelatedTopics) {
			count = len(r.RelatedTopics)
		}
		for i := 0; i < count; i++ {
			t := r.RelatedTopics[i]
			if t.Text != "" {
				_, _ = fmt.Fprintf(&sb, "- %s", t.Text)
				if t.FirstURL != "" {
					_, _ = fmt.Fprintf(&sb, " (%s)", t.FirstURL)
				}
				sb.WriteString("\n")
			}
		}
	}

	if sb.Len() == len(fmt.Sprintf("搜索: %s\n", query)) {
		sb.WriteString("\n未找到相关结果。\n")
	}

	return sb.String()
}
