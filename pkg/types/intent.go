package types

// Intent 表示感知层解析后的用户意图。
type Intent struct {
	RawInput string // 原始输入
	Goal     string // 解析出的目标描述
	Context  string // 附加上下文说明
}
