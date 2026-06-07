# codexgo 沙箱与安全管控接入状态

> 核实日期:2026-06-07 · 对应 codexgo v0.3.0
> 口径:逐项验证是否**端到端接到执行路径**(命令真正运行时是否生效),
> 而非仅「代码是否存在」。证据为 grep 实测(见各行验证点)。

## 结论

**不是 100%。** 安全的核心主链路(沙箱执行 + 审批 + 升权 + 权限)已生效,
但有两个明确缺口:**网络管控未接**、**命令级 execpolicy 未接**。

---

## ✅ 已接入运行路径(真生效)

| 能力 | 验证点 |
|------|--------|
| 沙箱执行(macOS) | `cli/exec_service.go` → `runSandboxedExec` → `sandbox.Spawn` → `/usr/bin/sandbox-exec -f <policy> -D… -- cmd`,真起 Seatbelt 子进程 |
| 沙箱按回合分档 | read-only / workspace-write → 进沙箱;danger-full-access → none(`SandboxType != None` 才走沙箱路径) |
| 执行前审批 | `core/unified_exec_executor.go` → `Session.RequestCommandApproval`,按 `AskForApproval` 策略发 `ExecApprovalRequest` 事件 |
| 审批 UI 弹窗 | v0.3.0(A1):`chat_bottom.handleCoreEvent` 路由审批事件 → ApprovalOverlay,交互模式可 y/a/n |
| 沙箱拒绝 → 升权重试 | `maybeEscalateUnifiedExec` → `resolveSandboxEscalation` → `ForceSandboxNone` 重试 |
| 权限请求 | v0.3.0(A3):`request_permissions` → ApprovalOverlay(permissions) |
| AskForApproval × sandbox 决策矩阵 | `core/approval_decision.go`(on-request / on-failure / never / untrusted 全在) |

## ⚠️ 缺口 1:网络管控未接(运行时不生效)

- `internal/networkproxy`(HTTP 代理 + MITM CONNECT 终止 + 托管 CA)与
  `execpolicy` 的网络规则:**整包实现 + 差分验证完成,但 assembly/exec
  路径从不导入、从不启动代理**。
- 验证:`grep StartProxy|NewProxy|networkproxy.|ApplyExecPolicyNetworkRules`
  在 cli/core/appserver/execserver/unifiedexec 下**无非测试调用**。
- **后果**:子进程网络访问不受 codexgo 这层管控(无域名 allow/deny、无
  流量拦截/审计)。Seatbelt/landlock 提供基础网络限制,但 codex 那套
  「按域名放行 / MITM 注入凭证」在 codexgo 运行时是关闭的。

## ⚠️ 缺口 2:execpolicy(命令级 Starlark 策略)未接

- `internal/execpolicy`(命令 allow/deny 的 Starlark DSL,~2292 行):
  **运行时不评估**。
- 验证:无 cli/core/appserver/unifiedexec 的非测试导入。
- **后果**:命令能否执行只靠「AskForApproval + 沙箱」,**没有细粒度的
  「这条命令直接禁/直接放」策略层**。

## 平台覆盖

| 平台 | 沙箱后端 | 状态 |
|------|----------|------|
| macOS | Seatbelt(`/usr/bin/sandbox-exec`) | ✅ 完整 |
| Linux | landlock + seccomp BPF 网络过滤(user/mount/pid namespace + ruleset) | ✅ 有完整后端(`backend_landlock_linux.go`) |
| Windows | — | ❌ 未实现 |

---

## 对 opendb 的影响(opendb 卖点 = LLM 只读、变更人工执行)

- **够用**:沙箱(macOS/Linux)+ 审批门 + 权限请求 + 升权确认——「执行前
  拦截、人工放行」主链路已生效,直接支撑 opendb 的只读+审批模型。
- **硬伤(须补)**:若要严格保证「Agent 不能私自联网外发数据库内容」,
  **缺口 1(网络管控)是硬伤**——目前这层关闭,需接 `networkproxy` 才能
  在运行时做网络隔离/审计。
- **建议补**:**缺口 2(execpolicy)** 能用策略直接禁掉 `DROP/DELETE/
  TRUNCATE` 类命令,而非只依赖审批弹窗——对「只读」是更硬的保障。

## 待办(UNWIRED C4)

| 项 | 风险 | 建议顺序 |
|----|------|----------|
| execpolicy 命令级策略接线 | 低(纯本地评估,无平台依赖) | 先做 |
| networkproxy 运行时启动 + 子进程环境注入 | 中(涉及子进程 env) | 后做 |

两项引擎均已就绪(移植+差分验证完成),属「接线即得」,不需重新开发。
