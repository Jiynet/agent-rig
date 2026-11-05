// eval 运行确定性的 Agent 评测，对比“全量历史”和“预算化上下文”两种策略。
//
// 使用方式：
//
//	go run ./cmd/eval                    # 两种模式都跑
//	go run ./cmd/eval -mode assembled    # 只跑上下文组装
//	go run ./cmd/eval -budget 800        # 调整组装预算
//	go run ./cmd/eval -json              # JSON 输出
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"ai-cli-agent/internal/eval"
)

func main() {
	modeFlag := flag.String("mode", "both", "full | assembled | both")
	budget := flag.Int("budget", 0, "assembled 模式的提示词 token 预算（0 表示使用默认值）")
	jsonOut := flag.Bool("json", false, "以 JSON 输出结果")
	flag.Parse()

	cfg := eval.DefaultConfig()
	if *budget > 0 {
		cfg.Context.MaxPromptTokens = *budget
	}

	modes, err := parseModes(*modeFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "参数错误: %v\n", err)
		os.Exit(2)
	}

	results := eval.RunAll(eval.DefaultTasks(), modes, cfg)
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintf(os.Stderr, "输出 JSON 失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	eval.PrintTable(os.Stdout, results)
}

func parseModes(raw string) ([]eval.Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "full":
		return []eval.Mode{eval.ModeFull}, nil
	case "assembled", "asm":
		return []eval.Mode{eval.ModeAssembled}, nil
	case "both", "":
		return []eval.Mode{eval.ModeFull, eval.ModeAssembled}, nil
	default:
		return nil, fmt.Errorf("未知模式 %q（可选 full / assembled / both）", raw)
	}
}
