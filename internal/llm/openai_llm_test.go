package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-cli-agent/pkg/types"
)

// newMockServer 启动一个模拟 OpenAI 兼容接口的 httptest 服务。
// handler 接收请求体字节并返回原始响应体（由测试自行拼装）。
// 同时校验请求方法为 POST、路径为 /chat/completions。
func newMockServer(t *testing.T, handler func(body []byte) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, handler(body))
	}))
}

// TestOpenAILLMGenerate 验证非流式请求：请求体组装、内容/tool_calls/usage 解析。
func TestOpenAILLMGenerate(t *testing.T) {
	srv := newMockServer(t, func(body []byte) string {
		var req struct {
			Stream bool  `json:"stream"`
			Tools  []any `json:"tools"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Stream {
			t.Error("expected stream=false for Generate")
		}
		if len(req.Tools) != 1 {
			t.Errorf("expected 1 tool in request, got %d", len(req.Tools))
		}
		return `{"choices":[{"message":{"content":"hi","tool_calls":[{"id":"call_1","type":"function","function":{"name":"shell","arguments":"{\"command\":\"echo hi\"}"}}]}}],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}`
	})
	defer srv.Close()

	client := NewOpenAILLM("", srv.URL)
	resp, err := client.Generate(context.Background(), &types.GenerateRequest{
		Model:    "gpt-4o",
		Messages: []*types.Message{{Role: types.RoleUser, Content: "hi"}},
		Tools:    []*types.ToolDefinition{{Name: "shell", Description: "run shell", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content != "hi" {
		t.Fatalf("expected content 'hi', got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.Name != "shell" || tc.ID != "call_1" {
		t.Fatalf("unexpected tool call: %+v", tc)
	}
	if tc.Input["command"] != "echo hi" {
		t.Fatalf("expected input command 'echo hi', got %v", tc.Input["command"])
	}
	if resp.Usage.PromptTokens != 11 || resp.Usage.TotalTokens != 16 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

// TestOpenAILLMGenerateStream 验证 SSE 流式解析：多个 data 分片拼接 + [DONE] 结束。
func TestOpenAILLMGenerateStream(t *testing.T) {
	srv := newMockServer(t, func(body []byte) string {
		var req struct {
			Stream bool `json:"stream"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !req.Stream {
			t.Error("expected stream=true for GenerateStream")
		}
		return "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
			"data: [DONE]\n\n"
	})
	defer srv.Close()

	client := NewOpenAILLM("", srv.URL)
	var sb strings.Builder
	var done bool
	err := client.GenerateStream(context.Background(), &types.GenerateRequest{
		Model:    "gpt-4o",
		Messages: []*types.Message{{Role: types.RoleUser, Content: "hi"}},
	}, func(c types.StreamChunk) error {
		if c.Done {
			done = true
			return nil
		}
		sb.WriteString(c.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if sb.String() != "Hello" {
		t.Fatalf("expected streamed content 'Hello', got %q", sb.String())
	}
	if !done {
		t.Fatal("expected [DONE] to set Done flag")
	}
}

// TestOpenAILLMGenerateStreamToolCall 验证流式响应中的 tool_call 增量解析。
func TestOpenAILLMGenerateStreamToolCall(t *testing.T) {
	srv := newMockServer(t, func(body []byte) string {
		return "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"shell\",\"arguments\":\"{}\"}}]}}]}\n\n" +
			"data: [DONE]\n\n"
	})
	defer srv.Close()

	client := NewOpenAILLM("", srv.URL)
	var got *types.ToolCall
	err := client.GenerateStream(context.Background(), &types.GenerateRequest{
		Model:    "gpt-4o",
		Messages: []*types.Message{{Role: types.RoleUser, Content: "run"}},
	}, func(c types.StreamChunk) error {
		if c.ToolCall != nil {
			got = c.ToolCall
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if got == nil {
		t.Fatal("expected tool call in stream")
	}
	if got.Name != "shell" || got.ID != "c1" {
		t.Fatalf("unexpected tool call: %+v", got)
	}
}

// TestOpenAILLMGenerateError 验证非 200 状态返回错误并携带状态码。
func TestOpenAILLMGenerateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"bad key"}}`)
	}))
	defer srv.Close()

	client := NewOpenAILLM("", srv.URL)
	_, err := client.Generate(context.Background(), &types.GenerateRequest{
		Model:    "gpt-4o",
		Messages: []*types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected error to contain status 401, got: %v", err)
	}
}
