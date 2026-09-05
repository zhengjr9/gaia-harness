# gaia-harness

可配置、可替换的 Agent Harness，参考 pi 的 session、tool loop、streaming 和扩展模型。

核心边界：

- `session`：会话生命周期、消息存储、压缩和 HTTP 服务，不绑定存储实现。
- `sandbox`：工作空间、文件、命令、Python 执行和 skills，不绑定本地或云端执行环境。
- `skills` / `mcp`：统一 Skill 与 MCP Server 接入。
- `agent`：基于 `gaia-ai-provider` 的高可用 agent loop、重试、预算、工具和 middleware。
- `wasmext`：WASM hook/middleware 扩展点，宿主不需要修改即可替换行为。

本项目不把本地路径、数据库或进程执行细节泄漏到 Agent API；这些都由接口和实现注入。

HTTP API：

- `POST /v1/sessions` 创建隔离会话
- `GET /v1/sessions/{id}` 读取会话和消息
- `POST /v1/sessions/{id}/messages` 写入消息
- `POST /v1/sessions/{id}/run` 写入用户消息并运行完整 agent loop

pi 原生协议：

- `@earendil-works/pi-protocol` 的长度前缀 CBOR 消息
- 通过 WebSocket 的 `/v1/pi` HTTP Upgrade 建立双向字节传输
- 前端使用 `@earendil-works/pi-client`，不使用自定义 JSON/SSE envelope

启动命令（workspace 默认就是启动命令执行时的当前目录）：

```bash
cd /Users/zhengjiarui/go/src/github.com/gaia-harness
GAIA_API_KEY='your-ark-api-key' \
GAIA_BASE_URL='https://ark.cn-beijing.volces.com/api/plan/v3' \
GAIA_PROVIDER=ark \
GAIA_MODEL=deepseek-v4-flash \
GAIA_SANDBOX_MODE=local \
go run ./cmd/gaia-harness -addr 127.0.0.1:8080
```

Linux 机器可省略 `GAIA_SANDBOX_MODE=local`，默认使用 bwrap。创建 session 时不传 `cwd` 会使用服务启动目录；也可以在 pi `create` command 中显式传 `cwd`。

使用 pi-tui 交互（另开终端）：

```bash
cd /Users/zhengjiarui/go/src/github.com/pi
GAIA_AGENT_URL=http://127.0.0.1:8080 \
PI_OFFLINE=1 \
node node_modules/tsx/dist/cli.mjs packages/coding-agent/examples/gaia-full-tui.ts
```

这会启动 `@earendil-works/pi-coding-agent` 的完整 `InteractiveMode`，保留 pi 原生的 header、footer、编辑器、快捷键、命令、主题、扩展 UI 和工具渲染；底层通过 `@earendil-works/pi-client` 原生协议连接 gaia-harness。`process.cwd()` 会作为 session 的 cwd/workspace。Node 需要 >=22.19.0。

Linux 默认使用 bubblewrap；没有 bwrap 的开发机可设置 `GAIA_SANDBOX_MODE=local` 做功能 smoke test。生产环境不要把 local 模式当作隔离边界。

WASM 扩展 ABI：导出 `memory`、`alloc(i32) -> i32`、`hook(i32, i32) -> i64`；返回值高 32 位为输出指针，低 32 位为输出长度。
