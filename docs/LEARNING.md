# nanocode 学习文档

这份文档记录开发过程中各个功能是怎么实现的。README 负责路线图和状态列表，这里负责解释代码脉络和关键取舍。

## 1. 阶段 0：骨架与单轮流式对话

入口在 `cmd/nanocode/main.go`。启动时先读取命令行参数，再通过 `internal/config/config.go` 加载模型配置，最后创建 runner 并把模型流式输出写到终端。

模型配置支持项目根目录 `settings.json` 和环境变量覆盖。`settings.json` 是本地真实配置，已经被 `.gitignore` 忽略；`settings-example.json` 只作为模板提交。

## 2. 阶段 1：显式 Agent 核心循环

核心逻辑在 `internal/agent/loop.go`。当前没有把主循环藏进黑盒 ReAct agent，而是显式维护：

```text
用户输入
  -> messages[]
  -> model.Stream(...)
  -> assistant message
  -> 如果有 tool_calls，执行工具
  -> 追加 tool_result
  -> 回到 model.Stream(...)
  -> 没有 tool_calls 时结束
```

这样 `messages[]` 如何增长、工具结果如何回填、权限闸门和上下文管理插在哪里都很清楚。

`read_file` 每次都直接读取磁盘最新内容，不缓存、不索引、不预处理。默认读取 2000 行，可以用 `offset` 和 `limit` 分段读取。

## 3. 阶段 1.5：模式与工作区边界

项目支持三种模式：

- `default`
- `plan`
- `accept-edits`

`internal/permission/workspace.go` 负责工作区路径边界。文件工具接收的路径会解析到当前 workspace root 内；如果试图逃出工作区，会直接报错。

`/mode <name>` 不只是修改提示符，而是重建 runner。因为 system prompt、权限策略、工具暴露列表都依赖当前模式。

## 4. 阶段 2：搜索工具与 git 状态

`grep` 保持工具名和输入输出契约稳定，底层优先调用 `rg --json`，如果本机没有 `rg` 再 fallback 到纯 Go 实现。

`glob` 按文件名 / 路径 pattern 查找文件，支持 `**`，结果按修改时间倒序，最多返回 100 条，避免把上下文塞满。

`git_status` 只执行只读 git 命令：

```text
git status --short --branch
git diff --stat
git diff --cached --stat
```

它让 Agent 在动手前知道当前分支、未提交改动和 diff 摘要。

## 5. 阶段 3：写操作与权限闸门

工具按权限分为：

- `read`
- `write`
- `shell`

权限策略集中在 `internal/permission/policy.go`：

- `plan`：拒绝写操作和 shell，并且不向模型暴露这些工具
- `default`：写操作和 shell 执行前询问
- `accept-edits`：普通文件写入自动允许，shell 仍需询问

Agent loop 在执行工具前先构造 permission request，再按当前 mode 判断 `allow`、`deny` 或 `ask`。`ask` 会交给 REPL 的确认器展示风险并等待用户输入。

`write_file` 用于写入完整文件，`edit_file` 使用精确字符串替换。写入前会通过 checkpoint 记录最近一次文件状态，`/undo` 可以撤销最近一次成功写入。

`bash` 在 workspace root 下执行命令，默认需要确认，支持超时和输出截断。

## 6. 阶段 4：上下文管理

`internal/contextmgr` 负责上下文预算：

- `EstimateTokens(text)`
- `EstimateMessages(messages)`
- `TrimToolResult(toolName, result)`
- `CompactMessages(messages)`
- `FormatUsage(messages)`

当前 token 估算是 MVP：按 rune 数粗略折算。它不精确，但足够用来做预算保护。

每次调用模型前，Agent 会先检查 `messages[]` 是否接近预算；超过阈值时，用规则型 summary 压缩较早消息，保留 system message、当前 user message 和最近交互。

每次工具执行后，tool result 会统一经过 `TrimToolResult`，避免大输出无限膨胀。

回复结束时会追加上下文使用情况，例如：

```text
[已使用1%上下文/1M]
```

最大上下文窗口来自 `settings.json` 的 `context_window`，也可以用 `NANOCODE_CONTEXT_WINDOW` 覆盖。

## 7. 阶段 5：会话持久化

会话存储在 `.nanocode/sessions/<session-id>.json`，由 `internal/session/store.go` 管理。

阶段 5 后，runner 增加：

```go
RunWithMessages(ctx, history, userPrompt, output)
```

它会复用历史 messages，追加本轮 user message，执行原有工具循环，并返回更新后的 messages。REPL 每轮普通输入结束后把 messages 保存回 session 文件。

CLI 支持：

```powershell
go run ./cmd/nanocode --continue
go run ./cmd/nanocode --resume <session-id>
```

`-prompt` 单轮模式默认不保存 session，避免脚本调用产生隐式历史。

## 8. 阶段 5.5：记忆机制

阶段 5 的 session 解决“当前会话记住刚才说过什么”。阶段 5.5 的 memory 解决“长期规则每次启动都生效”。

### 8.1 记忆文件放在哪里

MVP 有两个指令记忆来源：

- 用户级：`~/.nanocode/NANO.md`
- 项目级：仓库根目录 `NANO.md`

用户级记忆适合写个人长期偏好。项目级记忆适合写当前仓库稳定规则，比如 README 只做 roadmap、实现说明写到 `docs/LEARNING.md`。

项目提供 `NANO-example.md` 作为模板，但不会自动创建真实 `NANO.md`，避免替用户写项目规则。

### 8.2 记忆如何加载

`internal/memory` 提供 `Loader`。runner 创建 workspace policy 后，会创建 loader 并读取记忆文件。

文件不存在时静默跳过；读取失败时返回错误，避免用户误以为记忆已经生效。内容过长时会按字符上限截断，并在内容末尾追加提示。

加载顺序固定为：

1. 用户级记忆
2. 项目级记忆

项目级记忆被放在后面，是为了让当前仓库规则更靠近当前任务上下文。

### 8.3 为什么注入 system prompt

记忆是“长期指令”，不是工具结果，也不是会话消息。把它注入 system prompt 可以保证模型每轮一开始就知道这些稳定约定。

`internal/prompt/system.go` 在有记忆时生成 `Memory:` 区块；没有记忆时不输出空区块，保持 prompt 简洁。

### 8.4 `/memory` 如何工作

REPL 中新增：

```text
/memory
```

它只读展示当前 runner 加载到的记忆来源和内容：

- 用户级记忆路径与 loaded / missing 状态
- 项目级记忆路径与 loaded / missing 状态
- 实际注入 prompt 的内容

阶段 5.5 不实现 `/memory add`、`/memory edit` 或模型自动写入记忆。

## 9. 当前测试覆盖

常用验证命令：

```powershell
go test ./...
go build ./...
```

当前测试覆盖包括：

- 配置文件与环境变量加载
- workspace 路径边界
- `read_file` 默认窗口与 offset / limit
- `grep`、`glob`、`git_status`
- 权限策略矩阵
- `write_file`、`edit_file`、`bash`
- checkpoint 与 `/undo`
- 上下文估算、工具输出截断、消息压缩
- 回复尾部上下文用量显示
- session 创建、保存、加载、`--continue`、`--resume`
- REPL `/mode`、`/session`、`/memory`
- memory 文件加载、顺序、截断、prompt 注入

## 10. 后续维护约定

1. README 只维护路线图和状态列表。
2. 具体实现细节写在本学习文档。
3. 新增核心功能后，补充“为什么这样做”和“代码怎么流动”。
4. 中文注释和文档统一 UTF-8。
