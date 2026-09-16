package builtin_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
	"ai-cli-agent/pkg/types"
)

func TestFileWriteAndRead(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewFileWriteTool())
	reg.Register(builtin.NewFileReadTool())

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "hello from ai-cli-agent"

	// 写入
	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "file_write",
		Input: map[string]any{"path": testFile, "content": content},
	})
	if err != nil {
		t.Fatalf("file_write failed: %v", err)
	}
	if !res.OK {
		t.Fatalf("file_write not OK: %s", res.Output)
	}

	// 读取验证
	readRes, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "file_read",
		Input: map[string]any{"path": testFile},
	})
	if err != nil {
		t.Fatalf("file_read failed: %v", err)
	}
	if readRes.Output != content {
		t.Fatalf("expected %q, got %q", content, readRes.Output)
	}
}

func TestFileWriteAppend(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewFileWriteTool())

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "append.txt")

	// 第一次写入
	reg.Execute(context.Background(), &types.ToolCall{
		Name:  "file_write",
		Input: map[string]any{"path": testFile, "content": "line1\n"},
	})

	// 追加写入
	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "file_write",
		Input: map[string]any{"path": testFile, "content": "line2\n", "append": true},
	})
	if err != nil {
		t.Fatalf("file_write append failed: %v", err)
	}
	if !res.OK {
		t.Fatalf("file_write append not OK: %s", res.Output)
	}

	data, _ := os.ReadFile(testFile)
	expected := "line1\nline2\n"
	if string(data) != expected {
		t.Fatalf("expected %q, got %q", expected, string(data))
	}
}

func TestFileWriteForbiddenPath(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewFileWriteTool())

	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "file_write",
		Input: map[string]any{"path": "/etc/test_agent.txt", "content": "bad"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure for forbidden path")
	}
}

func TestFileWriteMissingParams(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewFileWriteTool())

	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "file_write",
		Input: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure for missing params")
	}
}

func TestHTTPRequestTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewHTTPRequestTool())

	// 使用 httpbin 的 GET 端点（公开测试服务）
	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "http_request",
		Input: map[string]any{"url": "https://httpbin.org/get"},
	})
	if err != nil {
		t.Fatalf("http_request failed: %v", err)
	}
	// 网络可能不可用，跳过而非失败
	if !res.OK {
		t.Skipf("http_request not OK (network may be unavailable): %s", res.Output)
	}
}

func TestHTTPRequestMissingURL(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewHTTPRequestTool())

	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "http_request",
		Input: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure for missing URL")
	}
}

func TestWebSearchTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewWebSearchTool())

	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "web_search",
		Input: map[string]any{"query": "Go programming language"},
	})
	if err != nil {
		t.Fatalf("web_search failed: %v", err)
	}
	// DuckDuckGo API 可能返回空结果，但不应报错
	if !res.OK {
		t.Skipf("web_search not OK (API may be unavailable): %s", res.Output)
	}
}

func TestWebSearchMissingQuery(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(builtin.NewWebSearchTool())

	res, err := reg.Execute(context.Background(), &types.ToolCall{
		Name:  "web_search",
		Input: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure for missing query")
	}
}

func TestAllToolsRegistered(t *testing.T) {
	specs := builtin.All()
	expected := []string{"shell", "file_read", "file_write", "web_search", "http_request"}

	if len(specs) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(specs))
	}

	names := make(map[string]bool)
	for _, s := range specs {
		names[s.Definition.Name] = true
	}
	for _, name := range expected {
		if !names[name] {
			t.Fatalf("missing tool: %s", name)
		}
	}
}
