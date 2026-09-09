package memory

import (
	"context"
	"log"

	"ai-cli-agent/pkg/types"
)

// SimpleMemory 简化实现：仅打印/记录记忆条目。
// 后续可替换为持久化存储（sqlite、向量库等）。
type SimpleMemory struct {
	logger *log.Logger
}

// NewSimpleMemory 创建简化记忆实现。
func NewSimpleMemory() *SimpleMemory {
	return &SimpleMemory{logger: log.Default()}
}

// Store 记录一条记忆。
func (m *SimpleMemory) Store(_ context.Context, entry *types.MemoryEntry) error {
	m.logger.Printf("[memory] kind=%s key=%s content=%s", entry.Kind, entry.Key, entry.Content)
	return nil
}

// Retrieve 简化实现不支持检索。
func (m *SimpleMemory) Retrieve(_ context.Context, _ string, _ int) ([]*types.MemoryEntry, error) {
	return nil, nil
}
