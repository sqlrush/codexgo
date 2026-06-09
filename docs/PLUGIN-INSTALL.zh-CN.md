# 安装并启用数据库插件(MCP 服务器)

> 说明 `scripts/install-plugin-local.sh` 做什么,以及在 codexgo 里启用插件 MCP
> 服务器的两种方式。配套设计见 [MCP-WIRING.zh-CN.md](./MCP-WIRING.zh-CN.md)。

## 概念

- **插件(plugin)**:一个能力包(`.codex-plugin/plugin.json` 声明)。本仓库的
  `plugins/codexgo-db-gaussdb` 这个插件带的能力就是**一个 MCP 服务器**(stdio
  二进制 `bin/codexgo-db-gaussdb`)。
- **MCP 服务器**:一个独立子进程,codexgo 启动时按需把它拉起,用 stdin/stdout 的
  JSON-RPC 通信,它对外暴露 `health`、`slowsql` 等工具。
- "安装插件"只是让 codexgo **知道有这个插件**并能找到它的二进制;**不连网、不改
  系统、可逆**。真正启动 MCP 服务器发生在你启动 codexgo 时。

## 方式 B(推荐):安装到插件 store,自动发现

一条命令构建并安装到 codexgo 的本地插件缓存:

```sh
scripts/install-plugin-local.sh
```

它做三件事:

1. `make -C ~/opendbx-mcp-for-codex build` —— 构建出 `bin/codexgo-db-gaussdb`。
2. 把整个插件包(`.codex-plugin/plugin.json` + `.mcp.json` + `bin/`)拷贝到
   `$CODEXGO_HOME/plugins/cache/local/codexgo-db-gaussdb/local/`
   (默认 `~/.codexgo`;`local` 是 marketplace 名,第二个 `local` 是版本号)。
3. 打印需要加到配置的 `[plugins]` 段。

然后在 `$CODEXGO_HOME/config.toml` 启用(`enabled = true` 需显式写出):

```toml
[plugins."codexgo-db-gaussdb@local"]
enabled = true
```

启动 codexgo 后,运行时会:解析该插件安装根 → 读 `.mcp.json` → 把
`${CODEX_PLUGIN_ROOT}` 替换为安装根 → 以子进程拉起 `bin/codexgo-db-gaussdb`,其
工具即可被模型调用、也可用 `/health` 这类 slash 命令直达。

**卸载**:`rm -rf ~/.codexgo/plugins/cache/local/codexgo-db-gaussdb`,并删掉
config.toml 里的 `[plugins."codexgo-db-gaussdb@local"]`。

## 方式 A:不安装,直接在 config 里登记 MCP 服务器

适合快速验证,免去 store:

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

两种方式效果相同——codexgo 启动时都会把该二进制作为 MCP 服务器拉起。重名时
`[mcp_servers]` 覆盖插件发现的同名 server。

## 验证连通(不连库也行)

```sh
make -C ~/opendbx-mcp-for-codex smoke    # initialize + tools/list 握手
```

连真实 GaussDB 后,可用 `/help` 查看全部命令,或对模型说"体检一下"。
