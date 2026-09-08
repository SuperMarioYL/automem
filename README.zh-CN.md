[English](./README.md) · [Website](https://automem.lei6393.com) · [GitHub](https://github.com/SuperMarioYL/automem)

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/hero-dark.svg">
  <img src="./assets/presentation/hero-light.svg" width="960" alt="Hero diagram">
</picture>

# automem

**让有用的会话上下文延续到下次启动。**

automem 从会话记录提取用户消息和文件引用，在本地保存，再通过短生命周期 CLI 检索相关摘要。

## 为什么需要它

反复粘贴同一项目决策很繁琐。保存会话后，在需要时召回匹配内容；本地流程无需嵌入服务或模型请求。

- **本地离线记忆** — 捕获与召回无需密钥或守护进程。
- **可检查的摘要** — 抽取的文字与路径保存在 JSONL 中。
- **预览集成修改** — install --dry-run 展示 hook 或包装器修改。

## 架构

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-dark.svg">
  <img src="./assets/presentation/architecture-light.svg" width="960" alt="Architecture diagram">
</picture>

capture 解析会话，将末尾用户消息、路径及 diff 信息写入 JSONL。recall 按加权词汇重合与指数时间衰减排序，并可增加 injected 计数。install 可写入调用同一 CLI 的 Claude Code hook 或 Aider 包装器。

| 组件 | 职责 |
| --- | --- |
| `Transcript extraction` | internal/capture |
| `Local JSONL store` | internal/store |
| `Lexical recall` | internal/recall |
| `Counters + hooks` | internal/stats; internal/install |

## 安装与快速上手

使用仓库清单声明的运行时版本。以下源码安装步骤可复现随仓示例。

```bash
git clone https://github.com/SuperMarioYL/automem.git
cd automem
go build ./cmd/automem
```

在临时库捕获 examples/session.transcript，召回构造函数决策并检查计数；示例由 Python 3 驱动。

```bash
python3 examples/presentation_demo.py
```

## 实际运行示例

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/process-dark.svg">
  <img src="./assets/presentation/process-light.svg" width="960" alt="Process diagram">
</picture>

The included transcript is recalled for auth.py; the store reports one record and one injection.

```text
# memory 1/1  (score 1.667)
also make sure the old constructor keeps working for callers we don't own
refactor auth.py to use dataclasses
files: auth.py
1 stored, 1 injected
  injection rate: 100% (1 of 1 memories recalled at least once)
  total injections: 1
  by agent:
    claude-code  1
```

完整命令与输出保存在 [docs/demo-results.json](./docs/demo-results.json). 输入和复现代码均随仓提供。

![已有终端录制](./assets/demo.gif)

保留已有录制供参考；上方文字示例给出当前可复现的操作。

## 用法

安装后在仓库根目录运行以下命令；处理自己的数据时替换相应路径。

```bash
go run ./cmd/automem capture --agent claude-code examples/session.transcript
go run ./cmd/automem recall --top 3 --no-mark "auth.py constructor"
go run ./cmd/automem stats
go run ./cmd/automem install --dry-run
```

## 配置

AUTOMEM_DIR 默认 ~/.automem，管理 store.jsonl。AUTOMEM_HOME 改变安装路径，AUTOMEM_BIN 指定写入 hook 的可执行文件。recall --top 设置返回数；--no-mark 只预览、不更新计数。install --dry-run 预览配置修改；确认预览后再运行会修改 Agent 配置的 install。

## 集成与职责分工

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-dark.svg">
  <img src="./assets/presentation/integrations-light.svg" width="960" alt="Integrations diagram">
</picture>

根据工作流选择输入与输出路径。本文本地示例验证其中明确说明的子流程。

| 路径 | 已实现职责 |
| --- | --- |
| Text / JSONL | Transcript capture |
| Local JSONL | Persistent memory records |
| Claude Code hooks | SessionStart / Stop integration |
| Aider wrapper | Best-effort integration |
| Terminal | Recall summaries and statistics |

## 限制与后续方向

- 召回使用词汇匹配，不是语义嵌入检索；它扫描已有记录，可能漏掉改写表达。
- injected 计数表示 recall 输出过该记忆，不证明模型实际利用它或改善回答。
- 源码将 Aider 包装器标为未验证；sync 与 team 是占位命令，此处未提供托管后端。

本地嵌入、更多 Agent 传输适配、Windows 支持与跨机器同步仍是后续方向。

## 许可与贡献

许可见 [LICENSE](./LICENSE). 反馈问题时请提供最小输入、执行命令和实际输出。
