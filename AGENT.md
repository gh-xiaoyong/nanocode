# AGENT.md

## 项目定位

`nanocode` 是一个仿照 Claude Code 核心思路的轻量级编码 Agent，使用 Go 和 Eino 实现。

当前目标不是一次性复刻完整能力，而是按阶段逐步搭建核心机制：

1. 模型连通与流式输出
2. ReAct Agent 与只读工具
3. 执行模式与工作区边界
4. 权限闸门与写操作
5. 上下文压缩、checkpoint、持久化、记忆

当前代码已完成到阶段 1.5：单轮 ReAct + 只读工具 + 执行模式/工作区边界，并补上了便于验证的终端 REPL。

---

## 当前已实现能力

- OpenAI-compatible 模型配置加载
- 最小 CLI 入口
- 单轮输入
- 单轮流式输出
- 终端 REPL 交互界面（`/help`、`/mode`、`/exit`）
- ReAct Agent 编排
- 只读工具：
  - `read_file`
  - `ls`
- 最小工作区路径边界约束
- 工具基础测试

当前未实现：

- 多轮会话历史
- `grep` / `glob`
- 权限确认
- `plan` / `default` 模式切换
- 写操作工具
- checkpoint / undo
- 会话持久化
- 记忆系统

---

## 代码结构约定

### 入口层

- `cmd/nanocode/main.go`

职责：

- 解析参数
- 加载配置
- 调用 runner

不要在这里堆模型初始化、工具装配、权限逻辑。

### Agent 编排层

- `internal/agent/loop.go`

职责：

- 组装当前阶段运行主流程
- 持有 runner
- 调用模型或 ReAct Agent
- 向终端输出最终流式结果

后续主循环演进优先落在这里。

### 配置层

- `internal/config/config.go`

职责：

- 加载和校验项目级 `settings.json` 与环境变量配置

新增配置时优先扩展这里，不要在业务代码里直接散读环境变量。

当前配置优先级约定：

1. 环境变量
2. 项目根 `settings.json`
3. 默认值

项目根提供：

- `settings-example.json`：配置模板
- `settings.json`：本地真实配置，已被 `.gitignore` 忽略

### 模型层

- `internal/model/factory.go`

职责：

- 根据项目配置创建 Eino 模型实例
- 收敛 provider 相关细节

新增 provider 或调整模型初始化方式时，优先修改这里。

### Prompt 层

- `internal/prompt/system.go`

职责：

- 提供当前阶段的系统提示词

这里的重点是行为边界约束，不只是文风控制。

### 工具层

- `internal/tools/`

职责：

- 工具注册
- 工具实现
- 路径边界复用逻辑

新增工具时优先走：

1. 在 `registry.go` 注册
2. 单独拆实现文件
3. 复用 `path.go` 做工作区约束

### 终端交互层

- `internal/ui/repl.go`

职责：

- 处理终端输入循环、内建命令和结果展示

不要把 Agent 主逻辑塞进 UI 层。

---

## 当前实现原则

1. 先做最小可运行闭环，再扩展复杂能力
2. 优先保持模块边界清晰
3. 当前阶段尽量不提前引入后面阶段的复杂抽象
4. 工具能力和权限能力分阶段接入
5. 对用户可见的说明、学习文档、中文注释要同步维护

---

## 注释与文档约定

### 中文注释

开发时需要补关键中文注释，重点写：

- 这段逻辑在整体流程中的作用
- 为什么要这样做
- 当前阶段的能力边界

避免逐行翻译式注释。

### 学习文档

项目内学习文档位置：

- [docs/LEARNING.md](D:/projects/nanocode/docs/LEARNING.md)

要求：

- 每完成一个核心功能，就补对应实现说明
- 优先讲清执行链路、设计原因、后续扩展点
- 尽量引用真实代码文件

### 编码约定

编码约定位置：

- [docs/ENCODING.md](D:/projects/nanocode/docs/ENCODING.md)

仓库文本文件统一使用：

- `UTF-8`
- `LF`

---

## 当前推荐开发顺序

基于 README 当前规划，下一步优先顺序建议是：

1. 阶段 1.5：执行模式与更明确的 workspace/path policy
2. 阶段 2：`grep` / `glob` 与仓库搜索能力
3. 阶段 3：写操作工具与权限闸门
4. 阶段 4+：上下文管理、checkpoint、持久化、记忆

---

## 后续改动时的优先检查点

每次新增能力时，优先检查：

1. 是否破坏了当前单轮流式输出
2. 是否绕开了工具注册入口
3. 是否绕开了工作区路径边界
4. 是否遗漏了中文注释
5. 是否需要同步更新：
   - `README.md`
   - `docs/LEARNING.md`
   - `docs/ENCODING.md`（如果涉及编码/文件约定）
