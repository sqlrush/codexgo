# codexgo 核心 MCP 接线说明 + 验证步骤

> 记录"把外部 MCP server 接入 codexgo 运行时"的核心接线(任务 #26),以及如何
> 端到端验证 gaussdb 插件。

## 接线了什么(已完成,本轮)

此前 codexgo 的 MCP **client** 代码齐全但是"孤岛":运行时从不启动 MCP server,
工具也到不了模型。本轮补齐了**核心链路**(`internal/cli/`):

1. **配置 → 启动**:`loadedConfig.McpServers` 暴露 `[mcp_servers]` 表;
   `buildMcpManager`(`mcp_wiring.go`)在装配期一次性启动所有配置的 MCP server
   (进程级、跨线程共享),逐个 server 启动失败只告警跳过,不拖垮装配。
2. **工具 → 模型**:`Manager.ListAllToolInfos()` 把每个连接的工具(携带 server
   身份)注入每线程的工具路由(`BuiltinToolDeps.Mcp` + `McpTools`),模型即可见
   并调用。
3. **回路由修复**:eager(直接广告)的 MCP 工具此前只带"原始工具名"、丢了
   server 身份,导致 `CallQualifiedTool` 无法解析。改为与 deferred 路径对称——
   模型可见名仍是原始名(如 `db_health`),但用规范名 `mcp__gaussdb__db_health`
   回路由到对应 server。事件展示的 server/tool 切分也一并修正。

> 详见 commit;核心改动在 `internal/cli/assembly.go`、`internal/cli/mcp_wiring.go`、
> `internal/core/tool_executors.go`、`internal/mcp/{manager,namespace}.go`。

## 如何验证(连真实 GaussDB)

### 1. 构建插件二进制

```sh
make -C plugins/codexgo-db-gaussdb build      # 产出 bin/codexgo-db-gaussdb
```

### 2. 在 codexgo 配置里登记该 MCP server

编辑 `~/.codexgo/config.toml`(CODEXGO_HOME 下),加入:

```toml
[mcp_servers.gaussdb]
command = "/绝对路径/codexgo/plugins/codexgo-db-gaussdb/bin/codexgo-db-gaussdb"
args = []
startup_timeout_sec = 20
tool_timeout_sec = 60
```

> 这是"手工登记"路径,用于先验证核心链路。"作为已安装插件自动发现
> `.mcp.json` + `${CODEX_PLUGIN_ROOT}` 替换"是下一步(见下方 待办)。

### 3. 启动 codexgo,让模型调用

启动后,对模型说(示例):

> 连接 GaussDB(host=… port=… user=… 库=…)并做一次健康体检。

模型应依次调用 `db_connect` → `db_health`,你会在工具事件里看到
`gaussdb / db_health` 的调用与结构化结果;随后可继续 `db_slowsql`、`db_sqltune` 等。

### 4. 快速冒烟(不连库也能验证工具已注册)

```sh
make -C plugins/codexgo-db-gaussdb smoke       # initialize + tools/list 握手
go test ./internal/cli/ -run TestBuildMcpManager   # 启动真实插件并断言 12 个工具+回路由名
```

## 待办(#26 余项)

- **插件自动发现**:从已安装插件读取 `.mcp.json`、做 `${CODEX_PLUGIN_ROOT}` 替换
  (codex 约定,兼容 `CLAUDE_PLUGIN_ROOT`/`PLUGIN_ROOT`)、并入有效 `mcp_servers`,
  免去手工登记。需要把插件发现接入运行时(当前装配未加载插件清单)。
- **slash → tools/call**:让 `/health` 这类人类命令确定性直达 MCP 工具(当前模型
  侧已可调用;人类 slash 直达入口待接)。
- 真机验证通过后:升 0.4.0 + 部署(见 [release-workflow])。
