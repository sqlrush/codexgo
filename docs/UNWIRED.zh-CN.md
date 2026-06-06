# codexgo 未接线功能清单

> 盘点日期:2026-06-06 · 对应 codexgo v0.2.3
> 用途:opendb 大规模集成前的功能基线;后续逐项补充的待办依据。

## 什么叫"未接线"

共同特征:**引擎/组件已移植并通过单测或差分验证,但没有接到
`cmd/codex → internal/cli → appserver/core/tui` 的运行路径**。
不是没写,是「最后一公里」没接。

判定方法(已逐项执行,可复现):
```sh
# 包级孤岛:有无非测试的外部导入者
grep -rl "sqlrush/codexgo/internal/<pkg>" internal cmd --include="*.go" \
  | grep -v _test.go | grep -v "internal/<pkg>/"
# 事件死分支:engine 产生的 EventMsgKind 在 TUI applyEvent 有无 case
# 斜杠命令:chat_bottom.go dispatchSlash / handleSlashOverlay 的覆盖
```

核实结论(2026-06-06):`memories` / `connectors` / `execpolicy` /
`realtimeconv` / `realtimewebrtc` 均为 **0 个非测试外部导入者**;
`mcp` 仅 server 侧接线(`cmd/codex-mcp-server`、`cmd_mcp_server.go`),
client 侧 0 调用;`networkproxy`/MITM 与 `execpolicy` 的接线代码
(`ApplyExecPolicyNetworkRules`)**只存在于测试中**。

---

## A 类 · 交互安全 / 可用性底线 ✅ 已于 v0.3.0 全部接线

| # | 功能 | 状态 |
|---|------|------|
| A1 | **TUI 审批弹窗** | ✅ v0.3.0:`chat_bottom.handleCoreEvent` 路由 `exec_approval_request` / `apply_patch_approval_request` → ApprovalOverlay;决策延迟成 tea.Cmd 回送(修复了与 /model 同类的事件循环死锁) |
| A2 | **自动压缩触发** | ✅ v0.3.0:`runTurn` 在首次 sampling 前调 `maybePreSamplingAutoCompact`,token 总量 ≥ 模型 `auto_compact_token_limit` 时自动内联压缩(port of run_pre_sampling_compact);失败不阻断 turn |
| A3 | **request_user_input / request_permissions 事件** | ✅ v0.3.0:`request_user_input` → RequestUserInputOverlay,`request_permissions` → ApprovalOverlay(permissions);两个 overlay 的同步 sender 死锁一并修复 |

> 附带(v0.3.0):**命令文本折叠**——ExecCell header 把多行/超长命令压成单行
> `▸ … [...]` 摘要(对齐 codex exec.rs),不再刷屏;**Working 状态行 + Worked-for
> 分隔线**(v0.2.3)。

## B 类 · 功能可见性(组件在,UI 不显示)

| # | 功能 | 现状 | 缺什么 | 规模 |
|---|------|------|--------|------|
| B1 | **斜杠命令 ~43 个** | 命令表全在;已接:`/model /new /clear /compact /diff /review /rename /quit /exit` | 其余落到「not supported yet」提示:`/status /resume /fork /init /theme /mcp /skills /memories /plan /goal /agent /permissions /logout /apps /plugins /hooks` 等 | 每个小 |
| B2 | **token_count / context_compacted 事件** | 引擎发出 | TUI 不显示用量进度 / 压缩提示 | 小 |
| B3 | **多 agent 协作事件** | 引擎 + `internal/agentgraph` 已 land 并验证 | TUI 不显示 collab spawn/handoff/waiting、`plan_update`、`thread_goal_updated` 进度 | 中 |
| B4 | **review 模式进出、model_reroute 等状态事件** | 引擎发出 | TUI 无指示 | 小 |

## C 类 · 整包孤岛(子系统已移植,主循环完全不调用)

| # | 子系统 | 行数 | 现状 | 缺什么 | 规模 |
|---|--------|------|------|--------|------|
| C1 | **memories 读/写**(`internal/memories`) | ~918 | 检索+引用+分阶段写入引擎完整,`~/.codexgo/memories_1.sqlite` 已建 | 主循环零调用 —— 记忆不自动生成/注入 | 中 |
| C2 | **MCP 客户端**(`internal/mcp`) | ~3388 | 完整(传输/OAuth/工具调用);**server 已接,client 未接** | core 工具分发不连外部 MCP 服务器 → 配置的 MCP 工具不生效 | 中-大 |
| C3 | **connectors**(`internal/connectors`) | ~1123 | 合并/过滤/缓存完整 | 上下文装配 / 工具选择不调用 | 中 |
| C4 | **execpolicy + networkproxy/MITM**(`internal/execpolicy`、`internal/networkproxy`) | ~2292+ | Starlark 策略 DSL + 网络规则;`ApplyExecPolicyNetworkRules`、MITM 数据路径已写并验证 | 接线**仅在测试中** —— 运行时不评估 exec 策略、不启网络代理 | 中-大 |
| C5 | **realtime 语音**(`realtimeconv` + `realtimewebrtc`) | ~2429 | WebRTC 桥 + 音频环路完整 | 无 session 初始化、无音频 UI;`/realtime` 未接 | 大(含 UI) |

---

## 校准说明(纠正常见误读)

1. **MCP 不是全孤岛**:`mcp-server`(codexgo 作为 MCP 服务器被别人调用)**已接线**;
   孤的是 **client**(codexgo 去连别人的 MCP 工具)。
2. **execpolicy / networkproxy / MITM** 是「移植 + 差分验证完成」,但
   **未接到 CLI 运行路径**(接线函数只有测试调用),不等于运行时已启用。
3. **审批不是完全没有**:headless `codexgo exec` 默认策略可工作;
   缺的是 **TUI 交互弹窗**。
4. 事件/命令的绝对数字不必精确——很多 EventMsgKind 是内部用、无需 UI;
   以上按**实际影响**分类。

---

## 补充顺序建议

- **opendb 之前**:补完 A 类(A1 审批弹窗、A2 自动压缩、A3 输入/权限请求),
  都是「小」规模、引擎现成、低风险;顺带挑 B1 高频命令(`/status` `/resume` `/init`)。
- **C 类按需拉取**:随 opendb 实际依赖决定(如需 MCP 工具或自定义 connector
  时再接对应项),不预先全接。
- 每补一项:升一个补丁版本 → 本地部署 → 验证通过 → 推送打标签
  (见 `docs/DESIGN.zh-CN.md` §4 发布流程)。

## 临时绕过(沙箱写入受阻)

A1 补齐前,如需模型直接写文件,在 `~/.codexgo/config.toml` 设:
```toml
sandbox_mode = "workspace-write"   # 允许写工作目录 + /tmp + $TMPDIR
```
