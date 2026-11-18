# ai-cli-agent

基于 **感知 → 规划 → 执行** 四层架构的 AI 命令行 Agent 框架，Go 语言实现。除配置解析使用 `yaml.v3` 外，全部基于标准库。

## 架构设计

```
用户输入
   │
   ▼
┌─────────────────────────────────────────────────────────────┐
│ 感知层 perception          输入解析 / 用户确认 / 输出格式化      │
└─────────────────────────────────────────────────────────────┘
   │ Intent
   ▼
┌─────────────────────────────────────────────────────────────┐
│ 规划层 planning           ReAct 循环（推理→工具→观察→再推理）    │
│   ├── 短期对话历史维护      TaskState.History                   │
│   ├── 记忆上下文注入        VectorMemory 语义检索                │
│   └── 全链路追踪            Span 级耗时/Token/工具指标            │
└─────────────────────────────────────────────────────────────┘
   │ Action                    ▲
   ▼                            │ ActionResult
┌─────────────────────────────────────────────────────────────┐
│ 执行层 execution           风险分级确认网关（不直接调用工具）      │
└─────────────────────────────────────────────────────────────┘
   │ JudgmentResult
   ▼
┌─────────────────────────────────────────────────────────────┐
│ 工具层 tools               注册 / 分发 / 并发执行 / 超时重试     │
│   └── builtin              shell / file_read / file_write /   │
│                            web_search / http_request          │
└─────────────────────────────────────────────────────────────┘
   基础设施：llm（OpenAI 兼容）/ memory（向量检索）/ context（预算化组装）/ tracing / eventbus
```

## 核心技术亮点

### 1. ReAct 推理循环

规划器驱动完整的 ReAct 循环：LLM 推理 → Function Calling → 工具执行 → 结果写回对话历史 → LLM 继续推理，直到给出最终回答或达到迭代上限。

```go
// 核心循环（internal/agent/run.go）
for {
    action := planner.SelectNextAction(ctx, state)  // LLM 推理
    judgment := planner.DispatchAction(ctx, action)  // 风险确认
    result := tools.Execute(ctx, call)               // 工具执行
    state = planner.UpdateState(ctx, state, result)  // 状态推进
    if done := planner.EvaluateCompletion(ctx, state) { break }
}
```

### 2. 并发工具执行

当 LLM 一轮返回多个 `tool_calls` 时，通过 `ConcurrentExecutor` 并发执行：

- **信号量控制并发数**（可配置 `max_concurrency`）
- **单工具超时**（`context.WithTimeout`）
- **指数退避重试**（可配置 `max_retries`）
- **`sync.WaitGroup` 管理 goroutine 生命周期**（有意不引入 `x/sync`）

### 3. MCP 协议支持

内置 MCP (Model Context Protocol) Server，通过 stdio 传输暴露工具给外部客户端：

```bash
# 直接调用
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | ./bin/mcp-server

# Claude Desktop 配置
{
  "mcpServers": {
    "ai-cli-agent": {
      "command": "/path/to/bin/mcp-server"
    }
  }
}
```

### 4. 向量记忆系统

基于字符 n-gram 向量化和余弦相似度的内存检索，无需外部向量库：

- `Store` 写入记忆，自动计算 n-gram 频次向量
- `Retrieve` 按语义相似度检索 top-K 相关记忆
- 检索结果自动注入 ReAct 循环的系统提示

### 5. 全链路追踪

`ConsoleTracer` 记录每个执行步骤的结构化 Span：

```
✓ agent.run [1200ms] llm_calls=3 tool_calls=2 total_tokens=1500
  ✓ perception.parse_input [1ms]
  ✓ plan.create [0ms]
  ✓ react.iteration [800ms] action_type=tool_call tool_name=shell
    ✓ execution.confirm [0ms]
    ✓ tool.execute [50ms]
    ✓ llm.generate [700ms] model=gpt-4o total_tokens=800
  ✓ react.iteration [400ms] action_type=respond
    ✓ llm.generate [400ms] model=gpt-4o total_tokens=700
  ✓ plan.synthesize [0ms]
  ✓ memory.store [0ms]
```

### 6. 风险分级确认

按工具类型自动分配风险等级，中高风险操作需用户确认：

| 工具 | 风险等级 | 策略 |
|------|---------|------|
| shell | Medium | 需用户确认 |
| file_write | Medium | 需用户确认 |
| http_request | Low | 自动放行 |
| file_read / web_search | Low | 自动放行 |

### 7. 预算化上下文管理

`internal/context` 在发送给 LLM 前组装上下文，避免 ReAct 循环中历史无限增长：

- **分区保留**：system / 当前目标 / 最近消息 / 工具观察 / 旧轮次摘要
- **工具输出截断**：单条观察按头尾截断，最新观察优先保留
- **配对保护**：`assistant(tool_calls)` 与 `tool(tool_call_id)` 作为不可拆分单元，裁剪不会产生孤儿消息
- **旧轮次摘要**：超出预算的早期消息压缩为结构化摘要，并插回原位置
- **可观测**：每次 LLM 调用记录 `context_tokens / context_original_tokens / summarized / truncated`

配置见 `configs/config.yaml` 的 `context` 段；`max_prompt_tokens: -1` 可关闭组装，退回全量历史透传。

### 8. 离线评测

`cmd/eval` 用 scripted LLM 运行确定性任务，对比全量历史与预算化上下文：

```bash
go run ./cmd/eval              # full vs assembled
go run ./cmd/eval -budget 500  # 调整组装预算
go run ./cmd/eval -json        # JSON 输出
```

评测覆盖直接回答、单工具、多工具并发、长工具输出、多轮上下文、迭代上限、用户拒绝、记忆注入，并校验 `tool_calls/tool_call_id` 配对完整性。

## 工具集

| 工具 | 功能 | 风险 |
|------|------|------|
| `shell` | 执行终端命令 | Medium |
| `file_read` | 读取本地文件 | Low |
| `file_write` | 写入本地文件（含路径安全校验） | Medium |
| `web_search` | 网络搜索（DuckDuckGo API，无需 Key） | Low |
| `http_request` | HTTP GET/POST 请求 | Low |

## 快速开始

```bash
# 构建
make build

# 配置模型
export OPENAI_API_KEY=sk-xxx

# 交互模式
./bin/agent -config configs/config.yaml

# MCP Server 模式
./bin/mcp-server

# 运行演示（无需 API Key，使用 scripted LLM）
go run ./cmd/demo
```

### 配置文件

复制 `configs/config.yaml` 为 `configs/config.local.yaml`（已被 gitignore 忽略），按需修改。

## 目录结构

```
.
├── cmd/
│   ├── agent/                # 交互模式入口
│   ├── mcp-server/           # MCP Server 入口
│   ├── demo/                 # 端到端演示
│   └── eval/                 # 离线评测入口
├── internal/
│   ├── agent/                # 编排层：主控制循环 + 并发执行
│   ├── perception/           # 感知层：输入解析 / 用户确认
│   ├── planning/             # 规划层：ReAct 循环 / 状态管理
│   ├── execution/            # 执行层：风险分级确认
│   ├── llm/                  # 模型层：OpenAI 兼容客户端
│   ├── context/              # 上下文：预算化组装 / 截断 / 摘要
│   ├── tools/                # 工具层：注册 / 分发 / 并发执行
│   │   └── builtin/          # 内置工具：shell / file / search / http
│   ├── memory/               # 记忆：向量检索
│   ├── mcp/                  # MCP 协议：Server / Transport / Protocol
│   ├── tracing/              # 追踪：Span / Metrics / ConsoleTracer
│   ├── eval/                 # 评测：确定性任务 / 配对校验 / 指标
│   └── eventbus/             # 事件总线
├── pkg/types/                # 共享数据结构
└── configs/                  # 配置文件
```

## 开发

```bash
make fmt     # gofmt 格式化
make vet     # go vet 静态检查
make lint    # golangci-lint
make test    # 运行全部测试
make test-race # 运行全部测试（含竞态检测）
make eval    # 离线评测：全量历史 vs 预算化上下文
```

## License

[MIT](LICENSE)
