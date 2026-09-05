package perception

import (
	"encoding/json"
	"fmt"
)

// prettyJSON 将任意值格式化为缩进 JSON 文本，供展示与调试使用。
func prettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("<unable to encode: %v>", err)
	}
	return string(b)
}

// ANSI 颜色常量，终端高亮输出使用。
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

// colorize 给文本加上颜色（仅在非空文本时生效）。
func colorize(text, color string) string {
	if text == "" {
		return text
	}
	return color + text + colorReset
}
