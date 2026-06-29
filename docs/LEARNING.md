# nanocode 学习文档

这份文档记录开发过程中各个功能是怎么实现的。README 负责路线图和状态列表，这里负责解释代码脉络和关键取舍。

## 1. 阶段 0：骨架与单轮流式对话

入口在 `cmd/nanocode/main.go`。启动时先读取命令行参数，再通过 `internal/config` 加载模型配置，最后创建 runner 并把模型流式输出写到终端。

模型配置支持项目根目录 `settings.json` 和环境变量覆盖。真实 `settings.json` 被 `.gitignore` 忽略，`settings-example.json` 只作为模板提交。

## 2. 阶段 1：显式 Agent 核心循环

核心逻辑在 `internal/agent/loop.go`。nanocode 没有把主循环藏进黑盒 ReAct agent，而是显式维护：

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

`read_file` 每次直接读取磁盘最新内容，不缓存、不索引、不预处理。默认读取窗口有限，模型可以用 `offset` 和 `limit` 分段读取。

## 3. 阶段 1.5：模式与工作区边界

项目支持三种模式：

- `default`
- `plan`
- `accept-edits`

`internal/permission/workspace.go` 负责工作区路径边界。文件工具接收的路径会解析到当前 workspace root 内；如果试图逃出工作区，会直接报错。

`/mode <name>` 不只是修改提示符，而是重建 runner。因为 system prompt、权限策略、工具暴露列表都依赖当前模式。

## 4. 阶段 2：搜索工具与 git 状态

`grep` 保持工具名和输入输出契约稳定，底层优先调用 `rg --json`，本机没有 `rg` 时 fallback 到纯 Go 实现。

`glob` 按文件名 / 路径 pattern 查找文件，支持 `**`，结果按修改时间倒序，最多返回 100 条。

`git_status` 只执行只读 git 命令，让 Agent 动手前知道当前分支、未提交改动和 diff 摘要。

## 5. 阶段 3：写操作与权限闸门

工具按权限分为：

- `read`
- `write`
- `shell`

权限策略集中在 `internal/permission/policy.go`：

- `plan`：拒绝写操作和 shell，并且不向模型暴露这些工具
- `default`：写操作和 shell 执行前询问
- `accept-edits`：普通文件写入自动允许，shell 仍需询问

Agent loop 在执行工具前构造 permission request，再按当前 mode 判断 `allow`、`deny` 或 `ask`。`ask` 会交给 REPL 的确认器展示风险并等待用户输入。

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

runner 增加：

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

session 解决“当前会话记住刚才说过什么”。memory 解决“长期规则每次启动都生效”。

MVP 有两个指令记忆来源：

- 用户级：`~/.nanocode/NANO.md`
- 项目级：仓库根目录 `NANO.md`

`internal/memory` 提供 loader。runner 创建 workspace policy 后，会创建 loader 并读取记忆文件。文件不存在时静默跳过；读取失败时报错；内容过长时会截断并追加提示。

加载顺序固定为：

1. 用户级记忆
2. 项目级记忆

项目级记忆放在后面，是为了让当前仓库规则更靠近当前任务上下文。

记忆通过 system prompt 注入。它是长期指令，不是工具结果，也不是普通会话消息。

## 9. 阶段 6.1：终端输出结构化

阶段 6.1 新增 `Reporter`，只负责写终端，不写入 `messages[]`。

Reporter 事件包括：

- `ToolStart`
- `ToolFinish`
- `ToolDenied`
- `ToolError`
- `SystemError`

这样用户能看到“正在调用工具”“工具完成”“权限拒绝”等状态，但模型上下文仍然只包含 assistant message 和 tool result。

## 10. 阶段 6.2：工具调用可视化

`ToolStart(...)` 返回开始时间，`invokeTool(...)` 在工具完成或失败后把开始时间交回 reporter，用来展示耗时。

工具专属摘要集中在 `internal/agent/reporter.go`，从现有工具 result 文本解析：

- `read_file`：读取文件和行范围
- `grep`：命中数量
- `glob`：返回文件数量
- `write_file`：路径和写入 bytes
- `edit_file`：路径和替换次数
- `bash`：输出行数 / 字符数或无输出

这些摘要只服务终端展示，不改变工具返回契约。

## 11. 阶段 6.3：diff 展示

阶段 6.3 让写操作在终端里更可检查：确认前能看到完整 diff，执行成功后能看到简短 `+N -M` 摘要。

`internal/diffview` 负责生成 unified diff。`write_file` 和 `edit_file` 没有改变工具输入输出契约，而是在工具注册元信息里挂了 `Preview` 函数。

Preview 只读取磁盘并模拟变化，不写文件、不创建 checkpoint。Agent loop 在权限请求阶段调用 preview，并把结果放进 `permission.Request.Preview`。这份 preview 只给 REPL 确认提示和 reporter 使用，不进入 `messages[]`。

## 12. 阶段 6.4：运行状态统计

`TurnStats` 只存在于当前 `RunWithMessages(...)` 调用内，不保存到 session，也不进入 `messages[]`。

统计点放在显式 Agent loop 中：

- 每次调用模型前记录一次模型调用
- 每个 tool call 记录一次工具调用
- 权限拒绝记录拒绝
- 工具执行失败记录失败
- `write_file` / `edit_file` 成功后记录写入文件数
- `bash` 通过权限并开始执行时记录 bash 次数

尾部输出由 `writeTurnFooter(...)` 统一生成，先取 context usage，再拼接 turn stats。

## 13. 阶段 6.5：错误体验优化

`internal/friendlyerror` 把底层错误包装成稳定的中文终端提示，包含标题、详情、建议和原始 cause。

错误来源主要包括：

- `internal/config` 的 typed error
- workspace 越界 typed error
- 模型请求错误
- 常见工具错误
- 权限拒绝原因

CLI 启动阶段不再使用 `log.Fatal`，而是通过 `fatal(err)` 输出 `friendlyerror.Format(err)`。

## 14. 阶段 6.6：REPL 命令增强

REPL 新增 `LineReader` 抽象：

```go
type LineReader interface {
    ReadLine(prompt string) (string, error)
}
```

真实终端使用 `github.com/peterh/liner` 支持输入历史和 Tab 补全，测试继续使用普通 reader。

命令列表集中在 `internal/ui/commands.go`。`/help`、输入单独 `/` 的提示、Tab 补全都复用同一份注册表，避免列表漂移。

`/clear` 清空当前 session 的 `Messages`，不创建新 session，也不删除 session 文件。下一轮用户输入时，runner 会重新注入最新 system prompt。

`/reload` 重建 runner，重新读取配置和 memory。成功后替换 runner 和 system prompt；失败时保留旧 runner、旧 model label 和旧 session。

REPL 内也支持：

- `/resume <session-id>`
- `/continue`

## 15. 阶段 6.7：交互细节打磨

阶段 6.7 继续优化 REPL 的终端可用性，不改变 Agent loop、工具契约、权限策略或 session 存储格式。

### 15.1 权限确认输入

`Confirm(...)` 从一次读取 `[y/N]` 改成循环读取，直到得到明确允许或拒绝。

支持输入：

- `y` / `yes`：允许一次
- 空输入 / `n` / `no`：拒绝
- `q` / `quit` / `cancel`：拒绝
- `p` / `preview`：重新显示 diff preview
- `a` / `args`：重新显示工具参数
- `h` / `help` / `?`：显示帮助

EOF 默认拒绝。这里没有实现永久允许或本会话总是允许，因此不改变权限策略。

### 15.2 `/mode` 输出增强

`/mode` 现在展示当前 mode、model 和工具摘要。

`/mode <name>` 成功后展示：

```text
已切换模式：default -> plan
```

如果目标模式与当前模式相同，直接提示“已在该模式”，不重建 runner。切换失败时旧 runner、旧 session 和旧 mode 都保持不变。

### 15.3 `/memory` 摘要化

`/memory` 默认只展示每个 source 的路径、状态、字符数和摘要。摘要按 rune 截断，默认上限为 1200。

额外支持：

- `/memory full`：显示完整内容
- `/memory status`：只显示来源状态，不显示内容

被截断时会提示可用 `/memory full` 查看完整内容。

### 15.4 `/session` 元信息

`/session` 显示：

- session id
- mode
- model
- workspace root
- messages 数量
- created time
- updated time
- session file path

时间使用本地时区格式：`2006-01-02 15:04:05`。

### 15.5 欢迎页

启动 REPL 时欢迎页会显示：

- model
- mode
- workspace
- session id
- messages 数量
- memory 加载状态

欢迎页只展示摘要，不打印完整 memory 或 messages。

## 16. 当前测试覆盖

常用验证命令：

```powershell
go test ./...
go build ./...
```

当前测试覆盖包括：

- 配置文件与环境变量加载
- workspace 路径边界
- 只读工具、搜索工具、git 状态工具
- 权限策略矩阵
- 写操作、bash、checkpoint、undo
- 上下文估算、截断、压缩和 footer
- session 创建、保存、加载、继续和恢复
- memory 加载、顺序、截断和 prompt 注入
- reporter 结构化输出、工具摘要、diff 摘要
- friendly error 分类和格式化
- REPL 命令、补全、状态展示、确认交互、memory 摘要、session 元信息

## 17. 后续维护约定

1. README 只维护路线图和状态列表。
2. 具体实现细节写在本学习文档。
3. 新增核心功能后，补充“为什么这样做”和“代码怎么流动”。
4. 中文注释和文档统一 UTF-8。
