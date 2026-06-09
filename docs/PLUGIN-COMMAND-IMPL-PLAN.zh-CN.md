# GaussDB 插件命令迁移 —— 详细实施方案(v0.9.0,只读监控全覆盖)

> 配套基线/评估见 [OPENDB-COMMAND-MIGRATION.zh-CN.md](./OPENDB-COMMAND-MIGRATION.zh-CN.md)。
> 本版把评估中**只读的「独立 + 并入」命令全部开发完**,生成 0.9.0。
> 已确认的默认决策:确定性直渲染 + assistant 摘要 · 细粒度工具 · perfsnap 持久化到
> `~/.codexgo/perfsnap/` · 工具名沿用 opendb · 分批验收。

## 1. 范围:23 个新增聚合工具(加现有 14 → 共 37)

| # | 工具 | 合并的 opendb 命令 | 组 |
|---|---|---|---|
| 1 | `sessions` | sessions + activesessions | A |
| 2 | `locks` | locks + blocktree | A |
| 3 | `lwlocks` | lwlocks | A |
| 4 | `longtx` | longtx | A |
| 5 | `vacuum` | vacuum + autovacuum | B |
| 6 | `xid` | xid | B |
| 7 | `bloat` | bloat | B |
| 8 | `space` | space + segments + toasttable | B |
| 9 | `tempusage` | tempusage | B |
| 10 | `hotkey` | hotkey | B |
| 11 | `gsmem` | gsmem + sessionmem | C |
| 12 | `wal` | wal + walsummary + checkpoint + backup | D |
| 13 | `replication` | replication + slots + logicalslots + pubsub | D |
| 14 | `bgworker` | bgworker | D |
| 15 | `resource` | resource | F |
| 16 | `os` | os | F |
| 17 | `users` | users | F |
| 18 | `alert` | alert | F |
| 19 | `perfsnap` | perfsnap | E |
| 20 | `sqlcount` | sqlcount | E |
| 21 | `tableinfo` | tableinfo | H |
| 22 | `indexadvise` | indexadvise | H |
| 23 | `params` | params | G(只读) |

**排除(本版不做)**:⛔写 `kill`/`alter`/`gather` · ⏭️后置 `cmha`/`respool`/`mot`/`jobs`/`ogerr` ·
`dbtop`(交互刷新)· `sql`(任意执行)。

## 2. 统一架构

**复用**:`render_ascii.go`(asciiTable/barLine/dispWidth/truncDisp/pad)、`conn.go`、`schemactx.go`。

**新增文件**(每工具一文件):
```
mon_sessions.go mon_locks.go mon_lwlocks.go mon_longtx.go
mon_vacuum.go mon_xid.go mon_bloat.go mon_space.go mon_tempusage.go mon_hotkey.go
mon_gsmem.go mon_wal.go mon_replication.go mon_bgworker.go
sys_resource.go sys_os.go sys_users.go sys_alert.go
perf_snap.go perf_sqlcount.go
schema_tableinfo.go schema_indexadvise.go admin_params.go
render_panel.go thresholds.go   (公共渲染 + 阈值常量)
```

**每工具三段式**:`registerX` → `collectX(ctx,conn) → struct` → `renderX(struct) string`。

**渲染范式**:数据展示类 → `audience=["user"]` 直渲染(秒出、对齐、数字准)+ `audience=["assistant"]`
结构化摘要(关键数字 + 告警,供模型答追问/串联)。跨维诊断仍由 health 单轮综合。

## 3. 逐工具规格(采样 + 参数 + UI)

### A. 会话 / 锁(4)
- **sessions**(sessions+activesessions):pg_stat_activity → pid/user/db/state/wait_event_type/
  wait_event/query/client_addr/backend_type;衍生 idle=now−state_change;过滤后台 backend_type;
  按 state 聚合;idle-in-tx>10min⚠️。参数 `active`/`all`。UI:state 分布条 + ASCII 表 + 高亮。
- **locks**(locks+blocktree):pg_locks⋈pg_stat_activity;递归 CTE 阻塞链(`kl.granted` 防爆炸)+
  环检测 + 根阻塞者 + 等待时长。参数 `[limit]`。UI:平铺等待表 + ASCII 阻塞树 + "kill 根 pid X"。
- **lwlocks**:pg_thread_wait_status → wait_status/wait_event/lwtid;按事件聚合 waiters 降序。
  参数 `[limit]`。UI:ASCII 表 + 等待分布条。
- **longtx**:pg_stat_activity → xact_start/state/query/backend_xmin;时长分级 >5min⚠️ >30min🔴;
  backend_xmin 非空=阻塞 vacuum。参数 `[limit]`。UI:ASCII 表 + 时长分级 + 阻塞标记。

### B. 空间 / MVCC(6)
- **vacuum**(vacuum+autovacuum):pg_stat_user_tables n_dead/n_live/last_(auto)vacuum;死元组比;
  距触发阈值=threshold+scale×reltuples;进行中 worker(pg_stat_progress_vacuum 若有)。参数 `[limit]`。
  UI:ASCII 表 + 死元组比条 + 超阈未触发🔴。
- **xid**:pg_database datfrozenxid age + Top 高龄表;距 freeze_max_age / 2³¹ 余量。参数 `[limit]`。
  UI:ASCII 表 + 回卷风险条。
- **bloat**:经典估算 SQL(reltuples/relpages+avg_width,免扫表);膨胀比 >20%⚠️。参数 `[阈值][limit]`。
  UI:ASCII 表 + 膨胀比条 + 回收建议。
- **space**(space+segments+toasttable):库级 pg_database_size;表级 pg_total_relation_size 分解
  (表/索引/TOAST)。参数 `[db|limit]`。UI:Panel 库级条 → 表级表 + 占比条。
- **tempusage**:pg_stat_database temp_files/bytes + 实时 spill 会话 + work_mem。UI:表 + spill 高亮。
- **hotkey**:pg_stat_all_tables seq/idx scan、ins/upd/del、HOT、活动评分。参数 `[limit]`。
  UI:表 + 评分条 + seq-heavy/update-heavy 标记。

### C. 内存(1)
- **gsmem**(gsmem+sessionmem):gs_total_memory_detail 按 memorytype;shared_buffers/max_dynamic_memory;
  命中率;gs_session_memory_detail Top 会话。参数 `[limit]`。UI:Panel 分段 + 使用率条 + Top 会话表。

### D. WAL / 复制(3)
- **wal**(wal+walsummary+checkpoint+backup):当前 LSN、归档(降级)、bgwriter checkpoints_timed/req +
  completion_target;请求式占比>30%⚠️。UI:Panel 分段 + 占比⚠️条。
- **replication**(replication+slots+logicalslots+pubsub):pg_is_in_recovery 自适应;主端每备库 LSN +
  lag + sync_state;复制槽 active/retained WAL/catalog_xmin。UI:Panel 每备库 + 延迟条 + 槽告警。
- **bgworker**:pg_thread_wait_status 线程聚合;archiver 失败累计。UI:表 + 告警行。

### F. 系统(4)
- **resource**:max_connections/wal_senders/worker_processes/prepared_xacts vs 当前;>80%⚠️。
  UI:Panel 4 组使用率条。
- **os**:pv_os_run_info(远程 DB 主机 load/mem/cpu);不可用→降级。UI:Panel + 进度条。
- **users**:pg_roles/pg_authid rolsuper/login/valid_until;过期/超权高亮。参数 `[pattern]`。UI:表。
- **alert**:pg_stat_database deadlocks/conflicts/temp_files;仅异常库;severity。UI:表 + severity。

### E. 性能新增(2)
- **perfsnap**:snap 采集多源 → ring buffer 持久化 `~/.codexgo/perfsnap/`;compare delta;list/baseline。
  参数 `snap|compare|list|baseline`。UI:对比表 before/after/Δ + ↑↓。
- **sqlcount**:gs_sql_count 按 user×类型 + avg/max 延迟。参数 `[limit]`。UI:表 + DML/DDL/DCL 占比条。

### G/H. Schema / 参数(3)
- **tableinfo**:pg_attribute+pg_index+pg_constraint+pg_stat_all_tables+size+分区。参数 `<schema.table>`。
  UI:多 section 表。
- **indexadvise**:gs_index_advise + EXPLAIN JSON;与现有索引去重。参数 `<sql>`。UI:建议列表。
- **params**:pg_settings 全量 + category + context(改后是否需重启);无参显常用。参数 `[pattern]`。
  UI:表 + 分组 + 可改性标记。

## 4. 公共 render 增强(render_panel.go)
- `asciiTree(nodes)` —— CJK-safe ASCII 阻塞树(`+-- └--`)
- `panel(sections)` —— 多段 key:value 面板
- `sevTag(level)` —— 统一 OK/⚠️/🔴 分级
- `deltaTable(before,after)` —— perfsnap 对比
- 阈值常量集中到 `thresholds.go`

## 5. 版本 / 测试 / 部署
- 版本 0.8.0 → **0.9.0**;同步 main.go + plugin.json + server instructions。
- 测试:render 表驱动单测 + **CJK 对齐 guard**(每行 dispWidth 相等);collect 真机 E2E **严格限流**
  (openGauss ~10 次失败锁账号,单连接不狂连);`go test -race` + vet + gofmt 全绿。
- 部署:install-plugin-local.sh → 用户重启验收。

## 6. 5 批里程碑
| 批 | 工具 | 验收点 |
|---|---|---|
| 1 | A 会话锁(sessions/locks/lwlocks/longtx) | 阻塞树 + idle/longtx 分级 |
| 2 | B 空间MVCC(6) | 死元组/膨胀/空间/回卷 |
| 3 | C+D 内存+WAL复制(gsmem/wal/replication/bgworker) | Panel 分段 + 主备延迟 |
| 4 | F 系统(resource/os/users/alert) | 使用率条 + 安全巡检 |
| 5 | E+G/H(perfsnap/sqlcount/tableinfo/indexadvise/params) | 快照对比 + 表详情 |

每批 build/test 绿 → 阶段验收 → 下一批;全部完成打 0.9.0。
