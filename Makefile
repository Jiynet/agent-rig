# ai-cli-agent 常用开发命令
GO       ?= go
BINARY   := bin/agent
PKG      := ./...

.PHONY: build run test test-race eval vet fmt lint clean

build: ## 构建二进制到 $(BINARY)
	$(GO) build -o $(BINARY) ./cmd/agent

run: ## 运行 agent（读取 configs/config.local.yaml）
	$(GO) run ./cmd/agent -config configs/config.local.yaml

test: ## 运行全部测试
	$(GO) test $(PKG)

test-race: ## 运行全部测试（含竞态检测）
	$(GO) test -race $(PKG)

eval: ## 离线评测：全量历史 vs 预算化上下文
	$(GO) run ./cmd/eval

vet: ## 静态检查
	$(GO) vet $(PKG)

fmt: ## 格式化源码
	gofmt -w $$(find . -name '*.go' -not -path './.idea/*')

lint: ## golangci-lint（需先安装: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest）
	golangci-lint run

clean: ## 清理构建产物
	rm -rf bin coverage.out

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'
