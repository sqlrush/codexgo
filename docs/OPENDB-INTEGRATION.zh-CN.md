# opendb 能力 → codexgo skill 集成方案与蓝图

> 制定日期:2026-06-07 · 对应 codexgo v0.3.1
> 目标:**把 opendb 的数据库运维能力做成 codexgo 的 skill;两个软件零运行时
> 调用关系。** opendb 是知识来源(SQL/阈值/规则/方法论),codexgo 自包含运行。

## 1. 已确认的架构决策(2026-06-07)

| # | 决策 | 选择 |
|---|------|------|
| 命令形态 | opendb 的 / 命令在 codexgo 里的呈现 | **skill 插件包 + 斜杠快捷别名**(一组 DB skill 打包成 `.codexgo-plugin`;高频能力加 `/health` 类斜杠快捷,需 codexgo 加「斜杠→skill」小桥) |
| 连库 | skill 如何连数据库 | **codexgo 独立连接配置(`~/.codexgo` 下,不共享 opendb 文件)+ 单当前库 + 多库批量巡检(借 multi-agent)** |
| SQL 输入 | 输入框承载 SQL | **框架级三分类**(命令 / SQL / 自然语言;SQL 零延迟直执行 → codexgo 需内置 DB 执行通道) |

## 2. 整体架构

```
codexgo(通用 agent,完全不依赖 opendb 软件)
   │
   ├─ 输入框三分类(框架级新增)        ← 决策3
   │     ├ /命令 → codex 命令 或 斜杠→skill 桥
   │     ├ SQL   → DB 执行通道(只读直跑 / 写操作走审批)
   │     └ 自然语言 → LLM turn(命中 DB skill)
   │
   ├─ DB 连接/执行通道(框架级新增,轻量 Go driver)  ← 决策2/3
   │     连接配置 ~/.codexgo/db-targets.yaml;当前库 + 多目标
   │
   ├─ 斜杠→skill 触发桥(框架级新增)   ← 决策1
   │     /health → 触发 db-health skill
   │
   └─ DB skill 插件包(.codexgo-plugin,声明 skills)  ← 决策1·内容主体
        db-health / db-slowsql / db-sessions / db-locks /
        db-space / db-explain / db-top / db-kill / ...
        (SKILL.md 方法论 + 只读巡检逻辑;知识源自 opendb)
```

**安全红利**:只读 SQL 直跑;写操作(DROP/DELETE/DDL/KILL)经 codexgo 审批门
(A1,已接);沙箱(已接)兜底。比 opendb 的纯 report-only 更细。

## 3. opendb 能力清单(扫描结果,skill 化素材)

每个能力 = 参数化 SQL + 阈值判定 + 渲染。SQL 按 DB 类型不同(Oracle `v$/dba_*`、
MySQL `information_schema/performance_schema`、PG `pg_stat_*`、OpenGauss `dbe_perf.*`)。

| 能力 | 用途 | 只读 | 核心数据源(Oracle 例) |
|------|------|------|----------------------|
| **health** | 24 项综合健康检查(OK/WARN/FAIL) | ✅ | v$instance/v$database/dba_tablespace_usage_metrics/v$sysstat/v$librarycache/v$pgastat/v$diag_alert_ext… |
| **slowsql** | 慢 SQL TopN(默认 >1000ms) | ✅ | v$sql(elapsed_time/executions/buffer_gets)+ FTS 检测 |
| **dbtop** | 实时负载/会话/IO/等待 dashboard | ✅ | v$active_session_history/v$system_event |
| **sessions** | 所有会话(状态/活动/等待) | ✅ | v$session |
| **activesessions** | Top 活动会话(ACTIVE,≤50) | ✅ | v$session WHERE status='ACTIVE' |
| **locks** | 锁等待(阻塞者/被阻塞对) | ✅ | v$lock JOIN v$session |
| **space** | 表空间/库空间用量(WARN85/FAIL95) | ✅ | dba_tablespace_usage_metrics |
| **explain** | 执行计划(SQL 或 sql_id) | ✅ | DBMS_XPLAN.DISPLAY/DISPLAY_CURSOR |
| **indexhealth** | 索引质量/碎片 | ✅ | dba_indexes |
| **kill** | 终止会话 | ❌写 | ALTER SYSTEM KILL SESSION(→ codexgo 审批) |

**健康检查 24 项阈值**(可数据化为 skill 的判定层):表空间 W85/F95、temp/undo
W80/F95、FRA W80/F95、连接数 W80/F95、buffer/library hit F<90/W<95、PGA W80/F95、
shared pool F<5/W<15、redo 切换 W10/F30、alert 错误 W1/F10、失效对象 W1/F10…

**规则引擎**:~273 条 Oracle 诊断规则(`internal/oracle/ruleengine/`,纯 Go,
signal→决策树→finding+action)。例:`db file sequential read` avg>20ms→Crit
「存储 I/O 严重异常」→建议增大 DB_CACHE_SIZE。**这是诊断 skill 的知识本体**
(codexgo skill 消费这套方法论,而非重写引擎)。

**Sentinel 48 指标**:会话负载5 / 吞吐3 / 等待延迟6 / 内存缓存4 / 存储5 /
redo归档4 / 锁并发4 / SQL性能4 / 系统模式6;9 种检测策略(阈值/硬顶/趋势/
加速度/复合/容量/漂移/回归/缺失)。→ 巡检 skill 的指标清单。

**连接配置格式**(opendb,codexgo 将用独立但参考此结构的 `~/.codexgo/db-targets.yaml`):
```yaml
connections:
  - name: prod_ora01
    db_type: oracle        # oracle|mysql|postgres|opengauss
    host: 192.168.1.10
    port: 1521
    service: PROD          # Oracle service / 或 sid
    database: prod_db      # MySQL/PG
    user: system
    auth_mode: prompt      # prompt|save|wallet|...
    privilege: sysdba      # Oracle
```
DSN:Oracle `oracle://user:pwd@host:port/service`(go-ora);MySQL
`user:pwd@tcp(host:port)/db`(go-mysql)。

## 4. skill 蓝图(能力 → codexgo skill)

每个 skill = `db-<name>/SKILL.md`(方法论 + 触发说明)+ 判定阈值 + 通过 DB
执行通道跑只读 SQL。打包进一个 `.codexgo-plugin`(声明 skills),斜杠别名可选。

**第一批(纯只读,价值最高)**:`db-health` `db-slowsql` `db-space` `db-locks`
`db-sessions`。
**第二批**:`db-explain` `db-top` `db-activesessions` `db-indexhealth`。
**第三批(写/危险,经审批)**:`db-kill`,以及诊断 skill `db-diagnose`(消费规则引擎方法论 + LLM)。
**批量**:`db-inspect-all`(多目标 + multi-agent 并行只读巡检 + 汇总报告)。

## 5. 需要的 codexgo 框架能力(决策驱动)

| 能力 | 来自决策 | 规模 |
|------|---------|------|
| DB 连接/执行通道(Go driver,连接配置 + 只读/写区分) | 2,3 | 中 |
| 输入框三分类(命令/SQL/自然语言)+ SQL → 执行通道 | 3 | 中 |
| 斜杠→skill 触发桥(`/health` 触发 db-health) | 1 | 小 |
| 多库批量(目标集 + multi-agent 并行巡检汇总) | 2 | 中(借现成 collab) |
| DB skill 插件包(.codexgo-plugin 声明 skills) | 1 | 小(插件机制现成) |

## 6. 分阶段实施(feat/opendb-* 分支,每阶段升版本验证)

| 阶段 | 内容 | 验证 |
|------|------|------|
| 0 · MVP | DB 执行通道骨架(连 1 个库跑只读 SQL)+ `db-health` skill 端到端 | 健康检查可用 |
| 1 | 第一批只读 skill(slowsql/space/locks/sessions)+ 斜杠别名桥 | 单库交互巡检 |
| 2 | 框架级三分类 + SQL 只读直跑 / 写操作审批 | 输入框承载 SQL |
| 3 | 多库批量巡检(db-inspect-all + multi-agent) | 批量可用 |
| 4 | 诊断 skill(规则方法论 + LLM)+ explain/top/indexhealth + 打包成插件 | 功能对齐 + 可分发 |

## 7. 待定技术点(实施前确认)

1. **SQL 执行靠 codexgo 内置 Go driver,还是 skill 脚本调系统 client?**
   推荐内置 Go driver(自包含、可控、支持三分类直执行;代价是 codexgo 引入
   go-ora/go-mysql 等依赖)。
2. **凭证**:连接密码存储(复用 codexgo keyring?prompt?)。
3. **第一批 DB 类型**:先 Oracle + MySQL(opendb 最完整),还是先单一种。

## 8. 风险与原则

- **只读优先**:第一批全只读;写/危险操作一律经审批门,不裸跑。
- **零耦合**:不读 opendb 的文件/进程;连接配置独立在 `~/.codexgo`。
- **增量**:每阶段独立可用、独立验证、独立版本;改坏可回退(见 DESIGN §4)。
