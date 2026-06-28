# nanocode

一个仿照 Claude Code 核心设计的轻量级编码 Agent，使用 Go + Eino 实现。

本项目的 README 承担路线图 / list 角色；具体实现学习记录见 [docs/LEARNING.md](D:/projects/nanocode/docs/LEARNING.md)。

## 核心目标

1. 带工具的 Agent 主循环
2. 终端交互 UI
3. 权限闸门与执行模式
4. 工作区路径边界
5. 文件写入、编辑、checkpoint 与撤销
6. 上下文压缩、会话持久化、记忆机制
7. 可扩展工具生态

## 当前状态

- 当前进度：**阶段 5.5 已完成**
- 已接入：显式 Agent loop、终端 REPL、只读工具、搜索工具、git 状态感知、写操作工具、权限闸门、checkpoint、上下文预算、工具输出截断、规则型消息压缩、会话持久化、指令记忆
- 下一阶段：**阶段 6：体验打磨**

## 已实现能力

- OpenAI-compatible 模型配置
- `settings.json` 本地真实配置，`.gitignore` 已忽略
- `settings-example.json` 配置模板
- 环境变量覆盖模型配置
- 启动终端 UI 时显示当前模型
- 单轮 `-prompt` 模式
- 终端 REPL：
  - `/help`
  - `/mode`
  - `/memory`
  - `/session`
  - `/undo`
  - `/exit`
- 显式核心循环：
  - `messages[] -> model.Stream -> tool_call -> tool_result -> messages[]`
- SSE / 流式文本输出
- 执行模式：
  - `default`
  - `plan`
  - `accept-edits`
- 工作区路径边界：
  - 文件工具默认只能访问当前项目根目录内路径
- 只读工具：
  - `read_file`
  - `ls`
  - `grep`
  - `glob`
  - `git_status`
- 写操作与命令工具：
  - `write_file`
  - `edit_file`
  - `bash`
- 权限策略：
  - `plan`：不向模型暴露写操作和 shell 工具，并拒绝写操作和 shell
  - `default`：写操作和 shell 执行前询问
  - `accept-edits`：普通文件写入自动允许，shell 仍需询问
- checkpoint：
  - 写入前保存最近一次文件快照
  - `/undo` 撤销最近一次成功写入
- 上下文管理：
  - 近似 token 估算
  - 根据 `settings.json` 的 `context_window` 显示上下文使用情况
  - 工具输出统一截断
  - 单轮内 messages 预算检查
  - 规则型 compaction summary
- 会话持久化：
  - `.nanocode/sessions/` 本地会话文件
  - REPL 内跨轮保留 messages
  - `/session`
  - `--continue`
  - `--resume <session-id>`
- 指令记忆：
  - 项目级 `NANO.md`
  - 用户级 `~/.nanocode/NANO.md`
  - 启动时注入 system prompt
  - `/memory` 查看当前加载记忆
  - `NANO-example.md` 模板

## 开发阶段

### 阶段 0：骨架与单轮对话

状态：已完成

- Go module
- CLI 入口
- OpenAI-compatible 配置
- 单轮输入
- 流式输出

### 阶段 1：显式 Agent 核心循环 + 只读工具

状态：已完成

- 显式维护 `messages[]`
- 模型 tool call 后执行工具
- 工具结果回填模型
- `read_file`
- `ls`

### 阶段 1.5：plan mode 与工作区边界

状态：已完成

- `default`
- `plan`
- `accept-edits`
- workspace path policy
- 动态 system prompt
- 终端 UI 模式切换

### 阶段 2：搜索工具 + git 状态感知

状态：已完成

- `grep`
  - 优先调用 `rg`
  - 无 `rg` 时 fallback 到纯 Go 实现
- `glob`
  - 支持 `**/*.tsx`
  - 按修改时间倒序
  - 最多返回 100 条
- `git_status`
  - 当前分支
  - 未提交改动
  - unstaged / staged diff stat

### 阶段 3：写操作 + 权限闸门

状态：已完成

- 工具权限分类：`read` / `write` / `shell`
- `plan` 模式只绑定只读工具
- 权限策略模块
- Agent loop 工具执行前权限判断
- REPL 权限确认
- 非交互 `-prompt` 模式默认拒绝需确认操作
- `write_file`
- `edit_file`
- `bash`
- 写入前 checkpoint
- `/undo` 撤销最近一次成功写入

### 阶段 4：上下文管理

状态：已完成

- token 近似估算
- 单轮内 messages 预算检查
- 工具输出预算
- 大输出统一截断
- 规则型 compaction summary
- 回复尾部显示上下文使用情况

### 阶段 5：会话持久化

状态：已完成

- 保存历史到磁盘
- `--continue`
- `--resume`
- `/session`
- REPL 内跨轮记住对话

### 阶段 5.5：记忆机制

状态：已完成

- 项目级 `NANO.md`
- 用户级 `~/.nanocode/NANO.md`
- 启动时读取并注入 system prompt
- `/memory`
- `NANO-example.md`

### 阶段 6：体验打磨

状态：未开始

- 更好的终端 UI
- 工具调用可视化
- diff 展示
- token / 耗时统计

### 阶段 7：扩展机制

状态：未开始

- hooks
- MCP
- slash command
- subagent
- skills

## 模型配置

项目根目录维护真实配置：

```json
{
  "model": {
    "base_url": "https://api.openai.com/v1",
    "api_key": "your-api-key-here",
    "model": "gpt-4.1",
    "temperature": 0.2,
    "max_tokens": 4096,
    "context_window": 1000000
  }
}
```

也可以使用环境变量：

- `NANOCODE_API_KEY`
- `NANOCODE_MODEL`
- `NANOCODE_BASE_URL`
- `NANOCODE_TEMPERATURE`
- `NANOCODE_MAX_TOKENS`
- `NANOCODE_CONTEXT_WINDOW`

兼容 `OPENAI_*` 环境变量。

## 验证命令

```powershell
go test ./...
go build ./...
go run ./cmd/nanocode
```
