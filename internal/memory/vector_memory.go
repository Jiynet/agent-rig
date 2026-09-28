package memory

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"

	"ai-cli-agent/pkg/types"
)

// VectorMemory 基于字符 n-gram 向量化和余弦相似度的内存记忆实现。
// 不依赖外部向量库，适合 demo 和小规模记忆场景。
type VectorMemory struct {
	mu        sync.RWMutex
	entries   []*memoryRecord
	ngramSize int // n-gram 大小，默认 3
}

type memoryRecord struct {
	entry  *types.MemoryEntry
	vector map[string]int // n-gram 频次向量
}

// NewVectorMemory 创建向量记忆实例。
func NewVectorMemory() *VectorMemory {
	return &VectorMemory{
		entries:   make([]*memoryRecord, 0),
		ngramSize: 3,
	}
}

// Store 写入一条记忆，自动计算 n-gram 向量。
func (m *VectorMemory) Store(_ context.Context, entry *types.MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	vector := m.textToVector(entry.Content)
	m.entries = append(m.entries, &memoryRecord{
		entry:  entry,
		vector: vector,
	})
	return nil
}

// Retrieve 根据查询文本检索最相关的 topK 条记忆。
// 使用余弦相似度衡量相关性。
func (m *VectorMemory) Retrieve(_ context.Context, query string, topK int) ([]*types.MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.entries) == 0 || topK <= 0 {
		return nil, nil
	}

	queryVec := m.textToVector(query)
	if len(queryVec) == 0 {
		return nil, nil
	}

	// 计算每条记忆与查询的相似度
	type scored struct {
		entry *types.MemoryEntry
		score float64
	}
	scores := make([]scored, 0, len(m.entries))
	for _, rec := range m.entries {
		sim := cosineSimilarity(queryVec, rec.vector)
		if sim > 0 {
			scores = append(scores, scored{entry: rec.entry, score: sim})
		}
	}

	// 按相似度降序排序
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// 取 topK
	if topK > len(scores) {
		topK = len(scores)
	}
	result := make([]*types.MemoryEntry, 0, topK)
	for i := 0; i < topK; i++ {
		result = append(result, scores[i].entry)
	}
	return result, nil
}

// textToVector 将文本转为 n-gram 频次向量。
func (m *VectorMemory) textToVector(text string) map[string]int {
	text = strings.ToLower(text)
	text = strings.TrimSpace(text)
	if len(text) < m.ngramSize {
		// 文本太短，直接用整个文本作为 key
		return map[string]int{text: 1}
	}

	vec := make(map[string]int)
	runes := []rune(text)
	for i := 0; i <= len(runes)-m.ngramSize; i++ {
		ngram := string(runes[i : i+m.ngramSize])
		vec[ngram]++
	}
	return vec
}

// cosineSimilarity 计算两个向量的余弦相似度。
func cosineSimilarity(a, b map[string]int) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64

	for k, va := range a {
		vb := b[k]
		dotProduct += float64(va) * float64(vb)
		normA += float64(va) * float64(va)
	}
	for _, vb := range b {
		normB += float64(vb) * float64(vb)
	}

	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
