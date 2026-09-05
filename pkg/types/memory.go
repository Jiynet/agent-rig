package types

// MemoryEntry 是一条待写入记忆的记录。
type MemoryEntry struct {
	Key      string
	Content  string
	Kind     string // short_term | long_term
	Metadata map[string]any
}
