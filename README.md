# gaia-harness

可配置、可替换的 Agent Harness，参考 pi 的 session、tool loop、streaming 和扩展模型。

核心边界：

- `session`：会话生命周期、消息存储、压缩和 HTTP 服务，不绑定存储实现。
- `sandbox`：工作空间、文件、命令、Python 执行和 skills，不绑定本地或云端执行环境。
- `skills` / `mcp`：统一 Skill 与 MCP Server 接入。
- `agent`：基于 `gaia-ai-provider` 的高可用 agent loop、重试、预算、工具和 middleware。
- `wasmext`：WASM hook/middleware 扩展点，宿主不需要修改即可替换行为。

本项目不把本地路径、数据库或进程执行细节泄漏到 Agent API；这些都由接口和实现注入。
