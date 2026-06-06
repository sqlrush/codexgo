# codexgo 方案设计文档

> 版本:对应 codexgo v0.2.2 · 更新日期:2026-06-06
> 维护者决策记录;英文技术细节见 `DEVIATIONS.md` 与 `docs/specs/`。

---

## 1. 产品定位

**codexgo 是一个独立产品**,而非 OpenAI Codex 的替代品:

- 起点:对 OpenAI Codex CLI **0.136.0**(Rust,~935K 行)的逐字节级 Go 重写,
  通过差分测试(`internal/paritytest/`,对照真实 codex 二进制)验证忠实度;
- 转折(2026-06-06 维护者决策):codexgo 与原生 codex **在同一台机器上完全
  隔离共存**,并走向**多模型后端**(GLM、DeepSeek 等非 OpenAI 厂商),
  不再追求 drop-in 替代;
- 兼容性保留在**协议与文件格式层**(请求体、rollout JSONL、SQLite schema、
  config.toml 格式),便于持续吸收上游改进。

```
                ┌─ 本地身份:完全独立(第 2 节)
  codexgo ──────┼─ 引擎内核:逐字节移植 + 差分验证(parity 资产)
                └─ 模型后端:多厂商直连(第 3 节,上游不具备)
```

---

## 2. 本地隔离方案

目标:**对原生 codex 的配置、记忆、凭证、环境变量零接触、零冲突。**

| 维度 | 原生 codex | codexgo |
|------|-----------|---------|
| 可执行文件 | `/opt/homebrew/bin/codex` | `/opt/homebrew/bin/codexgo` |
| 配置主目录 | `~/.codex/` | `~/.codexgo/` |
| 环境变量 | `CODEX_*` | `CODEXGO_*`(读取与导出子进程两个方向全部改名) |
| macOS 钥匙串 | service `codex` / `Codex MCP Credentials` | `codexgo` / `CodexGo MCP Credentials` |
| 项目级目录 | `<repo>/.codex/` | `<repo>/.codexgo/`(不读 `.codex/`) |
| 系统级配置 | `/etc/codex/` | `/etc/codexgo/` |
| 插件清单 | `.codex-plugin/plugin.json` | `.codexgo-plugin/` 优先,`.codex-plugin/`、`.claude-plugin/` 兼容回退 |

实现集中在 `internal/brand/brand.go`(身份常量单一来源)。
**线上身份**(OAuth client id、originator、User-Agent、登录端点)保持与上游
一致,保证 ChatGPT 账号登录可用;界面文案品牌化为独立的后续专项。

登录互不影响:`codexgo login` 写 `~/.codexgo/auth.json`,与原生
`~/.codex/auth.json` 完全独立;唯一注意点是两者的 OAuth 回调端口同为
1455,不要**同一时刻**执行两边的 login。

---

## 3. 多模型方案(核心差异化能力)

### 3.1 背景:上游的限制

- codex 0.136 把 `wire_api = "chat"` **整体移除**(配置该值直接报迁移错误,
  见上游 discussion #7782),自定义 provider 只能讲 OpenAI **Responses API**
  (`/v1/responses`);
- 实测(2026-06-06):GLM(open.bigmodel.cn/api/paas/v4)与 DeepSeek
  (api.deepseek.com/v1)的 `/responses` 均返回 404,**只提供
  `/chat/completions`**;
- 因此原生 codex 直连这类厂商不可行,必须外挂翻译代理(LiteLLM 等)。

### 3.2 codexgo 的解法:原生 chat 协议层

上游已无 chat 实现可移植,该层为 codexgo 全新编写(v0.2.0):

```
                       ┌──────────────────────────────────────────┐
   引擎回合循环          │   翻译层(本方案新增)                       │      第三方厂商
   (协议无关)           │                                          │
                       │  发出:internal/core/client_chat.go       │
  对话历史 ────────────▶│   · Responses items → chat messages      │───▶ POST /chat/completions
  11 个工具 ───────────▶│   · 工具 JSON-schema → chat function tools│      (GLM / DeepSeek / …)
                       │   · developer 角色 → system               │
                       │   · 工具结果 → role:"tool"                 │
                       │                                          │
  ResponseEvent ◀──────│  收回:internal/api/chat_completions.go   │◀─── SSE 流
  (与 Responses        │   · content 增量 → OutputTextDelta        │
   解析器同一事件语言)    │   · reasoning_content → 推理增量          │
                       │   · tool_calls 增量聚合 → FunctionCall     │
                       │   · finish → Completed{EndTurn}           │
                       └──────────────────────────────────────────┘
```

**关键设计**:翻译层把 chat SSE 聚合成与 Responses 解析器**完全相同**的内部
事件序列,引擎(回合循环、工具执行、沙箱、TUI)对协议差异零感知——因此
第三方模型获得**完整智能体能力**。已实测 GLM 自主发起 `exec` 调用、执行
shell 命令、根据结果续轮作答。

有意不做的部分:推理参数(effort/verbosity/service-tier)无 chat 等价物,
不发送;reasoning 内容流式显示但不回传历史(chat 协议不接受);多模态图片
暂不透传。

### 3.3 模型 → provider 自动路由

codexgo 扩展配置字段 `models = [...]`(上游没有;为空时不出现在序列化结果
中,保证与上游配置互通):

```toml
# ~/.codexgo/config.toml
model = "gpt-5.5"                  # 默认模型,/model 选择会更新此键

[model_providers.glm]
name = "GLM (Zhipu AI)"
base_url = "https://open.bigmodel.cn/api/paas/v4"
wire_api = "chat"
experimental_bearer_token = "<key>"   # key 直接放配置(0600),不进环境变量
models = ["glm-5.1"]                  # ← codexgo 路由扩展

[model_providers.deepseek]
name = "DeepSeek"
base_url = "https://api.deepseek.com/v1"
wire_api = "chat"
experimental_bearer_token = "<key>"
models = ["deepseek-v4-pro"]
```

装配时构建「模型名 → provider 工厂」路由表
(`appserver.NewModelRoutedClientFactory` + `cli.buildModelProviderRoutes`):
**只改 `model` 一个键即切换后端**,`model_provider` 无需变动;未声明的模型
落回默认 provider(ChatGPT 登录 / OPENAI_API_KEY)。

新增厂商 = 在配置里加一个 `[model_providers.X]` 块,任何 OpenAI 兼容
chat API(Qwen、Moonshot、本地 vLLM/Ollama 网关……)均适用。

### 3.4 /model 选择器(TUI)

- 入口:TUI 内输入 `/model`;
- 列表 = 内置 OpenAI 目录(gpt-5.5 / 5.4 / 5.4-mini / 5.3-codex / 5.2)
  + 各 provider 的 `models` 声明,支持输入过滤,标记 `(current)`;
- 选中后三件事原子发生:写回 `config.toml` 的 `model` 键(持久化)、
  以模型覆盖开启新会话(`thread/start` 带 `Model` 参数,路由立即生效)、
  底部状态栏同步;
- 实现:`internal/tui/model_picker.go` + `ChatBottomPane` 的 overlay 栈。

> 教训记录:overlay 回调最初在 Update 调用栈内同步调用
> `Program.Send`(无缓冲通道),导致事件循环死锁(选中即整屏冻结)。
> 修复为回调延迟到 `tea.Cmd`(bubbletea 在事件循环外执行),由
> `TestPickerAcceptDoesNotSendSynchronously` 以真实死锁形态锁定回归。

### 3.5 能力对比

| 能力 | 原生 codex 0.136 | codexgo v0.2.x |
|------|------|------|
| OpenAI 模型(ChatGPT 套餐/API key) | ✅ | ✅(同一 OAuth 链路) |
| Responses API 自定义厂商 | ✅ | ✅ |
| **chat-only 厂商直连(GLM/DeepSeek/…)** | ❌ 需外挂代理 | ✅ 原生 |
| 第三方模型的完整智能体能力(工具/沙箱) | —(取决于代理翻译质量) | ✅ 实测 |
| 模型名自动路由后端 | ❌ | ✅(`models` 扩展) |
| TUI 内 /model 即时切换+持久化 | 仅 OpenAI 目录 | ✅ 含第三方模型 |

---

## 4. 版本管理与发布流程(2026-06-06 起生效)

```
改动 → 升 ./VERSION(semver:功能→次版本,修复→补丁)
     → 本地 commit
     → ./scripts/deploy-local.sh(按 VERSION 构建 → /opt/homebrew/bin/codexgo)
     → 【维护者在本机验证】
     → 通过后:git push + 打 v<VERSION> 标签
```

- `VERSION` 文件是版本唯一来源,经 ldflags 注入二进制
  (`codexgo --version` 可见);
- 未经验证的提交**不出本机**。

---

## 5. 质量保障

- **单元/集成测试**:协议翻译、SSE 聚合、路由表、选择器交互、配置持久化、
  死锁回归均有专项测试;全量 `go test -race ./...` 常绿
  (已知例外:`internal/uds` 在本机因 sockaddr_un 路径长度限制失败,
  干净树上同样存在,非回归信号);
- **差分测试**:`CODEX_PARITY_BIN` 指向真实 codex 0.136 二进制,
  请求体逐字节对比、TUI 首帧逐格对比(strict 等级含 SGR 属性);
- **分叉登记**:所有有意偏离上游之处集中登记于 `DEVIATIONS.md`
  (身份隔离、wire_api chat、models 路由、/model 选择器、模拟光标等),
  目标是零未解释差异。

---

## 6. 已知限制与路线

| 项 | 状态 |
|----|------|
| /model 选择器的推理力度(effort)选择 | 未做(上游选择器具备),按需跟进 |
| TUI 斜杠命令覆盖 | 已接:/model /new /quit /clear /compact /rename /review;其余显示「not supported yet」提示,按使用优先级接线 |
| 界面 "codex" 文案品牌化 | 独立专项(涉及 TUI golden 重录) |
| 第三方模型多模态(图片) | chat 翻译层暂丢弃图片内容 |
| chat 厂商的用量统计 | 依赖厂商在流中带 usage,缺失时容忍为空 |

---

## 7. 关键文件索引

| 文件 | 职责 |
|------|------|
| `internal/brand/brand.go` | 本地身份常量(目录/环境变量前缀/钥匙串/插件清单) |
| `internal/api/chat_completions.go` | chat SSE 流客户端与事件聚合 |
| `internal/core/client_chat.go` | Prompt → chat 请求翻译(消息/工具/角色) |
| `internal/tools/chat_api.go` | 工具 spec → chat function tools |
| `internal/appserver/model_client_factory.go` | wire 分支 + 模型路由工厂 |
| `internal/cli/assembly.go` | 路由表构建、provider 认证链、默认模型解析 |
| `internal/cli/model_picker_host.go` | 选择器条目构建 + config 持久化 |
| `internal/tui/model_picker.go` | /model 选择器 UI |
| `internal/tui/overlay_list.go` | 通用列表 overlay(回调延迟执行) |
| `VERSION` / `scripts/deploy-local.sh` | 版本与本地部署 |
| `DEVIATIONS.md` | 有意分叉登记(英文,权威) |
