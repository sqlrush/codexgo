# codexgo 核心 MCP 接线说明 + 验证步骤

> 记录"把外部 MCP server 接入 codexgo 运行时"的核心接线(任务 #26),以及如何
> 端到端验证 gaussdb 插件。

## 接线了什么(已完成)

此前 codexgo 的 MCP **client** 代码齐全但是"孤岛":运行时从不启动 MCP server,
工具也到不了模型。现已补齐**核心链路 + 插件自动发现**(`internal/cli/`):

1. **配置 → 启动**:`loadedConfig.McpServers` 暴露 `[mcp_servers]` 表;
   `buildMcpManager`(`mcp_wiring.go`)在装配期一次性启动所有 MCP server
   (进程级、跨线程共享),逐个 server 启动失败只告警跳过,不拖垮装配。
2. **工具 → 模型**:`Manager.ListAllToolInfos()` 把每个连接的工具(携带 server
   身份)注入每线程的工具路由(`BuiltinToolDeps.Mcp` + `McpTools`),模型即可见
   并调用。
3. **回路由修复**:eager(直接广告)的 MCP 工具此前只带"原始工具名"、丢了
   server 身份,导致 `CallQualifiedTool` 无法解析。改为与 deferred 路径对称——
   模型可见名仍是原始名(如 `health`),但用规范名 `mcp__gaussdb__health`
   回路由到对应 server。事件展示的 server/tool 切分也一并修正。
4. **插件自动发现**(`plugin_mcp.go`):从已启用、已安装的插件(`[plugins]` 表 +
   插件 store cache)读取其 `.codex-plugin/plugin.json` → `.mcp.json`,做
   `${CODEX_PLUGIN_ROOT}` 替换(兼容 `${CLAUDE_PLUGIN_ROOT}`/`${PLUGIN_ROOT}`),
   解析为 MCP server 并入有效集合。配置的 `[mcp_servers]` 在重名时覆盖插件项
   (`effectiveMcpServers`)。
5. **slash → tools/call(人类确定性入口)**:新增 app-server 方法 `mcp/listTools`
   + `mcp/callTool`(codexgo 扩展,不影响 codex 协议),把进程级 MCP manager 作为
   gateway 暴露给 Processor。TUI 启动时拉取工具清单,输入 `/<工具名>`(如
   `/health`、`/slowsql {"threshold_ms":500}`)即**绕过 LLM 直接调用**该 MCP
   工具并渲染结果。命令集**完全来自已连接的 MCP 工具**,core 不硬编码任何 DB 命令
   (零耦合)。

> 核心改动:`internal/cli/{assembly,mcp_wiring,plugin_mcp,config_load}.go`、
> `internal/core/tool_executors.go`、`internal/mcp/{manager,namespace}.go`、
> `internal/appserverproto/mcp_tools_codexgo.go`、`internal/appserver/{assembly,mcp_tools}.go`、
> `internal/tui/{engine,model,chat,mcp_slash}.go`。

## 如何验证(连真实 GaussDB)

### 方式 B:作为已安装插件自动发现(推荐)

一条命令构建并安装到插件 store,再在 `[plugins]` 里启用即可,无需手工写
`[mcp_servers]`:

```sh
scripts/install-plugin-local.sh
```

然后在 `$CODEXGO_HOME/config.toml`(默认 `~/.codexgo`)加入:

```toml
[plugins."codexgo-db-gaussdb@local"]
enabled = true
```

启动 codexgo 后,运行时会自动发现该插件的 `gaussdb` MCP server(命令路径由
`${CODEX_PLUGIN_ROOT}` 解析为安装根下的 `bin/codexgo-db-gaussdb`)。

> 注意:`enabled = true` 需显式写出。重装/更新插件后重跑安装脚本即可。

### 方式 A:手工登记 [mcp_servers](无需安装到 store)

```sh
make -C ~/opendbx-mcp-for-codex build
```

```toml
[mcp_servers.gaussdb]
command = "~/opendbx-mcp-for-codex/bin/codexgo-db-gaussdb"
args = []
startup_timeout_sec = 20
tool_timeout_sec = 60
```

### 两种调用方式

**A) 模型驱动**:对模型说"连接 GaussDB(host/port/user/库)并做一次健康体检",
模型会依次调 `connect` → `health`,事件里能看到结构化结果。

**B) 人类 slash 直达(确定性,不走 LLM)**:直接输入工具名 slash:

```
/connect {"host":"...","port":8000,"user":"...","password":"...","database":"postgres"}
/health
/slowsql {"threshold_ms":500}
```

无参工具(如 `/health`)直接回车;带参工具传 JSON 对象。结果以 notice 形式
渲染在命令下方。命令集来自已连接的 MCP 工具(`/<工具名>`),无需任何 codexgo 侧
硬编码。

> 说明:动态命令暂未进入 `/` 自动补全弹窗(避免改动枚举式 slash 系统),输入即生效。

### 快速冒烟(不连库也能验证)

```sh
make -C ~/opendbx-mcp-for-codex smoke              # initialize + tools/list 握手
go test ./internal/cli/ -run 'TestBuildMcpManager|TestDiscoverPlugin|TestEffectiveMcp'
```

## 待办

- 真机验证通过后:升 0.4.0 + 部署(见 [release-workflow])。
- (可选增强)动态命令进入 `/` 自动补全弹窗;raw 名跨 server 冲突时按 server 消歧。
