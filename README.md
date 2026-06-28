# nanocode

一个仿照 Claude Code 核心设计的轻量级编码 Agent，用 Go 语言 + [Eino](https://github.com/cloudwego/eino) 框架实现。

目标不是复刻全部功能，而是把 Claude Code 的**核心机制**做对：带工具的 Agent 循环、权限/沙箱控制面、上下文与记忆管理、可恢复执行。

协作约定见：[AGENT.md](D:/projects/nanocode/AGENT.md)
学习记录见：[docs/LEARNING.md](D:/projects/nanocode/docs/LEARNING.md)
编码约定见：[docs/ENCODING.md](D:/projects/nanocode/docs/ENCODING.md)

## 核心理念

即使交互载体仍然是终端，一个编码 Agent 的本质也不在于 UI 是否花哨，而在于：

**带工具的 Agent 循环 + 权限/沙箱控制面 + 上下文/记忆管理 + 可恢复执行 + 可扩展工具生态**

1. **Agent 主循环**：用户输入 → 模型推理 → 决定调用工具或直接回答 → 执行工具 → 结果回灌模型 → 直到不再需要工具。即 ReAct 模式。
2. **工具集**：读文件、写/改文件、执行 shell、搜索（grep/glob）、列目录。工具是能力边界，也是风险来源。
3. **权限闸门**：危险/不可逆操作（写文件、跑命令）在执行前可被拦截并征求许可。这是和"纯聊天 bot"最大的区别。
4. **Plan mode / 只读规划模式**：复杂任务先允许读文件、搜索、分析和产出计划，禁止写文件和有副作用命令；用户确认后再进入执行模式。
5. **工作区边界与命令分级**：所有文件工具都受 workspace/path policy 约束；shell 命令按只读、构建/测试、文件修改、网络访问、敏感信息读取、危险命令分级。
6. **可恢复执行**：写入前保存 checkpoint / 文件快照，允许撤销最近一次工具修改；不能只依赖 git，因为用户工作区可能本来就是脏的。
7. **上下文管理**：对话变长时，在接近上限前做压缩（compaction），保留关键事实继续工作；工具输出也要有预算、截断和摘要策略。
8. **流式输出 + 可观测**：边想边出字，工具调用过程对用户可见。
9. **会话持久化**：历史能存能续。
10. **记忆**：跨会话的长期知识——项目约定、用户偏好、工作中沉淀的事实。和会话持久化不同，记忆不进对话历史，不受上下文压缩影响，换个新会话依然在。
11. **可扩展性**：先内置核心工具，后续预留 MCP、hooks、skills、subagent、slash command 等扩展点，避免工具体系被写死。

## Eino 复用 vs 自研

Eino 给你**发动机**（循环 + 工具 + 模型抽象），你造**车身**（权限、上下文、UI、持久化）。

| 能力 | 来源 |
|------|------|
| 统一消息 / 工具调用结构 (`schema.Message`) | Eino 现成 |
| 模型抽象 + 流式 (`components/model`, ToolCallingChatModel) | Eino 现成 |
| 工具定义（Go 函数 + struct tag 自动生成 JSON Schema，`utils/tool.InferTool`） | Eino 现成 |
| 模型抽象、工具 schema、消息结构 | Eino 现成 |
| 显式 Agent 主循环（messages → model → tool call → tool result） | 自研 |
| 执行中断 / 恢复（权限闸门基础，`compose.Graph` interrupt / checkpoint） | Eino 提供机制 |
| 流式渲染 / 日志 / token 统计 (`callbacks`) | Eino 提供钩子 |
| **权限策略 / 沙箱边界** | 自研 |
| **Plan mode / 执行模式切换** | 自研 |
| **文件 checkpoint / 回滚** | 自研 |
| **上下文压缩** | 自研 |
| **工具输出预算 / 截断 / 摘要** | 自研 |
| **会话持久化** | 自研 |
| **记忆机制** | 自研 |
| **终端 UI** | 自研 |

## 项目结构

```text
nanocode/
├── cmd/nanocode/main.go          # 入口：解析参数、初始化、启动 REPL
├── internal/
│   ├── agent/                    # Agent 编排
│   │   ├── agent.go              # 组装 ReAct agent / 自定义 graph
│   │   └── loop.go               # 主循环：输入→运行→处理中断→输出
│   ├── tools/                    # 工具实现（每个工具一个文件）
│   │   ├── read.go  write.go  edit.go
│   │   ├── bash.go  grep.go  glob.go  ls.go
│   │   └── registry.go           # 注册表 + 风险等级标注
│   ├── permission/               # 权限闸门
│   │   ├── mode.go               # plan/acceptEdits/default 等执行模式
│   │   ├── policy.go             # 规则：哪些工具/参数需要确认
│   │   ├── workspace.go          # 工作区/path 边界，仓库外读写限制
│   │   └── prompt.go             # 向用户征询（allow/deny/always）
│   ├── checkpoint/               # 文件快照与回滚
│   │   └── store.go              # 写操作前保存快照，支持撤销最近修改
│   ├── model/                    # 模型 provider 配置与构建
│   │   └── factory.go
│   ├── prompt/                   # 系统提示词、工具说明模板
│   │   └── system.go
│   ├── context/                  # 上下文/历史管理
│   │   ├── history.go            # 消息累积
│   │   ├── compact.go            # 接近上限时的压缩策略
│   │   └── budget.go             # token 估算、工具输出预算、截断策略
│   ├── session/                  # 会话持久化
│   │   └── store.go              # 存盘/续会话（JSON 文件即可起步）
│   ├── memory/                   # 跨会话记忆
│   │   ├── instruction.go        # 指令型：加载 NANO.md，注入 system prompt
│   │   └── facts.go              # 事实型：读写记忆文件 + 索引
│   ├── git/                      # git 状态感知
│   │   └── status.go             # branch/status/diff 摘要，避免误伤用户改动
│   ├── codeintel/                # 代码智能（后续可接 LSP）
│   │   └── diagnostics.go        # diagnostics / references / definitions 预留
│   ├── extension/                # 扩展点预留
│   │   ├── hooks.go              # 工具调用前后 hook
│   │   └── mcp.go                # MCP 工具接入预留
│   └── ui/                       # 终端交互
│       ├── render.go             # 流式渲染、markdown、工具调用展示
│       └── repl.go               # 输入循环
├── configs/                      # 模型 key、默认参数
├── go.mod
└── README.md
```

起步阶段不必拆这么细，但这个边界划分能让权限、上下文这些"非 Eino 现成"的关注点不被工具/循环代码淹没。

## 开发步骤

每个阶段都能独立跑起来。

**阶段 0 — 骨架与单轮对话**
跑通 `go mod` + Eino 依赖，先接 OpenAI 兼容接口（`base_url`、`api_key`、`model` 三件套），实现"输入一句 → 流式打印回答"。验证模型连通和流式渲染，还没有工具。

**阶段 1 — 接入显式 Agent 核心循环 + 第一个只读工具**
用 Eino 的模型、消息和工具抽象，但在项目内显式维护 `messages[] → model → tool call → tool result → messages[]` 循环。先只给 `read_file`、`ls` 这类只读、零风险工具。验证完整循环：模型能自己决定读文件并基于内容回答。只读工具先行，可在没有权限系统时安全地把循环调通。

**阶段 1.5 — Plan mode 与工作区边界**
加入执行模式：默认模式可按策略申请写入，plan mode 只能读文件、搜索和分析。所有文件工具先接入 workspace/path policy，默认限制在当前项目根内，仓库外读写必须单独授权。

**阶段 2 — 搜索类工具 + git 状态感知**
加 `grep`、`glob`，让 Agent 具备"在代码库里找东西"的能力，是后续所有任务的基础。同时加入 `git status` / `git diff` 摘要，让 Agent 知道当前分支和用户未提交改动。

**阶段 3 — 写操作 + 权限闸门（核心难点）**
加 `write_file`、`edit_file`、`bash`，同时引入 `permission` 模块。用 Eino graph 的 interrupt 在工具执行前暂停，展示"准备执行什么"，等 allow/deny 再 resume。edit 工具走"精确字符串替换"，比让模型重写整个文件更可控。写入前必须创建文件 checkpoint，支持撤销最近一次修改。

**阶段 4 — 上下文管理**
实现 token 估算和压缩：超过阈值时，把早期对话总结成一条摘要消息，保留近期原始消息。工具输出也要走预算控制：大文件、大 grep 结果、测试日志默认截断，必要时生成摘要并把完整结果落盘引用。否则长任务必然撞上下文上限。

**阶段 5 — 会话持久化**
存历史到磁盘，支持 `--continue` / `--resume`。Eino 的 checkpoint 也可用于"中断处恢复"。

**阶段 5.5 — 记忆机制**
分两步：
- **指令型**：启动时查找项目根的 `NANO.md`（及用户级 `~/.nanocode/NANO.md`），内容拼进 system prompt。这一步成本极低、收益大，可以提前到阶段 1 之后做。
- **事实型**：约定一个记忆目录（每条事实一个文件，带 frontmatter），加一个索引文件在会话启动时载入；给 Agent 一个 `remember` 工具主动写入。关键是记忆**不进对话历史**，所以不被压缩、跨会话存活。

**阶段 6 — 体验打磨**
更好的终端 UI（可选 Bubble Tea + Glamour 做 markdown）、工具调用 diff 展示、错误重试、配置文件、token/耗时统计。

**阶段 7 — 扩展机制**
预留并逐步实现 hooks、MCP、slash command、subagent。subagent 可以先做成只读 `task` 工具：隔离上下文完成搜索/审计/阅读大量文件，最后只把摘要回传主会话。

## 早期决策点

这几个会影响结构，值得先定：

1. **模型 provider**：优先支持 OpenAI 兼容接口，配置项固定为 `base_url`、`api_key`、`model`、`temperature`、`max_tokens`。这样可以同时接 OpenAI、Azure OpenAI、兼容网关和本地兼容服务；Anthropic 原生接口后续作为第二 provider 加。
2. **权限粒度**：不能只按"工具类型"判断。MVP 可先做到工具级确认，但策略结构要支持参数级判断：路径是否越界、bash 是否有副作用、是否读敏感文件、是否访问网络。
3. **默认执行模式**：建议提供 `default`、`plan`、`accept-edits` 三种。`plan` 禁止写入和有副作用命令；`accept-edits` 可自动接受普通文件编辑，但 bash 仍需确认。
4. **checkpoint 粒度**：MVP 先做文件级快照，记录每次工具调用修改了哪些文件；后续再做会话级 checkpoint 和多步回滚。
5. **何时下沉到 graph**：当前已经把核心 tool loop 显式写在项目内；后续如果权限 interrupt、checkpoint、resume 需要更强的可恢复编排，再评估是否下沉到 Eino `compose.Graph`。

## OpenAI 兼容模型配置

MVP 默认只要求 OpenAI-compatible chat completions / tool calling 能力。

当前推荐在**项目根目录**维护真实配置文件 `settings.json`，并参考模板 `settings-example.json`：

```json
{
  "model": {
    "base_url": "https://api.openai.com/v1",
    "api_key": "your-api-key-here",
    "model": "gpt-4.1",
    "temperature": 0.2,
    "max_tokens": 4096
  }
}
```

如果你不想把真实 key 写死在文件里，也可以改成：

```json
{
  "model": {
    "base_url": "https://api.openai.com/v1",
    "api_key_env": "NANOCODE_API_KEY",
    "model": "gpt-4.1",
    "temperature": 0.2,
    "max_tokens": 4096
  }
}
```

配置优先级当前是：

1. 环境变量
2. 项目根 `settings.json`
3. 默认值（仅 `base_url`、`temperature`、`max_tokens` 有默认值）

支持的环境变量包括：

- `NANOCODE_API_KEY`
- `NANOCODE_MODEL`
- `NANOCODE_BASE_URL`
- `NANOCODE_TEMPERATURE`
- `NANOCODE_MAX_TOKENS`

以及兼容的 `OPENAI_*` 变量。

仓库已通过 `.gitignore` 忽略 `settings.json`，所以可以本地保存真实配置；`settings-example.json` 用于分发模板。本地或第三方兼容服务只需要替换 `base_url` 和 `model`。`model/factory.go` 不直接绑定具体厂商，内部只依赖"支持流式输出 + tool calling"的能力。

## 状态

- 当前代码已完成**阶段 2**
- `grep` / `glob` / `git_status` 已接入
- 下一步进入**阶段 3：写操作 + 权限闸门**
- `README` 继续承担路线图 / list 角色
- 具体实现细节请看 [docs/LEARNING.md](D:/projects/nanocode/docs/LEARNING.md)
