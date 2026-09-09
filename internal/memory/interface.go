// Package memory 提供 Agent 的记忆系统。
// 支持短期记忆写入与基于向量相似度的长期记忆检索。
package memory

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// Memory 是记忆系统的抽象接口。
type Memory interface {
	// Store 写入一条记忆。
	Store(ctx context.Context, entry *types.MemoryEntry) error
	// Retrieve 根据查询文本检索最相关的 topK 条记忆。
	Retrieve(ctx context.Context, query string, topK int) ([]*types.MemoryEntry, error)
}
