# 多数据库连接与并发巡检 —— 交互设计(仅设计,暂不实现)

> 背景:首批实现已完成"**连接单库**"的 GaussDB MCP 插件。本文设计后续如何
> **同时连接多个数据库**,并借助 codexgo 的**多 agent 并发能力**对一批库发起
> **并发健康巡检(fleet health-check)**,以及与之配套的**连接交互**。
>
> 本文只做设计;不改动现有代码。落地拆分见末尾"分阶段实施"。

参考:[PLUGIN-DB-DESIGN.zh-CN.md](./PLUGIN-DB-DESIGN.zh-CN.md)、
[OPTIMIZATIONS-OVER-OPENDB.zh-CN.md](./OPTIMIZATIONS-OVER-OPENDB.zh-CN.md)

---

## 1. 现状与约束

- 当前 `internal/db/Conn` 持有**单个** `*sql.DB`(`db_connect` 会替换上一个连接)。
  → 同一 MCP 进程内若被多个 agent 并发用于不同库,会相互覆盖,**不可并发多库**。
- 一库一 MCP:GaussDB / openGauss / oracle / mysql / pgsql 各自独立插件。
- 密码不落盘明文:codexgo 已有 keyring;敏感信息走 keyring 引用。
- codexgo 已有多 agent / collab 引擎,可并发派生子 agent。

**核心问题**:多库并发巡检,连接如何组织?有两条路线。

---

## 2. 两种连接模型(关键决策)

### 方案 A:单进程 + 命名连接注册表(推荐)

把 `Conn`(单连接)升级为 `Registry`(`map[label]*sql.DB`,RWMutex 保护)。

- `db_connect` 改为**注册一个命名连接**(label 唯一);可重复调用注册多库。
- 每个工具新增**可选** `connection` 参数(label);省略时用"当前默认连接"
  (保持单库调用的向后兼容)。
- 不同 label 各自独立的 `*sql.DB` 连接池,**进程内天然支持并发查询**。

```
                ┌──────────── codexgo (主体 + 多 agent) ────────────┐
                │  agent#1 ──db_health(connection=prod-gauss01)──┐   │
                │  agent#2 ──db_health(connection=prod-gauss02)──┤   │
                │  agent#3 ──db_health(connection=uat-gauss03) ──┤   │
                └────────────────────────────────────────────────┼──┘
                                                                  ▼
                       ┌──────── 1 个 gaussdb MCP 进程 ────────┐
                       │ Registry{ prod-gauss01: *sql.DB,      │
                       │           prod-gauss02: *sql.DB,      │
                       │           uat-gauss03 : *sql.DB }     │
                       └───────────────────────────────────────┘
```

**优点**:与现有"一库一 MCP"完全兼容(同种库的多实例放一个进程);连接复用;
实现改动小(Conn → Registry,工具加一个可选参数)。
**缺点**:同种库的多实例共享一个进程,单进程崩溃影响该种库的全部目标(可接受,
巡检为只读、可重试)。

### 方案 B:多进程,一目标一进程

codexgo 为每个目标启动一份插件进程(各自单连接)。
**优点**:故障隔离最彻底。
**缺点**:MCP 配置当前是**静态**的,"按目标动态拉起参数化实例"需要 codexgo 侧
新增动态 spawn 能力,改动大;进程数 = 目标数,资源开销高。

> **结论**:首个多库版本采用**方案 A**(命名连接注册表)。方案 B 留作超大规模
> (上百目标)或强隔离需求时的演进选项。

---

## 3. 连接交互设计(用户如何"连多个库")

### 3.1 目标清单(inventory)

新增**库存清单文件**:`~/.codexgo/db-targets.toml`(纯结构,**不含明文密码**)。

```toml
[[target]]
label    = "prod-gauss01"
kind     = "gaussdb"          # 决定用哪个 MCP 插件
host     = "10.0.0.11"
port     = 8000
user     = "dbmon"
database = "postgres"
sslmode  = "require"
secret   = "keyring:codexgo/db/prod-gauss01"   # 密码取自 keyring 的引用
group    = "prod"             # 可选:分组,便于"对 prod 全量巡检"

[[target]]
label = "prod-gauss02"
kind  = "gaussdb"
host  = "10.0.0.12"
port  = 8000
user  = "dbmon"
secret = "keyring:codexgo/db/prod-gauss02"
group = "prod"
```

- **密码**:`/dbconn add` 时通过 `request_user_input`(已接线的 A3 能力,密码态
  输入不回显)收集,写入 keyring,清单里只存 `keyring:` 引用。
- **校验**:加载时对每个 target 做 schema 校验(label 唯一、port 1–65535、
  kind 在已安装插件集合内)。

### 3.2 `/dbconn` 命令族(人类入口)

| 命令 | 行为 |
|------|------|
| `/dbconn add` | 交互式录入一个目标:host/port/user/db/sslmode + 密码(keyring),test 通过后写清单 |
| `/dbconn list` | 列出清单(label/kind/host/group/最近一次 test 状态) |
| `/dbconn test <label\|group\|--all>` | 并发探活(仅 ping),返回可达性矩阵 |
| `/dbconn use <label>` | 设为"当前默认连接"(单库操作时省略 connection 参数即用它) |
| `/dbconn rm <label>` | 删除清单项 + 清理 keyring |

### 3.3 选择交互(TUI)

`/dbconn` 与巡检命令在需要"选哪些库"时,弹出**多选列表**(复用 codexgo 现有
overlay 组件):按 group 折叠,空格多选,回车确认。也支持直接传参跳过交互:
`/fleet-health --group prod`。

---

## 4. 并发巡检编排(多 agent)

### 4.1 新命令:`/fleet-health`

```
/fleet-health [--group prod | --all | label1,label2,...] [--concurrency 8]
```

编排流程(由 codexgo 多 agent 引擎驱动):

```
协调者(coordinator)
  ├─ 解析目标集合(来自清单/分组/显式列表)
  ├─ 并发派生 N 个子 agent(受 --concurrency 限流,默认 = min(8, CPU-2))
  │     每个子 agent:
  │        db_connect(label, kind 对应的 MCP)  →  db_health(connection=label)
  │        → 返回结构化 HealthReport{score, grade, items, counts}
  ├─ 汇总:按 score 升序排列(最差在前),聚合 counts
  └─ 产出 fleet 报告:总览(N 库 / 危险 x / 警告 y)+ 明细表 + Top 风险项
```

### 4.2 为什么天然契合现有结构

- `db_health` **已返回结构化 `HealthReport`**(score/grade/items),无需改动即可被
  协调者跨库聚合、排序、出"健康分排行榜"。这正是
  [优化点二](./OPTIMIZATIONS-OVER-OPENDB.zh-CN.md)(加权健康分)的红利:**单库分数
  天生可在多库间比较与汇总**。
- 子 agent 之间相互独立,失败一个不影响其余(与逐项容错同构)。
- 限流:`--concurrency` 控制同时连库数,避免对生产端造成连接风暴。

### 4.3 跨库聚合报告(示意)

```
Fleet Health — group=prod (6 库)        危险 1 · 警告 2 · 良 3
┌────────────────┬───────┬──────┬───────────────────────────────┐
│ target         │ score │ grade│ 首要风险                       │
├────────────────┼───────┼──────┼───────────────────────────────┤
│ prod-gauss04   │   48  │ 危险 │ 缓存命中 91% (FAIL) · 连接 96% │
│ prod-gauss02   │   72  │ 警告 │ idle-in-tx 14 · 死元组 320k    │
│ prod-gauss05   │   78  │ 警告 │ xid 回卷 61%                   │
│ prod-gauss01   │   88  │ 良   │ 活动会话偏高                   │
│ …              │       │      │                               │
└────────────────┴───────┴──────┴───────────────────────────────┘
建议优先处理 prod-gauss04(缓存命中过低 + 接近连接上限)。
```

---

## 5. 安全与限流

- **只读**:巡检全程只读;`/dbconn test` 仅 ping。
- **密码不落盘**:仅 keyring 引用进清单;子 agent 拿到的是连接句柄,不传播明文。
- **连接风暴防护**:每个目标连接池 `MaxOpenConns` 小(现为 4);`--concurrency`
  再限制同时巡检的目标数。
- **审批**:对生产 group 的批量操作可走已接线的审批门(A1)二次确认。

---

## 6. 分阶段实施(后续落地,非本次)

1. **M1 连接注册表**:`Conn` → `Registry`(map+RWMutex);工具加可选 `connection`
   参数;`db_connect` 注册命名连接。单库行为保持兼容。
2. **M2 清单 + keyring**:`~/.codexgo/db-targets.toml` schema + 加载校验;
   `/dbconn add/list/use/rm/test`;密码走 keyring。
3. **M3 并发巡检**:`/fleet-health` 协调者 + 子 agent 派生 + 限流 + 跨库聚合报告。
4. **M4 多选 TUI**:目标选择 overlay;分组折叠。

> 每个里程碑独立可验证;M1 是地基(其余都依赖命名连接)。
