# codexgo 数据库能力插件化设计

> 制定日期:2026-06-07 · 对应 codexgo v0.3.1 · 状态:待评审 → 评审通过后开工
> 本文是多轮讨论的最终结论,取代此前的 `OPENDB-INTEGRATION.zh-CN.md`(那版
> 的「driver 进核心 / framework / SDK 化」结论已被推翻,以本文为准)。

---

## 1. 目标与核心原则

把 **opendb 的数据库运维能力**做成 codexgo 的**外接 MCP 插件**;codexgo 与
opendb 两个软件**零运行时调用关系**(opendb 只是被扫描、借鉴的知识来源)。

**四条铁律(贯穿全文):**

1. **codexgo 不依赖任何插件** —— 不装插件,codexgo 仍是完整可用的通用 agent。
2. **领域业务零进 codexgo 核心** —— 数据库的 driver / SQL / 连库 / 巡检逻辑
   全在插件里,codexgo 核心不含任何数据库代码。
3. **插件可独立分发 / 安装 / 迭代** —— 数据库只是第一个插件;未来 OS、应用、
   中间件等各领域插件用同一套通用机制接入。
4. **一份能力,人和 LLM 双入口** —— 人用 `/命令` 直接确定执行,LLM 用工具
   描述自然语言调用,走同一份能力。

**归属判断准则(任何代码该放哪用这把尺子):**
> 「把数据库换成未来的 OS / 中间件插件,这段代码还需要吗?」
> 需要 → codexgo 核心(通用插件设施);只为数据库 → 插件域。

---

## 2. 总体架构

```
codexgo 核心(通用,中立,零领域业务)
├─ 输入框三分类:/命令 · SQL · 自然语言
│     ├ codex 自有命令(/model /clear…)→ codex
│     ├ 插件斜杠命令(/health…)        → 斜杠→MCP tools/call 直调(确定执行)
│     ├ SQL                            → 当前库 server 的 SQL 工具(只读直跑 / 写操作审批)
│     └ 自然语言                        → LLM turn(命中插件工具)
├─ MCP client(连任意 MCP server,说 MCP 协议)
├─ 插件加载 / 安装(读 plugin.json,拉起 MCP server)
├─ 斜杠→tools/call 通用路由 + 命令映射
└─ 活动上下文指针(当前连的是哪个 server + 哪个 target;轻量,不持 DB 连接)

         │ 仅通过 MCP 协议(initialize / tools/list / tools/call)交互
         ▼
数据库插件(每个库一个,完全独立,codexgo 不 import、不感知内部)
  codexgo-db-gaussdb   ← 独立 repo / 独立 server / 独立版本
  codexgo-db-opengauss ← 独立 repo
  codexgo-db-oracle    ← 未来
  codexgo-db-mysql     ← 未来
  codexgo-db-pgsql     ← 未来
```

**关键决策记录(讨论结论):**

- **不自建 framework、不做 SDK 化** —— N 个库 server 之间真正的共性只有「MCP
  协议脚手架」,而那个用**现成的 MCP Go SDK**(第三方库)就解决了;数据库业务
  各库差异大且要独立迭代,各写各的。少量重复用「复制 + 约定」处理,真痛了再
  抽个几十行的小 util,不预设大框架。
- **一个库一个 MCP server** —— 因为不同库迭代节奏差异大,独立 repo/版本/发版
  才能真正独立迭代;商业化单卖也天然干净(卖哪个库给哪个 repo 的 server,零
  牵连、零共享 IP)。
- **driver 在插件,不在 codexgo 核心** —— 每个库 server 自带它的 driver
  (gaussdb 用 `HuaweiCloudDeveloper/gaussdb-go` → `sql.Open("gaussdb",…)`;
  opengauss 用 `jackc/pgx/v5` → `sql.Open("pgx",…)`)。

---

## 3. 数据库插件(每个库一个,独立)

### 3.1 形态:一个独立 repo + 独立 MCP server

```
codexgo-db-gaussdb/                  ← 独立仓库,与 codexgo 无依赖关系
├─ .codexgo-plugin/
│   └─ plugin.json        清单:mcpServers(启动命令)+ commands(斜杠映射)+ version
├─ bin/
│   └─ codexgo-db-gaussdb-<os>-<arch>   MCP server 二进制(按平台编译)
│        └─ 内含:现成 MCP Go SDK(协议层) + gaussdb-go(driver) +
│                 连库 + 各能力的 SQL + 阈值判定 + 渲染 + 领域知识
└─ (无单独 SKILL.md —— 领域知识写进 MCP server 的 instructions / 工具 description)
```

- **MCP 协议层** = 现成 MCP Go SDK(如 `mark3labs/mcp-go` 或官方
  `modelcontextprotocol/go-sdk`),不自己造。
- **业务** = driver + SQL + 阈值 + 渲染,全在二进制里,零进 codexgo。
- **领域知识**(24 项含义、怎么解读)= 走 **MCP 原生载体**:每个工具的
  `description` + server 的 `instructions`(initialize 返回)。**不另配 SKILL.md**
  ——纯 MCP、单一知识来源、可移植到任何 MCP client(讨论结论:叠 SKILL.md 是
  非标准的过度设计)。

### 3.2 工具(tools)——人/LLM 共用的能力单元

每个库 server 暴露同名同语义的工具(便于 codexgo/LLM 跨库一致认知,但实现是
各库自己的 SQL):

| 工具 | 用途 | 只读 |
|------|------|------|
| `db_connect` | 连接到一个 target(host/port/user/…) | — |
| `db_health` | 综合健康检查(各库自己的检查项 + 阈值) | ✅ |
| `db_slowsql` | 慢 SQL TopN | ✅ |
| `db_sessions` | 会话列表 | ✅ |
| `db_locks` | 锁等待 | ✅ |
| `db_space` | 空间用量 | ✅ |
| `db_query` | 执行一条 SQL(只读直跑;写操作返回需审批标记) | 读✅/写→审批 |
| `db_explain` | 执行计划 | ✅ |
| `db_kill` | 终止会话 | ❌写→审批 |

MVP 只做 `db_connect` + `db_health`。

### 3.3 连接与凭证(在插件侧)

- 连接由 **MCP server 自己管**(它持 driver + 连接池);codexgo 不持 DB 连接。
- 连接目标配置:`db-targets.yaml`(host/port/service/user/auth_mode),由插件
  读取(放插件自己的配置目录,或 codexgo 通过 `db_connect` 工具参数传入)。
- 凭证:`auth_mode=prompt` 时 codexgo 通过 `request_user_input`(A3 已接)弹密码,
  作为 `db_connect` 参数传给 server;`auth_mode=save` 用 codexgo keyring 存取。
- 写操作安全:`db_query`/`db_kill` 等写/危险操作,server 返回「需审批」,codexgo
  弹审批门(A1 已接)确认后才真正执行。**只读直跑,写操作审批。**

### 3.4 打包 / 发版 / 安装

- 每个库**独立仓库、独立版本号**(与 codexgo 解耦:codexgo v0.x / gaussdb 插件 v1.y)。
- 打包:各平台 MCP server 二进制 + plugin.json,打成 `.tar.gz`(codexgo 已有
  `bundle_archive` 打包 + 校验)。
- 安装/升级:走 codexgo 插件 marketplace / install 机制(已移植)。
- 商业化单卖:卖某库 = 交付该库 repo 的 server 二进制 + plugin.json,完全独立。

---

## 4. codexgo 核心要新建/接线的(通用,所有领域插件复用)

| # | 工作 | 说明 | 规模 |
|---|------|------|------|
| 1 | 接通 **MCP client** 到运行路径 | 代码已在 `internal/mcp`(client 完整),接到 turn 引擎 + 插件加载;LLM 能用 MCP 工具(UNWIRED C2) | 中 |
| 2 | **斜杠→tools/call 直调** + 命令映射 | 读 plugin.json 的 commands 映射,把 `/health` 注册成斜杠命令 → 直接 `CallTool`(不经 LLM,确定执行) | 中(新建) |
| 3 | **输入框三分类** | /命令 · SQL · 自然语言;SQL → 当前库 server 的 `db_query` 工具 | 中(新建) |
| 4 | **活动上下文指针** | 记「当前连的 server + target」,把 `/health`、SQL 路由到对的 server;footer 显示当前库 | 小 |
| 5 | 插件加载支持 mcpServers + 平台二进制 | 拉起插件声明的 MCP server(stdio,command/args/env);插件机制扩展 | 小 |

这 5 项是 codexgo 一次性通用基础设施,数据库是第一个用户;未来 OS/中间件插件
直接复用,codexgo 核心**不含任何数据库业务**。

---

## 5. 交互流程(端到端)

```
启动:codexgo 读已装插件的 plugin.json → 按 mcpServers 拉起 MCP server(stdio)
      → initialize(拿 server instructions 领域知识)→ tools/list 缓存工具

连库:/dbconn gauss01            ← 斜杠命令(不带参→弹目标选择器,复用 /model picker)
        → codexgo CallTool(gaussdb-server, "db_connect", {target:"gauss01", ...})
        → auth_mode=prompt 则先弹密码(request_user_input)
        → 连接在 server 侧建立;codexgo 记「当前 server+target」,footer 显示 db: gauss01

用:  /health                    ← 斜杠直调:CallTool("db_health") → 渲染(确定,不经 LLM)
     贴一段 SQL                  ← 三分类识别 → CallTool("db_query")(只读直跑/写→审批)
     "为什么这库慢"              ← 自然语言 → LLM turn,命中 db_slowsql 工具(灵活)

批量:/db-inspect-all g1,g2      ← 连多库 → multi-agent 每库并行只读巡检 → 汇总
```

---

## 6. 分阶段实施(feat/opendb-* 分支,每阶段升版本验证)

| 阶段 | 内容 | 验收 |
|------|------|------|
| **0 · MVP** | codexgo 核心:接 MCP client(1)+ 斜杠→tools/call(2)+ 活动指针(4)+ 插件加载(5);**第一个库**:`codexgo-db-gaussdb` 独立 server(现成 MCP SDK + gaussdb-go + `db_connect` + `db_health`)。端到端跑通 `/dbconn` + `/health`。 | `/health` 出健康报告 |
| 1 | 三分类输入(3)+ `db_query`(只读直跑 / 写操作审批)+ 第一批只读工具(slowsql/space/locks/sessions) | 单库交互巡检 + SQL 直输 |
| 2 | `codexgo-db-opengauss`(第二个库,验证「一库一 MCP」范式可复制) | 两库各自独立可用 |
| 3 | 多库批量巡检(multi-agent 汇总) | 批量可用 |
| 4 | explain/kill + 自然语言诊断(借 opendb 规则方法论 → 工具/instructions) | 功能对齐 |
| 后续 | Oracle / MySQL / PgSQL 各加一个独立 repo server | 按需 |

**验证边界(诚实)**:codexgo 这边能交付代码 + 单测(MCP 用 mock / sqlmock)。
**连真实 gaussdb/opengauss 库的端到端验收需要你的环境**(db-targets + 网络
可达 + 凭证)——我这边没真库。

---

## 7. 商业化预留(轻量,不过度)

- 每个库独立 repo/server/版本 → 单卖天然干净(已满足,无需额外架构)。
- **License/授权**:商业化单卖需要 per-server 授权校验(防越权/盗用)。**MVP 不做**,
  但每个库 server 预留一个授权钩子位(先空实现),商业化时填。
- 不预设 framework、不预设 multi-repo 拆分以外的复杂度——简单优先。

---

## 8. 与既有约定的关系

- 复用 codexgo 已接能力:审批门(A1)、用户输入(A3)、keyring、multi-agent、
  /model picker overlay、插件 marketplace。
- 发布遵循 `DESIGN.zh-CN.md` §4:bump VERSION → 本地部署 → 用户验证 → 推送打标签;
  可 `rollback-local.sh` 回退。
- 本设计的偏差(三分类、斜杠→tools/call、MCP client 接线)将登记到 DEVIATIONS.md。

---

## 9. 待你确认的点(评审清单)

1. 总体架构(§2)与四条铁律(§1)认可?
2. 「不要 framework、一库一 MCP、driver 在插件」(§2 决策记录)认可?
3. MCP server 用现成 Go SDK(`mark3labs/mcp-go` 或官方 `go-sdk`)——有无指定偏好?
4. 领域知识走 MCP instructions/description、不另配 SKILL.md(§3.1)认可?
5. MVP 范围(§6 阶段0:gaussdb + db_connect + db_health)认可?
6. 评审通过后即开 `feat/opendb-mvp` 分支开工。
