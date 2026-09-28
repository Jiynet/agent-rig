package memory_test

import (
	"context"
	"testing"

	"ai-cli-agent/internal/memory"
	"ai-cli-agent/pkg/types"
)

func TestVectorMemoryStoreAndRetrieve(t *testing.T) {
	mem := memory.NewVectorMemory()
	ctx := context.Background()

	// 存入几条记忆
	entries := []struct {
		key     string
		content string
	}{
		{"golang", "Go 语言 golang 并发编程 goroutine channel"},
		{"python", "Python 是一种解释型高级编程语言"},
		{"redis", "Redis 是一个开源的内存数据结构存储"},
		{"tcp", "TCP 是传输层可靠连接协议，使用三次握手建立连接"},
		{"docker", "Docker 是一个开源的容器化平台"},
	}

	for _, e := range entries {
		if err := mem.Store(ctx, &types.MemoryEntry{
			Key:     e.key,
			Kind:    "long_term",
			Content: e.content,
		}); err != nil {
			t.Fatalf("store failed: %v", err)
		}
	}

	// 检索与 "Go 语言并发" 相关的记忆
	results, err := mem.Retrieve(ctx, "Go 语言并发编程", 3)
	if err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	// 第一条应该是 golang 相关的
	if results[0].Key != "golang" {
		t.Fatalf("expected 'golang' as top result, got: %s", results[0].Key)
	}
}

func TestVectorMemoryRetrieveEmpty(t *testing.T) {
	mem := memory.NewVectorMemory()
	ctx := context.Background()

	results, err := mem.Retrieve(ctx, "test", 5)
	if err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for empty memory, got: %v", results)
	}
}

func TestVectorMemoryRetrieveTopK(t *testing.T) {
	mem := memory.NewVectorMemory()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		mem.Store(ctx, &types.MemoryEntry{
			Key:     "entry",
			Kind:    "short_term",
			Content: "这是一条关于 Redis 缓存的记忆条目",
		})
	}

	results, err := mem.Retrieve(ctx, "Redis 缓存", 3)
	if err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}
	if len(results) > 3 {
		t.Fatalf("expected at most 3 results, got %d", len(results))
	}
}

func TestVectorMemorySimilarityRanking(t *testing.T) {
	mem := memory.NewVectorMemory()
	ctx := context.Background()

	mem.Store(ctx, &types.MemoryEntry{
		Key:     "exact",
		Kind:    "long_term",
		Content: "TCP 三次握手是建立连接的过程",
	})
	mem.Store(ctx, &types.MemoryEntry{
		Key:     "partial",
		Kind:    "long_term",
		Content: "UDP 是无连接的传输协议",
	})
	mem.Store(ctx, &types.MemoryEntry{
		Key:     "unrelated",
		Kind:    "long_term",
		Content: "今天天气很好适合出门",
	})

	results, err := mem.Retrieve(ctx, "TCP 三次握手连接", 3)
	if err != nil {
		t.Fatalf("retrieve failed: %v", err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}

	// 最相关的应该是 TCP 相关的
	if results[0].Key != "exact" {
		t.Fatalf("expected 'exact' as top result, got: %s", results[0].Key)
	}
}

func TestVectorMemoryConcurrent(t *testing.T) {
	mem := memory.NewVectorMemory()
	ctx := context.Background()

	// 并发写入
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			mem.Store(ctx, &types.MemoryEntry{
				Key:     "concurrent",
				Kind:    "short_term",
				Content: "并发写入测试条目",
			})
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	// 并发读取
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			mem.Retrieve(ctx, "测试", 5)
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestNoopMemory(t *testing.T) {
	mem := memory.NewNoop()
	ctx := context.Background()

	if err := mem.Store(ctx, &types.MemoryEntry{Key: "test", Content: "test"}); err != nil {
		t.Fatalf("noop store should not error: %v", err)
	}

	results, err := mem.Retrieve(ctx, "test", 5)
	if err != nil {
		t.Fatalf("noop retrieve should not error: %v", err)
	}
	if results != nil {
		t.Fatal("noop retrieve should return nil")
	}
}
