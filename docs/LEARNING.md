# nanocode 学习文档

这份文档用来记录 `nanocode` 在开发过程中的功能实现方式。

它重点回答四类问题：

- 这个功能放在哪一层实现
- 一次请求是怎么流过这些模块的
- 为什么当前版本先这样做
- 以后扩展时，优先从哪里接入

当前文档先覆盖已经实现的内容：阶段 0 的最小单轮流式对话、阶段 1 的显式工具循环、阶段 1.5 的执行模式与工作区边界，以及阶段 2 接入的 `grep` / `glob` / `git_status` 工具。
此外，当前还补上了一个便于手动验证的终端 REPL 交互层。

---

## 1. 当前已经实现了什么

目前代码已经具备这些能力：

1. 从项目根 `settings.json` 和环境变量加载 OpenAI-compatible 模型配置
2. 启动一个最小 CLI
3. 接收单轮用户输入
4. 用 Eino 的模型与工具抽象实现显式 Agent 核心循环
5. 给 Agent 挂上 `read_file` 和 `ls` 两个只读工具
6. 让模型在单轮回答中自主决定是否先读文件/看目录
7. 以流式方式把最终回答输出到终端
8. 用测试保护工作区路径边界和只读工具的基础行为
9. 显式支持执行模式：`default`、`plan`、`accept-edits`
10. 用独立的 workspace policy 对象统一管理项目根目录边界
11. 提供可持续交互的终端 REPL，支持帮助、退出和模式切换命令
12. 提供最小可用的 `grep` 仓库文本搜索能力
13. 提供按文件名/路径模式查找文件的 `glob` 能力
14. 提供只读 git 状态感知能力：当前分支、未提交改动、diff 摘要

换句话说，当前不是“普通聊天程序”，而是一个最小可运行的、带只读工具的编码 Agent。

---

## 2. 整体执行链路

对应文件：

- `cmd/nanocode/main.go`
- `internal/config/config.go`
- `internal/ui/repl.go`
- `internal/agent/loop.go`

一次请求现在的执行路径是：

1. `main.go` 解析命令行参数
2. `config.Load()` 读取模型配置
3. 如果没有传 `-prompt`，就通过 `ui.ReadSinglePrompt()` 从终端读一行输入
4. `agent.NewSingleTurnRunner()` 组装运行器
5. Runner 内部创建模型、注册工具、绑定工具 schema
6. `runner.Run()` 把用户输入交给 Agent
7. 显式核心循环判断模型是否产生 tool call
8. 工具执行结果回灌给模型
9. 最终回答写回终端

如果没有传 `-prompt`，则会进入 REPL：

1. 终端展示当前模式和帮助提示
2. 用户输入普通文本时，按“一次输入一轮 Agent”执行
3. 用户输入 `/mode plan` 这类命令时，交给 UI 层处理
4. 如果模式切换成功，会重建对应模式下的 runner

这个结构的核心思想是：入口层只负责“接请求”，而不直接关心模型、工具和 Agent 编排细节。

---

## 3. 入口层是怎么实现的

对应文件：

- `cmd/nanocode/main.go`
- `internal/ui/repl.go`

### 3.1 `main.go` 的职责

`main.go` 很克制，只做三件事：

1. 解析 `-prompt`
2. 加载配置
3. 调用 runner 执行

它不直接创建模型，也不直接碰工具。

这样拆的好处是：

- 入口不会随着 Agent 复杂度增加而膨胀
- 后面如果要换成 REPL、多轮会话、HTTP API，都可以复用 runner 和底层模块

### 3.2 `ui.ReadSinglePrompt()` 的职责

阶段 0 / 阶段 1 还没有多轮历史，所以 `ReadSinglePrompt()` 只读取一行输入。

这个函数单独放在 `internal/ui`，是为了把“终端交互”从“Agent 逻辑”里剥出来。后续如果换更复杂的终端 UI，这一层可以替换，但不需要重写 Agent 本身。

### 3.3 当前 REPL 是怎么实现的

对应文件：

- `internal/ui/repl.go`
- `cmd/nanocode/main.go`

现在入口层已经支持两种运行方式：

1. 传 `-prompt`：走单轮调用，适合脚本和快速验证
2. 不传 `-prompt`：进入终端 REPL，适合连续手动测试

REPL 当前只做终端交互层应该做的事情：

- 展示欢迎信息
- 展示当前加载的模型信息
- 读取一行输入
- 识别内建命令
- 把普通输入转交给 runner 执行
- 输出每轮的流式结果

当前支持的内建命令有：

- `/help`
- `/mode`
- `/mode <name>`
- `/exit`

这里有一个很重要的实现点：`/mode` 不只是修改界面上显示的模式名，而是会通过 runner factory 重新创建一个新的 runner。

这样 `plan` / `default` / `accept-edits` 的切换才是真正生效的，而不是“看起来切了，实际还按旧模式在跑”。

---

## 4. 配置层是怎么实现的

对应文件：

- `internal/config/config.go`

### 4.1 为什么单独做配置模块

模型配置读取如果散落在 `main.go`、`model`、`agent` 里，后面会很难维护。

单独抽成 `config.LoadFromEnv()` 之后：

- 调用侧只拿结构化配置
- provider 层不用自己反复读环境变量
- 后面改成配置文件或命令行参数覆盖时，更容易演进

### 4.2 当前配置策略

当前配置来源分成两层：

1. 项目根 `settings.json`
2. 环境变量覆盖

其中环境变量优先级更高。

这套设计是为了同时满足三件事：

1. 项目本身可以带一份清晰的模型配置入口
2. 本机仍然可以用环境变量临时覆盖
3. 兼容 Claude Code 一类工具常见的“项目配置文件”使用习惯

当前优先读取：

- `NANOCODE_*`

同时兼容：

- `OPENAI_*`

这是为了既保留项目自己的命名空间，也允许直接复用已有的 OpenAI-compatible 环境变量。

### 4.3 `settings.json` 当前支持什么字段

对应文件：

- `internal/config/config.go`
- `settings-example.json`

当前 `settings.json` 支持的 `model` 字段有：

- `base_url`
- `api_key`
- `api_key_env`
- `model`
- `temperature`
- `max_tokens`

其中 `api_key` 和 `api_key_env` 二选一都可以：

- `api_key`：直接把真实 key 放在本地 `settings.json`
- `api_key_env`：在配置文件里写环境变量名，真实值仍从环境变量读取

项目根的 `settings.json` 已经被 `.gitignore` 忽略，所以允许本地保存真实 key；而 `settings-example.json` 负责给仓库提供模板。

### 4.4 为什么 `model` 必须显式指定

当前实现对 `model` 不做隐式兜底。

原因很简单：模型名一旦猜错，问题不是“不能跑”，而是“跑到了错误的模型”，这会让后续调试非常混乱，所以这里宁愿尽早失败。

---

## 5. 模型层是怎么实现的

对应文件：

- `internal/model/factory.go`

### 5.1 `factory.go` 的作用

这一层专门负责把 `config.ModelConfig` 转成 Eino 的模型实例。

它的边界很明确：

- 配置从上层来
- 具体 provider 的接法在这里落地
- 上层不需要知道 OpenAI-compatible 的细节

这意味着后续如果支持：

- Azure OpenAI
- 本地兼容网关
- 其他 provider

优先修改点就是这里。

### 5.2 `NewChatModel()` 做了什么

`NewChatModel()` 把项目自己的配置结构，转换成 Eino OpenAI 模型需要的 `ChatModelConfig`。

这个函数解决的是“能否正常聊天和流式输出”的问题。

### 5.3 `NewToolCallingChatModel()` 为什么存在

ReAct Agent 不是只要“能聊天”就够了，它还要求模型支持 tool calling。

所以这里单独做了一层断言：

- `NewChatModel()`：返回通用 `BaseChatModel`
- `NewToolCallingChatModel()`：要求它同时实现 `ToolCallingChatModel`

这样接口意图更清晰，也能更早暴露 provider 能力不匹配的问题。

---

## 6. `loop.go` 是怎么从阶段 0 演进到阶段 1 的

对应文件：

- `internal/agent/loop.go`

### 6.1 阶段 0：最小单轮流式对话

阶段 0 时，`loop.go` 做的是最小闭环：

1. 构造 `system + user` 消息
2. 直接调用 chat model 的 `Stream(...)`
3. 循环 `Recv()` 拿增量分片
4. 把分片写到终端

这一版的目标不是“智能”，而是打通：

`用户输入 -> 模型调用 -> 流式输出`

只要这个闭环通了，后面才值得往中间插 Agent 和工具。

### 6.2 为什么 `loop.go` 仍然保留这个名字

虽然现在还是单轮，但它已经承担了“请求进入后如何执行”的核心职责，所以它天然就是后续 Agent 主循环的落点。

换句话说，`loop.go` 不是“某一轮模型调用”，而是“这个程序怎样跑完一轮任务”。

### 6.3 阶段 1：升级成显式工具循环

阶段 1 后，`loop.go` 不再只是“调用一次模型”，而是显式维护一条核心循环：

```text
messages[] -> model.Stream -> 合并 assistant message -> 是否有 tool_calls
                                                     ├─ 没有：输出最终文本
                                                     └─ 有：执行工具 -> 追加 tool message -> 回到 messages[]
```

之前阶段曾短暂使用 Eino 的 `react.NewAgent(...)` 托管这条循环。

现在项目把这条循环显式写在 `internal/agent/loop.go` 里，原因是后续要接：

- 权限闸门
- 工具执行前确认
- checkpoint
- 工具输出治理
- resume

也就是说，现在的主流程是：

`用户输入 -> messages[] -> 模型 -> 工具调用 -> tool_result -> messages[] -> 最终回答`

### 6.4 `NewSingleTurnRunner()` 现在负责什么

当前它主要负责四件事：

1. 创建 tool-calling model
2. 获取当前工作区路径
3. 注册当前阶段可用工具
4. 收集工具 schema
5. 建立工具名到工具实现的映射
6. 生成当前模式下的 system prompt

这样 `main.go` 完全不需要知道：

- 工具有哪些
- 工具 schema 怎么绑定
- tool call 怎么执行
- system prompt 怎么注入

这些实现细节都封装在 runner 里。

### 6.5 `Run()` 现在负责什么

现在的 `Run()` 是当前项目的核心循环落点：

1. 初始化 `system + user` 消息
2. 调用 `model.Stream(...)`
3. 收到文本 chunk 后立即写回终端
4. 同时收集所有 chunk，合并成完整 assistant message
5. 把 assistant 消息追加到 `messages`
6. 如果没有 `ToolCalls`，结束
7. 如果有 `ToolCalls`，逐个执行工具
8. 把工具结果包装成 `schema.ToolMessage(...)`
9. 追加到 `messages`
10. 回到第 2 步继续循环

这一版和 Claude Code 的核心形态更接近，因为 messages 的增长和 tool result 回灌都在项目代码里可见。

### 6.6 流式输出怎么和工具循环同时存在

当前显式核心循环使用 `model.Stream(...)`。

每一步模型调用时，`streamAssistantMessage(...)` 会做两件事：

1. 收到文本 chunk 后立刻写回终端
2. 同时收集所有 chunk，流结束后用 `schema.ConcatMessages(...)` 合并成完整 assistant message

这样做的原因是：

- 用户能尽快看到模型输出
- 循环结束后仍然能拿到完整的 `ToolCalls`
- 工具调用结果还能继续追加为 `ToolMessage`

所以当前不是“为了工具调用放弃流式”，而是：

```text
Stream chunks -> 终端即时输出
              -> 合并 assistant message
              -> 判断 ToolCalls
              -> 执行工具并回灌
```

---

## 7. 显式核心循环是怎么实现的

对应文件：

- `internal/agent/loop.go`

### 7.1 为什么不用黑盒 ReAct Agent

阶段早期用 `react.NewAgent(...)` 可以快速验证工具调用是否跑通。

但 Claude Code 类工具最核心的扩展点，恰好在工具调用前后：

- 调用工具前是否需要权限确认
- 工具输出是否需要截断/摘要
- 写文件前是否要 checkpoint
- 工具失败如何恢复
- 是否允许 resume

这些能力如果都藏在黑盒 ReAct Agent 里，后续会越来越难插进去。

所以现在改成显式循环：

```text
用户输入
  ↓
messages[]
  ↓
model.Stream(...)
  ↓
assistant.ToolCalls ?
  ↓                  ↓
无                 有
↓                  ↓
输出文本        执行工具
                  ↓
             追加 ToolMessage
                  ↓
             回到 messages[]
```

### 7.2 工具 schema 和工具执行怎么对应

`NewSingleTurnRunner()` 会通过 `buildToolRuntime(...)` 同时构建两份数据：

1. `[]*schema.ToolInfo`：给模型看，让模型知道可调用哪些工具
2. `map[string]InvokableTool`：给运行时用，根据 tool name 找到真正的工具实现

这两份数据来自同一批工具，所以模型看到的工具 schema 和实际可执行工具是一致的。

### 7.3 tool result 怎么回灌

当模型返回 `assistantMsg.ToolCalls` 后，`invokeTool(...)` 会：

1. 取出工具名
2. 从 `invokers` map 找到工具
3. 把模型生成的 JSON arguments 交给 `InvokableRun(...)`
4. 把结果包装成 `schema.ToolMessage(...)`
5. 用原始 `toolCall.ID` 关联回 assistant tool call

这一步对应 Claude Code 核心循环里的：

```text
执行工具 -> 追加 tool_result -> messages[]
```

---

## 8. 只读工具是怎么实现的

对应文件：

- `internal/tools/registry.go`
- `internal/tools/read.go`
- `internal/tools/ls.go`

### 8.1 为什么阶段 1 先做 `read_file` 和 `ls`

对编码 Agent 来说，最基础的仓库感知能力就是：

- 知道目录里有什么
- 能读具体文件内容

所以阶段 1 先只做：

- `ls`
- `read_file`

这两个工具就已经能支撑很多基础问答，比如：

- “README 现在写了什么”
- “项目入口在哪”
- “loop.go 是怎么实现的”

### 8.2 `Registry` 的作用

`Registry` 负责集中装配当前阶段启用的工具。

这样后续扩展时，不需要把工具初始化逻辑散落到 Agent 创建代码里，而是统一从 `Registry.Build(...)` 这个入口接。

以后新增：

- `grep`
- `glob`
- `bash`
- `write_file`

优先接入点都应该是 `Registry`。

### 8.3 `read_file` 的实现方式

`read_file` 用的是 Eino 的 `tool/utils.InferTool(...)`。

它的好处是：

1. 用 Go struct 声明输入参数
2. 自动生成工具 JSON schema 给模型看
3. 自动把 JSON 入参解码成 Go struct

当前 `read_file` 不是一次读取完整文件，而是按需读取一个行窗口：

1. 默认从第 1 行开始读
2. 默认最多读 2000 行
3. 支持 `offset` 指定起始行
4. 支持 `limit` 指定读取行数

例如一个文件有 5000 行时，模型可以先读：

```json
{"file_path":"large.go"}
```

看到结构后，再精确读取：

```json
{"file_path":"large.go","offset":3500,"limit":500}
```

这样做的目的，是让模型按需读取文件，而不是把大文件一次性塞进上下文。

`read_file` 的核心逻辑是：

1. 用 `resolvePath(...)` 校验路径
2. 每次直接 `os.Stat(...)` 确认目标仍然是文件
3. 每次直接打开磁盘文件
4. 逐行扫描到请求窗口，读满窗口后尽快停止
5. 如果后面还有内容，就提示下一次应该使用的 `offset`

这里有一个重要约定：`read_file` 不缓存、不索引、不预处理文件内容。

也就是说，只要文件刚刚被修改，下一次调用 `read_file` 就应该读到磁盘上的最新内容。

### 8.4 `ls` 的实现方式

`ls` 也是 `InferTool(...)` 风格。

内部逻辑是：

1. 校验目录路径
2. 确认目标确实是目录
3. 读取直接子项
4. 生成排序后的文本列表

它返回的是适合模型直接阅读的纯文本，而不是复杂结构体，这样更方便进入下一轮推理。

---

## 9. 工作区路径边界是怎么做的

对应文件：

- `internal/tools/path.go`

### 9.1 为什么阶段 1 就先做路径约束

虽然完整权限系统还没上，但只读工具如果不做边界控制，模型就可能去读工作区外的内容。

这会导致两个问题：

1. 行为边界不清晰
2. 后面权限系统接入时，已有工具需要重新收口

所以当前先把最基础的 workspace 约束做起来。

### 9.2 `resolvePath(...)` 的职责

它现在负责：

1. 规范化工作区根目录
2. 规范化用户请求路径
3. 判断目标是否仍然位于工作区内

如果目标路径逃出 workspace，就直接报错。

这相当于阶段 1 的最小版 `workspace/path policy`。

### 9.3 为什么单独抽到 `path.go`

因为路径边界逻辑不属于某一个工具，而是所有文件工具共享的横切逻辑。

单独抽出来之后，后续无论是：

- `grep`
- `glob`
- `write_file`
- `edit_file`

都可以复用同一套路径约束，而不是每个工具各写一遍。

---

## 10. 系统提示词是怎么用的

对应文件：

- `internal/prompt/system.go`

当前 system prompt 的核心作用不是“塑造人设”，而是约束 Agent 的行为边界：

- 当前只有只读工具
- 需要仓库事实时先用工具
- 没读过文件就不要假装看过

这类提示词在阶段 1 特别重要，因为模型刚开始具备工具能力，最容易出现的问题就是：

- 明明该读文件却直接猜
- 没读文件却说自己“看过代码”

所以这一层属于“行为约束提示”，不是“文风提示”。

---

## 11. 测试现在覆盖了什么

对应文件：

- `internal/tools/tools_test.go`

当前测试主要保护阶段 1 最容易出问题的地方：

1. 路径越界是否被拦住
2. `read_file` 是否真的能读到工作区文件
3. `ls` 是否真的能列出目录项

另外，UI 层现在也补了基础测试，覆盖：

4. `/help` 是否能正确显示帮助
5. `/mode plan` 是否会真正重建对应模式的 runner

这套测试还不大，但已经保护了当前“只读工具 + 路径边界”的最小闭环。

它的意义在于：后面继续加工具时，至少不会把现在这层基础能力悄悄弄坏。

---

## 12. 阶段 1.5 是怎么实现的

对应文件：

- `cmd/nanocode/main.go`
- `internal/permission/mode.go`
- `internal/permission/workspace.go`
- `internal/prompt/system.go`
- `internal/agent/loop.go`
- `internal/tools/path.go`

### 12.1 为什么要先做阶段 1.5

阶段 1 虽然已经能跑通只读 ReAct，但它还有两个基础设施问题没立住：

1. 执行模式只是 README 里的概念，代码里还没有显式入口
2. 工作区路径边界只是一个工具函数，还没有收敛成可复用的策略对象

如果这两件事不先做，后面继续接：

- `grep` / `glob`
- 写操作工具
- 权限确认
- 仓库外授权

都会很容易返工。

所以阶段 1.5 的本质不是“多一个功能”，而是把后面阶段要依赖的基础边界先立起来。

### 12.2 执行模式是怎么接进来的

对应文件：

- `internal/permission/mode.go`
- `cmd/nanocode/main.go`

当前代码把执行模式定义为一个显式类型 `permission.Mode`，并约定三个值：

- `default`
- `plan`
- `accept-edits`

`main.go` 通过 `-mode` 参数接收模式字符串，再调用 `permission.ParseMode(...)` 做解析和校验。

这样做有两个好处：

1. CLI 层已经把“模式”变成真实输入，而不是停留在规划文档里
2. 后续权限系统、工具策略、写操作判断，都可以直接基于这个模式类型扩展，而不是反复比较裸字符串

### 12.3 为什么现在就保留 `accept-edits`

虽然当前还没有真正的写操作工具，但阶段 1.5 仍然先把 `accept-edits` 放进模式枚举里。

原因是它属于执行模型的一部分，而不是写工具本身的一部分。

也就是说，现在先把“模式空间”定义完整，后面阶段 3 接写操作时，只需要实现行为，不需要再改动入口协议。

### 12.4 workspace policy 为什么要单独做成对象

对应文件：

- `internal/permission/workspace.go`

阶段 1 时，路径边界逻辑只是一个 `resolvePath(...)` 函数。

阶段 1.5 后，它被升级成 `WorkspacePolicy` 对象，负责：

1. 保存工作区根目录
2. 对外暴露 `RootDir()`
3. 统一做 `ResolvePath(...)`

这样做的原因是：

- 路径约束不再只是某个工具内部的小工具函数
- 它开始具备“系统级策略”的角色
- 后续可以自然扩展出更复杂的规则，例如：
  - 仓库外白名单
  - 只读路径
  - 可写路径
  - 敏感目录拦截

也就是说，阶段 1 的路径函数更像“校验逻辑”，阶段 1.5 的 `WorkspacePolicy` 更像“权限边界对象”。

### 12.5 `loop.go` 在阶段 1.5 有什么变化

对应文件：

- `internal/agent/loop.go`

阶段 1.5 里，`NewSingleTurnRunner(...)` 的签名变了：

- 之前只接配置
- 现在显式接 `mode permission.Mode`

同时它会在内部：

1. 获取工作区根目录
2. 构建 `WorkspacePolicy`
3. 把 policy 传给工具注册层
4. 用 mode 和 workspace root 共同生成 system prompt

这说明 `loop.go` 现在不只是组装“模型 + 工具”，而是在组装“模型 + 工具 + 执行边界”。

### 12.6 system prompt 为什么改成动态构建

对应文件：

- `internal/prompt/system.go`

阶段 1 时，system prompt 是一个常量字符串。

阶段 1.5 后，它改成 `BuildSystemPrompt(mode, workspaceRoot)` 动态生成。

这样做是因为 prompt 现在不仅要说明：

- 当前有只读工具

还要说明：

- 当前运行在哪种模式
- 当前工作区根目录是什么
- 只能访问哪个边界之内的文件

这类信息是运行时才知道的，所以不能继续写成静态常量。

### 12.7 为什么要把 workspace root 明确写进 prompt

对应文件：

- `internal/prompt/system.go`

虽然工具层已经会实际拦截越界路径，但把 workspace root 直接告诉模型仍然有意义：

1. 可以减少模型无意义地尝试访问工作区外路径
2. 可以让模型在回答里更准确地描述边界
3. 可以把“工具真实边界”和“模型理解边界”尽量对齐

这属于“行为前置约束”，可以减少很多无效尝试。

### 12.8 工具层是怎么适配 workspace policy 的

对应文件：

- `internal/tools/registry.go`
- `internal/tools/path.go`
- `internal/tools/read.go`
- `internal/tools/ls.go`

阶段 1.5 后，工具层不再直接拿 `rootDir string`，而是拿 `*permission.WorkspacePolicy`。

这意味着：

- `Registry` 管的是“策略对象”
- `read_file` 和 `ls` 用的也是同一套策略对象
- `path.go` 成了一个薄封装，只负责转调 policy

这样后面加更多工具时，不需要每个工具自己关心根目录字符串怎么处理，只要依赖同一个 policy 即可。

### 12.9 测试在阶段 1.5 补了什么

对应文件：

- `internal/tools/tools_test.go`

除了原来的路径边界和只读工具测试之外，阶段 1.5 还补了：

- `permission.ParseMode("plan")` 的解析测试

这类测试虽然小，但意义很直接：

- 防止 CLI 模式入口后面被误改
- 保证后续模式扩展时至少有一层基础保护

### 12.10 阶段 1.5 的结果是什么

做完阶段 1.5 之后，项目的状态变成：

- 它已经不只是“能读仓库的单轮 Agent”
- 而是“带执行模式和工作区策略的单轮 Agent”

这为阶段 2 和阶段 3 提供了更稳定的落点：

- 阶段 2 可以继续往工具层扩展 `grep` / `glob`
- 阶段 3 可以在现有 mode + workspace policy 基础上接权限确认和写操作

---

## 13. 当前实现的边界

虽然阶段 1 已经能跑通，但当前仍然有明显边界：

1. 只有单轮，没有多轮会话历史
2. 没有 `grep`、`glob` 这类搜索工具
3. 没有权限确认和执行模式切换
4. 没有 checkpoint / undo
5. 没有会话持久化和记忆系统
6. 工具结果还没有做预算、摘要和截断治理

所以当前版本更像“最小仓库阅读 Agent”，还不是完整的 Claude Code 风格工作流。

---

## 14. 阶段 2 的第一步：`grep` 是怎么接进来的

对应文件：

- `internal/tools/grep.go`
- `internal/tools/registry.go`
- `internal/tools/tools_test.go`

### 14.1 为什么阶段 2 先做 `grep`

阶段 1 的 Agent 虽然已经能：

- 看目录
- 读文件

但它还有一个明显短板：前提是它得先知道“该读哪个文件”。

`grep` 补上的就是这个中间能力：

- 先在整个工作区里找“某个符号/文本出现在哪里”
- 再基于命中的文件继续调用 `read_file`

也就是说，`grep` 让 Agent 从“按已知路径读取”迈向“先搜索，再定位，再读取”。

### 14.2 当前 `grep` 的输入为什么保持得比较小

当前版本的 `grep` 输入只支持：

- `pattern`
- `path`
- `case_sensitive`
- `is_regexp`
- `max_results`

这是刻意控制的。

阶段 2 的第一步，重点不是把搜索工具做成一个完整的 ripgrep 替身，而是先给 Agent 一个稳定、简单、足够常用的仓库搜索接口。

这样模型更容易学会用，也更方便后续继续扩展：

- `glob`
- 文件类型过滤
- 忽略目录策略
- 输出预算和摘要

### 14.3 `grep` 当前怎么工作

当前实现流程是：

1. 通过 `resolvePath(...)` 校验搜索起点仍在工作区内
2. 判断目标是文件还是目录
3. 如果是目录，就递归遍历所有文件
4. 逐行扫描文本
5. 命中后返回：
   - 相对路径
   - 行号
   - 行内容

输出格式类似：

```text
internal/tools/grep.go:12: type grepInput struct {
README.md:130: **阶段 2 — 搜索类工具 + git 状态感知**
```

这种格式的好处是模型和人都容易读，而且下一步继续 `read_file` 时也容易定位。

### 14.4 `grep` 现在为什么是“双后端”

对应文件：

- `internal/tools/grep.go`

当前版本已经把 `grep` 升级成“双后端”：

1. 优先调用本机 `ripgrep (rg)`
2. 如果 `rg` 不存在或当前调用失败，再回退到纯 Go 实现

这样做的目的，是同时拿到两边的好处：

- `rg` 的成熟性能和更好的大仓库体验
- 纯 Go 回退路径带来的可移植性和工具稳定性

也就是说，对 Agent 来说，工具名字和输入输出契约不变，但底层已经可以优先享受 `ripgrep` 的搜索能力。

### 14.5 ripgrep 后端当前怎么接

当前 `runRipgrep(...)` 会：

1. 先用 `exec.LookPath("rg")` 检查本机是否可用
2. 按工具参数组装命令行参数，例如：
   - `--line-number`
   - `--with-filename`
   - `--fixed-strings`
   - `--ignore-case`
   - `--max-count`
3. 在工作区根目录下执行 `rg`
4. 把输出转换成项目内部统一格式

之所以还要做一层格式转换，是因为我们希望最终返回给 Agent 的仍然是统一的：

```text
relative/path.go:12: matched line
```

这样无论底层是 `rg` 还是纯 Go，模型拿到的工具结果形状都尽量一致。

### 14.6 当前测试保护了什么

`grep` 现在补了两类测试：

1. 能否递归命中工作区内多个文件
2. 是否继续遵守工作区边界，不允许路径越界

另外还补了：

3. 输出格式规范化是否符合现有契约
4. 纯 Go 回退路径是否仍然可用

这保证了阶段 2 的第一步至少能稳定提供“搜索定位”能力，而不会绕开当前已经立住的 workspace policy。

---

## 15. 阶段 2 的第二步：`glob` 是怎么接进来的

对应文件：

- `internal/tools/glob.go`
- `internal/tools/registry.go`
- `internal/tools/tools_test.go`

### 15.1 `glob` 解决什么问题

`grep` 是按内容找，适合“我知道关键字，要找它出现在哪里”。

`glob` 是按文件名或路径模式找，适合“我不知道内容关键字，只知道文件类型或路径形状”。

典型例子是：

```text
**/*.tsx
internal/**/*.go
docs/*.md
```

如果用户问“项目里所有 `.tsx` 文件有哪些”，这类问题就应该用 `glob`，而不是 `grep`。

### 15.2 当前 `glob` 的输入

当前输入保持很小：

- `pattern`：必填，例如 `**/*.tsx`
- `path`：可选，指定搜索起点目录，默认工作区根目录

这符合阶段 2 的目标：先把仓库探索的基础工具补齐，而不是一次性做一个复杂文件查询语言。

### 15.3 为什么结果按修改时间倒序

`glob` 的结果不是按字母排序，而是按文件修改时间倒序。

原因很朴素：在真实开发任务里，最近修改过的文件通常更可能和当前任务相关。

这不是严格规则，但作为 Agent 的默认启发式很有用：

- 最近动过的文件更早进入上下文
- 用户更容易看到当前活跃区域
- 模型后续决定 `read_file` 时更容易先读相关文件

### 15.4 为什么有 100 条硬上限

`glob` 当前最多返回 100 个文件。

这是为了避免类似 `**/*` 这样的宽泛 pattern 一次性把上下文塞爆。

如果结果超过 100 条，工具会在末尾提示：

```text
(truncated: showing first 100 matches, refine pattern to see more)
```

这样模型可以根据提示继续收紧 pattern 再搜一次，例如：

```text
src/**/*.tsx
internal/**/*.go
```

### 15.5 当前 `glob` 怎么实现

当前实现使用 `github.com/bmatcuk/doublestar/v4` 来支持 `**` 这类 glob pattern。

执行流程是：

1. 通过 `resolvePath(...)` 校验搜索起点仍在工作区内
2. 确认搜索起点是目录
3. 用 `doublestar.FilepathGlob(...)` 找匹配项
4. 过滤掉目录，只保留文件
5. 转成相对路径
6. 按修改时间倒序排序
7. 最多返回 100 条

### 15.6 当前测试保护了什么

`glob` 现在补了三类测试：

1. `**/*.tsx` 能递归找到匹配文件，并排除非匹配文件
2. 结果按修改时间倒序排列
3. 超过 100 条时会截断并提示
4. 搜索起点不能逃出 workspace

---

## 16. 阶段 2 的第三步：`git_status` 是怎么接进来的

对应文件：

- `internal/tools/git_status.go`
- `internal/tools/registry.go`
- `internal/tools/tools_test.go`

### 16.1 为什么需要 git 状态感知

编码 Agent 在读写代码前，必须知道当前仓库是不是干净的。

原因是用户工作区可能本来就有未提交改动。如果 Agent 不知道这些改动，就很容易：

- 误判当前代码状态
- 覆盖用户正在做的工作
- 在回答里忽略已经改过但未提交的文件

所以阶段 2 在搜索工具之后补上 `git_status`，让 Agent 至少能看到：

- 当前分支
- 工作区是否有未提交改动
- 未暂存 diff 摘要
- 已暂存 diff 摘要

### 16.2 当前 `git_status` 执行哪些命令

当前工具只执行只读 git 命令：

```text
git status --short --branch
git diff --stat
git diff --cached --stat
```

其中：

- `status --short --branch`：给出当前分支和文件状态
- `diff --stat`：给出未暂存改动摘要
- `diff --cached --stat`：给出已暂存改动摘要

这一步不会修改工作区，也不会自动 add / commit / checkout。

### 16.3 输出为什么是摘要而不是完整 diff

阶段 2 只做“状态感知”，不是完整代码审查。

完整 diff 可能非常长，直接塞进上下文会很危险。所以当前先返回 diff stat，让模型知道：

- 哪些文件变了
- 每个文件大概有多少增删
- 是否需要后续用 `read_file` 精确读取

后面如果要做更强的 git 工具，可以再加：

- `git_diff_file`
- `git_show`
- `git_log`

但当前先不让 `git_status` 承担太多职责。

### 16.4 当前测试保护了什么

测试会在临时目录里初始化一个 git 仓库：

1. 提交一个初始文件
2. 修改已跟踪文件
3. 新增未跟踪文件
4. 调用 `git_status`

然后验证输出里包含：

- 分支头信息
- modified 文件
- untracked 文件
- diff stat 摘要

这样可以保证工具不是依赖当前开发仓库状态，而是在干净的临时仓库里验证自己的行为。

---

## 17. 后续维护约定

从现在开始，这份文档按下面的方式维护：

1. 每完成一个核心功能，就补一节
2. 每节优先说明“为什么这样做”和“代码怎么流动”
3. 尽量引用真实文件，而不是只讲概念
4. 重点记录实现脉络，不写流水账式开发日志

下一批建议优先补充的主题：

- 权限闸门怎么插到工具调用前
- checkpoint / undo 准备怎么接
- `grep` / `glob` 加进来后，仓库搜索链路怎么跑
