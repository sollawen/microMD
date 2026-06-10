# opencode 扩展机制调研报告

> 调研日期：2026-06-10
> 目的：为 Editor-Agent Bridge 的 **opencode 接收端** 选定实现方案
> 调研依据：本地已安装的 `@opencode-ai/plugin@1.4.10` SDK 类型定义、`@opencode-ai/sdk@1.4.10`、真实插件 `@different-ai/opencode-browser@4.6.0` 源码、`https://opencode.ai/config.json` schema、opencode 官方文档（/docs/acp、/docs/server、/docs/plugins、/docs/tools）。
> 一句话结论：**用 Plugin（TUI 插件为主）实现接收端；ACP 不适用；我们的 socket 方案无需改动。**（最初曾推荐 server 插件，后修正为 TUI 插件为主，见 §6.5。）

---

## 0. TL;DR（给赶时间的人）

| 问题 | 结论 |
|------|------|
| opencode 有几种扩展机制？ | 三种：**Plugin**（TS/JS 模块，hook + 工具）、**MCP**（标准协议 server，给 LLM 加能力）、**ACP**（让 opencode 当编辑器内嵌 agent 的协议） |
| ACP 能替代我们自建 socket 吗？ | **不能。** ACP 是 stdio 子进程模型（编辑器 spawn `opencode acp`），方向和生命周期都和我们相反。详见 §4。 |
| opencode 接收端用哪个？ | **Plugin 的 TUI 插件**。它走 TUI 原生输入通道发送，server 完全无感；UI/slash 能力也是原生最强。详见 §6、§7。 |
| 对 `editor-agent-bridge-plan.md` 的影响？ | **协议层零改动**。opencode 接收端与 pi 接收端实现高度相似（都是 TS），可大量复用。详见 §7。 |

---

## 1. 三种扩展机制一览

| 机制 | 定位 | 跑在哪 | 我们接收端能用吗 |
|------|------|--------|------------------|
| **Plugin** | 在 opencode 进程内挂 hook、加工具、改行为、做 UI。最灵活。 | opencode 进程内（server 进程 或 TUI 进程） | ✅ **主力**（server 插件） |
| **MCP** | 标准 MCP server，给 LLM 提供 tools/resources/prompts。独立进程。 | 独立子进程，opencode 作为 MCP client 连接它 | ❌ 不适合（它是"给 LLM 加能力"，不是"接收外部推送"） |
| **ACP** | opencode 作为 **ACP server**，让编辑器（Zed/JetBrains…）内嵌它。 | opencode 作为编辑器的 stdio 子进程 | ❌ 方向相反，不适用（详见 §4） |

核心区别一句话：
- **Plugin** = "我（opencode）内部长出一个钩子"。
- **MCP** = "我（opencode）去连一个外部能力源喂给我的 LLM"。
- **ACP** = "我（opencode）被一个编辑器当 agent 用"。

---

## 2. Plugin 机制深入

> 一手依据：`/Users/sollawen/.config/opencode/node_modules/@opencode-ai/plugin/dist/index.d.ts`、`tool.d.ts`、`tui.d.ts`、`shell.d.ts`、`example.js`；官方 `/docs/plugins`。

### 2.1 两类插件入口（重要）

opencode 的架构是 **client/server 分离**的：`opencode`（TUI 模式）= 一个 server 进程 + 一个 TUI client 进程；`opencode serve` = 纯 server。因此插件分两类，**各自跑在不同进程**：

```typescript
// index.d.ts
export type PluginModule = { id?: string; server: Plugin; tui?: never };   // server 端
export type TuiPluginModule = { id?: string; tui: TuiPlugin; server?: never }; // TUI 端
```

一个**模块只能二选一**（`tui?: never` / `server?: never` 互斥）。

| | **server 插件** (`Plugin`) | **TUI 插件** (`TuiPlugin`) |
|---|---|---|
| 跑在 | opencode server 进程（常驻，只要 opencode 开着就在） | TUI 进程（TUI 退出就没） |
| 入口签名 | `(input: PluginInput, options?) => Promise<Hooks>` | `(api: TuiPluginApi, options?, meta?) => Promise<void>` |
| 拿到什么 | `input.client`（OpencodeClient）、`input.serverUrl`、`input.$`（BunShell）、`input.project`、`input.directory`、`input.worktree` | `api.*`（command/route/ui/state/theme/event/client/lifecycle…） |
| 适合做 | 起后台监听、拦截 hook、调 client API、给 LLM 加工具 | 注册 slash 命令、渲染 UI widget、渲染对话框 |

**对我们的影响**：起 Unix socket server 应在 **server 插件**里（server 进程常驻）；slash 命令和 widget 在 **TUI 插件**里。但 server 插件能通过 `client.tui.*` 反向控制 TUI（showToast/appendPrompt/submitPrompt），所以**只写 server 插件也能覆盖大部分需求**，见 §6。

### 2.2 Server 插件：Hooks 全貌

> 出处：`index.d.ts` 的 `export interface Hooks`。

一个 server 插件就是返回一组 hooks 的函数。Hooks 按功能分组：

**A. 通用事件 / 配置 / 工具 / 认证 / provider**
| Hook | 作用 |
|------|------|
| `event` | 通用事件回调，收到 `Event`（SSE 事件的镜像，见 §2.5） |
| `config` | 修改 opencode 配置 |
| `tool: { [name]: ToolDefinition }` | **注册自定义工具**（LLM 可调用） |
| `auth` / `provider` | 自定义认证流 / 注册 provider 模型 |

**B. 消息与 LLM 调用拦截**（对我们最关键）
| Hook | 触发时机 | 能干什么 |
|------|---------|---------|
| `chat.message` | 收到一条新（用户）消息时 | 拿到 sessionID/message/parts；**可在此感知"用户发话了"** |
| `chat.params` | 发给 LLM 前 | 改 temperature/maxTokens 等**参数** |
| `chat.headers` | 发给 LLM 前 | 改 HTTP headers |
| `experimental.chat.messages.transform` | 发给 LLM 前 | **改整条消息列表**（`output.messages`）——注入上下文就用它 |
| `experimental.chat.system.transform` | 发给 LLM 前 | **改 system prompt**（`output.system: string[]`） |
| `experimental.session.compacting` | 压缩前 | 定制压缩 prompt |
| `experimental.text.complete` | 文本补全 | 内联补全 |
| `tool.definition` | 注册工具后 | 改工具的 description/parameters |

**C. 工具执行 / 命令 / 权限**
| Hook | 作用 |
|------|------|
| `tool.execute.before` / `tool.execute.after` | 工具执行前后（可改 args / 改结果） |
| `command.execute.before` | **命令执行前**（可注入 parts）——可借实现"伪命令" |
| `permission.ask` | 权限询问时（可自动 allow/deny） |
| `shell.env` | 设置子进程环境变量 |

> **关键**：`experimental.chat.messages.transform` 和 `chat.message` 组合，等价于 pi 的 `before_agent_start` + `context` 注入。这是我们实现"用户下次发言时带上待用上下文"的落点。

### 2.3 自定义工具 API（tool.d.ts）

> 出处：`tool.d.ts`；真实用例见 `@different-ai/opencode-browser/dist/plugin.js`。

```typescript
import { tool } from "@opencode-ai/plugin";

const myTool = tool({
  description: "做某件事",
  args: { foo: tool.schema.string().describe("参数说明") },  // tool.schema === zod
  async execute(args, ctx) {                                   // ctx: ToolContext
    // ctx.sessionID / messageID / agent / directory / worktree / abort
    // ctx.metadata({ title, metadata })   // 附加元信息
    // ctx.ask({ permission, patterns, always, metadata })  // 申请权限
    return "结果文本";   // 或 { output, metadata }
  },
});
```

最小插件（来自 SDK 自带 `example.js`）：

```javascript
import { tool } from "@opencode-ai/plugin";
export const ExamplePlugin = async (_ctx) => ({
  tool: {
    mytool: tool({
      description: "This is a custom tool",
      args: { foo: tool.schema.string().describe("foo") },
      async execute(args) { return `Hello ${args.foo}!`; },
    }),
  },
});
```

真实大型范例：`@different-ai/opencode-browser` 是个 server 插件，`export default plugin`，返回 `{ tool: { browser_debug, browser_navigate, browser_query, ... 数十个工具 } }`，并用 `.opencode/skill/browser-automation/SKILL.md` **自带一个 skill**（插件可携带 skill，opencode 会自动加载）。

### 2.4 TUI 插件：TuiPluginApi 全貌

> 出处：`tui.d.ts`。能力极强，用 SolidJS（`@opentui/solid`）渲染。

`TuiPluginApi` 主要命名空间：

| 命名空间 | 能力 |
|----------|------|
| `api.command` | **`register(cb => TuiCommand[])`**（含 `slash:{name,aliases}` → 斜杠命令）、`trigger(value)`、`show()` |
| `api.route` | `register(routes)` 自定义页面、`navigate(name)` |
| `api.ui` | `Dialog` / `DialogAlert` / `DialogConfirm` / `DialogPrompt` / `DialogSelect`（≈ pi 的 confirm/input/select）、`Slot`（往 host 插槽塞内容，**这就是 widget**）、`toast(...)`、`dialog`（对话框栈） |
| `api.ui.Slot` 的 host 插槽 | `home_logo` / `home_prompt_right` / `home_bottom` / `home_footer` / `session_prompt_right` / `sidebar_title` / `sidebar_content` / `sidebar_footer` —— 多个位置可挂 widget |
| `api.theme` | 读写主题（含全色板） |
| `api.kv` | **持久化键值存储**（跨重启） |
| `api.state` | 大量只读状态：`config`/`provider`/`path`/`vcs`/`session.{messages,status,diff,todo,permission,question}`/`part`/`lsp`/`mcp` |
| `api.client` | `OpencodeClient`（同 server 插件拿到的，见 §2.6） |
| `api.event` | **`on(type, handler)`** 订阅 Event（返回 unsubscribe） |
| `api.renderer` | 底层 `CliRenderer` |
| `api.slots` | `register(plugin)` 注册自定义插槽 |
| `api.plugins` | `list/activate/deactivate/add/install` 管理插件自身 |
| `api.lifecycle` | `signal`（AbortSignal）+ `onDispose(fn)` —— **能起后台任务并在退出时清理** |

`TuiCommand` 字段：`{ title, value, description, category, keybind, slash:{name,aliases}, onSelect }`。带 `slash` 的命令会出现在斜杠命令列表里——这正是我们想要的 `/eab paste`、`/eab discard`。

### 2.5 Event 类型（可监听的事件全集）

> 出处：`@opencode-ai/sdk/dist/gen/types.gen.d.ts` 第 602 行 `export type Event = ...`。

server 插件的 `Hooks.event` 收到的、TUI 插件 `api.event.on(type, ...)` 订阅的，都是下面这些（节选对我们相关的）：

- **Session**：`session.created` / `updated` / `deleted` / `status` / `idle` / `compacted` / `diff` / `error`
- **Message**：`message.updated` / `removed` / `message.part.updated` / `message.part.removed`
- **File**：`file.edited` / `file.watcher.updated`
- **Permission**：`permission.updated` / `replied`
- **TUI**：`tui.prompt_append` / `tui.command_execute` / `tui.toast_show`
- 其他：`command.executed`、`todo.updated`、`pty.*`、`server.connected`、`installation.*`、`lsp.*`、`vcs.branch_updated`

> 注意：Event 都是**事后通知**（SSE 推送）。要**拦截/修改** LLM 调用，得用 §2.2 的 Hooks（`chat.*` / `experimental.chat.*`），不是 Event。`session.idle`（agent 空闲）对"等 agent 空闲再注入"有用。

### 2.6 OpencodeClient（server/TUI 插件都拿到的 HTTP API client）

> 出处：`@opencode-ai/sdk/dist/gen/sdk.gen.d.ts` 的 `class OpencodeClient`。

这是 opencode 自身 HTTP server 的完整 API client（基于 [hey-api](https://heyapi.dev) 生成）。插件拿到的 `client` 指向**自己这个 opencode 实例**的 server。命名空间与关键方法：

| 命名空间 | 关键方法 | 对我们的用途 |
|----------|---------|-------------|
| `client.session` | `list` / `create` / `get` / `messages` / **`prompt`** / **`promptAsync`** / `command` / `abort` / `fork` / `status` | **`prompt`/`promptAsync` = 主动发消息触发对话**（≈ pi `sendUserMessage`） |
| `client.tui` | `appendPrompt` / **`submitPrompt`** / `clearPrompt` / **`showToast`** / `executeCommand` / `openModels` / `openSessions` / `publish` / `control.{next,response}` | server 插件**反向控制 TUI**：弹 toast、塞输入框、提交、执行命令 |
| `client.event` | `subscribe`（SSE） | 订阅事件流 |
| `client.config` | `get` / `update` / `providers` | 读写配置 |
| `client.app` | `log` / `agents` | 写日志、列 agent |
| `client.file` / `client.find` | `list`/`read` / `text`/`files`/`symbols` | 文件操作 |
| `client.mcp` | `status`/`add`/`connect`/`disconnect`/`auth` | 动态管 MCP（见 §3） |

**`client.session.prompt` 的典型签名**（来自 `sdk.gen.d.ts`）：
```typescript
session.prompt({ body: { id: sessionID, prompt: "...", ... } })
session.promptAsync({ body: { id: sessionID, prompt: "...", ... } })  // 立即返回
```

### 2.7 Plugin 加载方式（官方 /docs/plugins）

两种，自动发现 + 配置声明：

1. **本地文件目录**（启动时自动加载，无需声明）：
   - `.opencode/plugins/` —— 项目级
   - `~/.config/opencode/plugins/` —— 全局
   - 放 `.js` / `.ts` 文件即可（TS 由内置 Bun 直接执行，无需预编译）。
2. **npm 包**（在 `opencode.json` 声明）：
   ```json
   { "plugin": ["opencode-helicone-session", "@my-org/custom-plugin", "superpowers@git+https://..."] }
   ```
   - 安装命令：`opencode plugin <module>`（`-g` 全局、`-f` 强制）。
   - 用户本地 `opencode.json` 已用此方式（`@different-ai/opencode-browser`、`superpowers`）。

> 开发期最省事：把 `agent-bridge.ts` 丢进 `~/.config/opencode/plugins/`，启动自动加载，改完重启即可（也可配合 `opencode plugin` 做 npm 发布）。

### 2.8 最小 server 插件骨架（针对我们接收端）

```typescript
// ~/.config/opencode/plugins/agent-bridge.ts  (或 npm 包)
import net from "node:net";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";

export default async function plugin(input) {
  const { client, directory, serverUrl } = input;

  // —— 待用上下文（纯上下文报文暂存于此） ——
  let pending = null;

  // —— 1) 启动时写注册表 ——
  const sockPath = path.join(registryDir(), `opencode-${process.pid}.sock`);
  await fs.promises.writeFile(
    path.join(registryDir(), `opencode-${process.pid}.json`),
    JSON.stringify({ name: `opencode-${process.pid}`, pid: process.pid,
      transport: "unix", socket: sockPath, protocol: "eab-1",
      startedAt: Date.now()/1000, cwd: directory }, null, 2)
  );

  // —— 2) 起 Unix socket server 监听 microNeo ——
  const server = net.createServer((conn) => {
    let buf = "";
    conn.on("data", async (d) => {
      buf += d.toString();
      let nl;
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl); buf = buf.slice(nl+1);
        const msg = JSON.parse(line);                       // 见协议 §5 of plan
        if (msg.type !== "context") continue;
        const p = msg.payload;
        if (p.prompt) {
          // 3a) 带 prompt：立即触发对话（≈ pi sendUserMessage）
          await client.session.promptAsync({ body: { prompt: formatWithPrompt(p) } });
          await client.tui.showToast({ body: { message: `microNeo: 已发起对话` } });
        } else {
          // 3b) 纯上下文：暂存待用
          pending = { ...p, from: msg.editor };
          await client.tui.showToast({ body: { message: `microNeo: 上下文待用 (${p.basename})` } });
        }
      }
    });
  });
  server.listen(sockPath);

  // —— 4) 用户下次发言时注入（≈ pi before_agent_start） ——
  return {
    "experimental.chat.messages.transform": async (_input, output) => {
      if (!pending) return;
      output.messages.push(wrapContextAsMessage(pending));  // 把待用上下文塞进消息列表
      pending = null;
    },
    event: async ({ event }) => {
      // 可选：监听 session.idle 等做额外动作
    },
  };
}

function registryDir() { /* 与 plan §4.1 相同算法 */ }
function formatWithPrompt(p) { /* 把上下文+prompt 格式化成给 LLM 的文本 */ }
function wrapContextAsMessage(p) { /* 把上下文包成一条 message */ }
```

> 上面 `client.session.promptAsync` 的 body 细节、`showToast` 的入参以 `sdk.gen.d.ts` / `types.gen.d.ts` 为准（实现时再对齐字段）。骨架意在展示**能力组合的可行性**。

---

## 3. MCP 机制深入

> 一手依据：`config.json` schema 的 `McpLocalConfig`/`McpRemoteConfig`/`McpOAuthConfig`；`OpencodeClient.mcp.*`；官方 /docs/tools、/docs/server。

### 3.1 它是什么
MCP（Model Context Protocol）是一个**标准协议**（Anthropic 主导），让 AI 客户端连接外部"能力源"。opencode 作为 **MCP client**，去连接用户配置的 MCP server。一个 MCP server 能向 LLM 暴露三类能力：**tools**（可调用工具）、**resources**（可读资源）、**prompts**（提示模板）。

### 3.2 配置方式（用户 `opencode.json` 已在用）
```json
{
  "mcp": {
    "MiniMax": {
      "type": "local",                          // 本地子进程
      "command": ["uvx", "minimax-coding-plan-mcp", "-y"],
      "environment": { "MINIMAX_API_KEY": "{env:MINIMAX_API_KEY}" },
      "enabled": true,
      "timeout": 5000                           // ms，默认 5000
    },
    "web-reader": {
      "type": "remote",                         // 远程 HTTP
      "url": "https://open.bigmodel.cn/api/mcp/web_reader/mcp",
      "headers": { "Authorization": "..." },
      "oauth": { "clientId": "...", "scope": "..." }   // 可选
    }
  }
}
```
- 也可用 `opencode mcp add/list/auth/logout/debug` 命令管理，或运行时 `client.mcp.add/connect/disconnect`。
- 权限可按 `mymcp_*: "ask"` 控制该 MCP 所有工具。

### 3.3 能满足我们接收端需求吗？—— 不能

MCP 的定位是**"给 LLM 加可调用的能力"**：它的 server 是被动等 LLM 调用的。它**没有**这些能力：
- ❌ 没有"被外部进程主动推送"的入口（它是 server，但等的是 LLM 的 tool call，不是外部 IPC）。
- ❌ 不能监听 opencode 的生命周期事件、不能拦截 LLM 调用、不能改上下文。
- ❌ 不能主动给 LLM 发消息触发对话。
- ❌ 不能做 opencode 的 UI 反馈。

它**唯一**能做的相关的事是：给 LLM 提供一个工具（如 `get_editor_context`），让 LLM 主动去"拉"编辑器上下文。但那是**拉模型**，和我们"microNeo 主动推送"的**推模型**相反，且依赖 LLM 想起来去调用。

> 结论：MCP 不适合做接收端。它顶多在**反向场景**（让 opencode 的 LLM 主动查 microNeo 状态）里有价值，那是 Phase 4 的事。

### 3.4 最小 MCP server 骨架（仅供参考，不推荐用于本项目）
```typescript
// 一个 stdio MCP server，用 @modelcontextprotocol/sdk
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
const server = new Server({ name: "eab", version: "0.1.0" }, { capabilities: { tools: {} } });
server.setRequestHandler(CallToolRequestSchema, async (req) => {
  if (req.params.name === "get_editor_context") { /* 返回 microNeo 状态 */ }
});
```
配进 `opencode.json` 的 `mcp` 即可被 opencode 加载。**但如上所述，语义不符，不用。**

---

## 4. ACP 机制深入（重点）

> 一手依据：官方文档 `https://opencode.ai/docs/acp/`。

### 4.1 它是什么
ACP（Agent Client Protocol）是 Zed 团队主导的**开放协议**，用于**标准化"代码编辑器 ↔ AI 编码 agent"通信**。opencode 实现了 **ACP server** 端。

### 4.2 怎么用
编辑器配置里声明：spawn `opencode acp` 作为子进程，通过 **JSON-RPC over stdio** 通信。
```json
// Zed: ~/.config/zed/settings.json
{ "agent_servers": { "OpenCode": { "command": "opencode", "args": ["acp"] } } }
```
支持的客户端：Zed、JetBrains（`acp.json`）、Avante.nvim、CodeCompanion.nvim 等。

### 4.3 数据流与生命周期
```
编辑器(Zed)  ──spawn──►  opencode acp (子进程, headless, 无 TUI)
     │                        │
     └── JSON-RPC / stdio ────┘   编辑器是宿主，agent 是被嵌入的子进程
```
- 方向：**编辑器是 client/宿主，opencode 是被它拉起的 server/子进程**。
- 生命周期：opencode acp 的生杀大权在编辑器手里；编辑器关了，这个 opencode 实例就没了。
- 文档明确：via ACP，opencode 功能与终端一致（工具/MCP/自定义命令全支持），少数 slash 命令（/undo /redo）暂不支持。

### 4.4 为什么 ACP **不适用**于我们的 bridge

表面看"编辑器→agent"方向一致，但**架构语义完全不同**：

| 维度 | ACP 模型 | 我们的 Bridge 模型 |
|------|---------|-------------------|
| 进程关系 | 编辑器 **spawn** opencode 子进程 | microNeo 与 opencode **各自独立运行**，互不 spawn |
| opencode 形态 | 全新的 **headless 子进程**（无 TUI） | 用户**正在用的那个 opencode**（TUI 模式，有对话历史） |
| 上下文归属 | 属于编辑器内嵌的这个新实例 | 要进入用户已有的 opencode 会话 |
| 通信方式 | JSON-RPC over **stdio** | 我们设计的 Unix **socket**（注册表发现） |
| 触发方 | 编辑器主动驱动 agent | microNeo 偶尔推送一份上下文 |

**致命错配**：如果 microNeo 想用 ACP，它得自己 spawn 一个 `opencode acp` 子进程——但那是**一个全新的、独立的 opencode 实例**，和用户在另一个终端窗口正开着的 opencode TUI **毫无关系**（对话不共享、上下文不共享）。这完全违背用户意图（"把编辑器上下文送进我正在用的那个 agent"）。

> ACP 解决的是另一类问题：**"我没单独开 agent，让编辑器内嵌一个"**（Zed/JetBrains 用户）。我们解决的是：**"我有独立运行的 agent，想从外部编辑器推上下文进来"**。两者正交。

### 4.5 ACP 的唯一借鉴价值
ACP 印证了 opencode 有清晰的 client/server 架构（见 §2、§5）。但它本身不是我们的路。**无需因 ACP 而修改 `editor-agent-bridge-plan.md` 的 socket 方案。**

---

## 5. 架构事实：opencode 的 client/server 分离与"第三条路"

调研中确认 opencode 是 **client/server 分离**架构：
- **server 进程**：暴露完整 HTTP API（`OpencodeClient` 就是对它的封装）。
- **client**：TUI 是一个 client；`opencode attach <url>` 可连到已运行的 server；plugin 的 `input.serverUrl` 就是这个地址。

由此浮现一条**第三条路**：**microNeo 不起 socket，直接 HTTP POST 到 opencode server**。但有限制：

| 模式 | HTTP server 是否对外可用 | microNeo 能否直接 HTTP 对接 |
|------|------------------------|---------------------------|
| `opencode serve` | ✅ 明确暴露，默认 `127.0.0.1:4096`（见 /docs/server） | ✅ 可以 |
| `opencode`（TUI 日常模式） | 内部确实有 server（`PluginInput.serverUrl`、TUI `api.client` 都靠它），但**端口/可访问性未文档化**（很可能动态端口、仅进程内或 loopback） | ⚠️ 不保证 |

### 5.1 实测验证（2026-06-10，已确认）

启动一个 `opencode`（TUI 日常模式，PID 31647）后实测，结果决定性地推翻了"第三条路"：

| 验证项 | 命令 | 结果 |
|--------|------|------|
| 是否监听 TCP | `lsof -nP -iTCP -sTCP:LISTEN \| grep opencode` | **❌ opencode 不监听任何 TCP 端口** |
| 4096 是否开 | `nc -z 127.0.0.1 4096` | **❌ 未监听** |
| opencode 唯一对外 TCP 连接 | `lsof -p 31647 -i` | 仅 `127.0.0.1:52525->127.0.0.1:7890`（**出站到 ClashX 代理**，是 remote MCP 走 HTTP 出去，非 server） |
| opencode 的 3 个 unix socket | `lsof -p 31647 -U` | fd22/24/26，**对端 = 本地 MCP 子进程 `chrome-devtools-mcp` 的 fd0/1/2（stdin/stdout/stderr）** —— 即 MCP 的 stdio 三连，见下方澄清 |
| 是否多进程 | `pgrep -P 31647` | **单 opencode 进程**，唯一子进程是 MCP。client/server 是**进程内**逻辑分离，非物理多进程（修正 §5 开头的说法） |
| 数据目录有无 serverUrl/lock/pid | `find ~/.local/share/opencode` | **无**。只有 `opencode.db`(SQLite) + `storage/*.json` + 日志 |
| 日志是否提到 listen/http | grep 最新日志 | **无** listen/http/4096 字样；只有 `service=plugin ... loading`、`service=mcp key=... connected` |

#### 5.1.1 那 3 个 unix socket 到底是什么？（澄清常见误解）

有人会问（实测时也确实被问到）："opencode 有 3 个 unix socket，是不是和配置里 3 个 MCP 一一对应？" —— **不是**。精确关系是：

- 配置里共 **4 个 MCP**：`chrome-devtools`(local)、`MiniMax`(local，启动失败)、`web-reader`(remote)、`zread`(remote)。
- 那 3 个 socket 实为**唯一成功启动的本地 MCP（chrome-devtools-mcp）的 stdio 三连**，已用 socket 地址双向指认证实：
  - `opencode.fd22(0x1f9990..)` ↔ `chrome-devtools-mcp.fd0(stdin)(0x89c83a..)`
  - `opencode.fd24(0xd4741f..)` ↔ `chrome-devtools-mcp.fd1(stdout)(0x2c4caf..)`
  - `opencode.fd26(0x36813a..)` ↔ `chrome-devtools-mcp.fd2(stderr)(0x604980..)`
- 2 个 remote MCP（web-reader/zread）走 HTTP，经 ClashX 代理出站（即上表那个 `->7890` 连接）。

**结论**：这 3 个 socket 全是"opencode 作为 MCP **client** 去连它自己的 MCP 子进程"用的通道，与"接收外部推送"毫无关系。opencode 自己**不对外提供任何监听 socket**。

#### 5.1.2 铁证结论

日常 TUI 模式的 opencode **完全不暴露 HTTP server、也不监听任何 socket**。→ **"第三条路（直接 HTTP）"彻底排除**。

> 因此唯一可行的 opencode 接收端实现就是 **Plugin（在 opencode 进程内部起自己的 Unix socket）**——这正是 §6/§7 的推荐方案，也正好和 pi 端、以及 `editor-agent-bridge-plan.md` 的 socket 协议完全一致。选型就此**最终确定**，无需再观望 ACP/HTTP。
>
> 附带收获：实测确认 opencode 是**单进程**架构（client/server 是进程内逻辑分离，plugin 的 `input.serverUrl` 指向进程内 in-process 调用，而非独立监听端口），所以一个 server 插件就能访问全部 client API、全部 hook，无需跨进程协调。

---

## 6. 能力对照矩阵（落到我们的 7 项需求）

> 行 = 接收端需求（来自 `editor-agent-bridge-plan.md` §7）；列 = 三种机制。
> ✅ 直接支持 / ⚠️ 间接可做 / ❌ 不支持。

| 接收端需求 | Plugin (server) | Plugin (TUI) | MCP | ACP |
|-----------|:---:|:---:|:---:|:---:|
| 1. 启动时起 Unix socket server | ✅ `net.createServer`（Node 全能力） | ✅ 同（lifecycle.signal 管生命周期） | ❌ | ❌ |
| 2. 启动时写注册表 JSON | ✅ `fs` | ✅ `fs` | ❌ | ❌ |
| 3. 收报文后暂存上下文 | ✅ 模块变量 | ✅ 模块变量 / `api.kv` 持久化 | ❌ | ❌ |
| 4. 带 prompt 时主动触发对话 | ✅ `client.session.prompt`/`promptAsync` | ✅ **`TuiPromptRef.set()`+`submit()`**：走原生输入流，server 当作用户手敲（见 §6.5） | ❌（只能被动等 LLM 调用） | ⚠️（语义是编辑器驱动，不是外部推） |
| 5. 纯上下文的后续处理 | ✅ `experimental.chat.messages.transform` 隐式注入 | ✅ **预填输入框（`ref.set` 不 submit）/ `/eab paste`**（见 §6.5） | ❌ | ❌ |
| 6. UI 反馈（widget/toast） | ✅ `client.tui.showToast`/`appendPrompt` | ✅ `api.ui.toast`/`Slot`(widget)/`Dialog*` | ❌ | ❌ |
| 7. 自定义 slash 命令（`/eab paste`） | ⚠️ `command.execute.before` 拦截 或 `client.tui.executeCommand` | ✅ `api.command.register`（含 `slash`） | ❌ | ❌ |

**初读结论（后被修正）**：乍看 server 插件靠 `messages.transform` 覆盖了 #5，似乎更强。**但这建立在"注入 = 隐式改写发往 LLM 的消息流"这个未被质疑的前提上**。一旦换到 TUI 原生输入通道（见 §6.5），需求 #5 不再需要拦截 LLM，TUI 插件在全部 7 项上都达到 ✅，且 #6/#7 原生优于 server 插件。**结论：用 TUI 插件**。MCP 和 ACP 始终不适用。

---

## 6.5 关键修正：为什么改选 TUI 插件（而非 server 插件）

> 这一节记录一个重要的选型反转，以及修正过程中的错误前提，免得回头犯糊。

### 错误前提
最初推荐 server 插件，理由是：需求 #5（"纯上下文报文 → 暂存 → 用户下次发言时注入"）只有 server 侧的 `experimental.chat.messages.transform` hook 能做，TUI 侧没有任何消息拦截能力（`tui.d.ts` 里 `messages` 相关只有只读 getter，确证）。

### 被点破的盲点
这个推理有个未被质疑的前提：**"注入上下文" = "隐式改写发往 LLM 的消息流"**。但 TUI 是 opencode 的输入层，**所有用户输入本来就是 TUI 发给 server 的**。所以完全可以走另一条路：

> **microNeo → TUI 插件 → `TuiPromptRef.set()` + `submit()` → server（当作一条普通用户消息）**

server 根本不知道有外部介入，它收到的就是"用户敲了一段话回车了"。`messages.transform` 那套拦截机制整个不需要了。

### API 证据（`tui.d.ts`）
```typescript
export type TuiPromptRef = {
  current: TuiPromptInfo;          // 读当前输入
  set(prompt: TuiPromptInfo): void;// 填内容（input + parts）
  submit(): void;                  // 等价于用户回车
  focus()/blur()/reset()
}
// 提供该 ref 的宿主插槽：
//   home_prompt:     { workspace_id, ref }
//   session_prompt:  { session_id, ref, on_submit }
```
插件渲染进 `session_prompt`（或 `home_prompt`）插槽即可拿到 `ref`，随后 `ref.set({...})` + `ref.submit()`。

### 需求 #5 在新模型下的重新设计（UX 反而更好）
不再做"隐式注入"，改成两种**透明**做法：
- **(a) 预填输入框**：收到纯上下文时 `ref.set({input: 上下文})` 但**不** `submit()`。用户切到 opencode，看到输入框已有一段编辑器上下文，自己接着敲问题、回车。最自然、最透明。
- **(b) `/eab paste` slash 命令**：暂存到 `api.kv`，用户想用时敲 `/eab paste` 把暂存内容拼进当前输入。

相比之下，原来的"隐式注入"会让上下文在 transcript 里不可见（agent 偷偷知道了点东西）——对"用户主动 send"的场景，**可见反而是优点**。

### TUI 插件独有的两个优势（server 插件做不到/做不好）
- **真 widget**：`api.ui.Slot` 可在 `sidebar_content`/`session_prompt_right` 等位置常驻显示"待用上下文"状态。
- **真 slash 命令**：`api.command.register` 带 `slash` 字段，原生支持；server 插件只能 `command.execute.before` 硬拦。

### 唯一代价（边缘）
- **`opencode serve` 无头模式下不加载**：但无头模式连用户都没有，本就不在我们"用户在编辑器里、要把上下文送给正在对话的 agent"的场景内（详见 §6.6）。不是真正需要应对的代价。

### 修正后的结论
**用 TUI 插件**。

---

## 6.6 为什么根本不考虑 `opencode serve` 无头模式

有人（包括本报告的早期版本）可能想"要不要也为 `opencode serve` 无头场景准备一个 server 插件"。**答案是不需要，这是伪需求**：

- 我们整个 bridge 的前提是：**用户在 microNeo 里编辑，想把上下文送给「自己正在对话的 agent」**。
- `opencode serve` 是 headless 服务端，**根本没有用户、没有交互界面、没有"正在进行的对话"**。
- 一个连用户都没有的进程，microNeo 往里推上下文，是推给谁？没有意义。

所以：**只做 TUI 插件，不做 server 插件**。§2.2/§2.8 里 server 插件 Hooks、`messages.transform`、`session.promptAsync` 等内容仅作为调研完整性保留，不进入实现路径。

---

## 7. 推荐方案与对主设计文档的反作用

### 7.1 推荐方案：opencode 接收端 = 一个 TUI 插件

> （本节已按 §6.5/§6.6 的修正重写。旧版推荐 server 插件、保留 serve 备选，均已废弃。）

**方案：TUI 插件**
```
opencode (单进程, TUI 模式, 用户日常交互)
   └─ 加载 TUI 插件 ~/.config/opencode/plugins/agent-bridge.ts
        ├─ api.ui.Slot 渲染 session_prompt / home_prompt → 拿到 promptRef   [拿输入通道]
        ├─ api.lifecycle.signal 管生命周期：net.createServer 起 Unix socket  [需求 1]
        ├─ 启动时写注册表 JSON                                         [需求 2]
        ├─ 收报文（按 payload 是否带 prompt 分流）：
        │    ├─ 带 prompt → promptRef.set({input: 格式化文本}) + submit()  [需求 4, 走原生输入流]
        │    └─ 纯上下文 → promptRef.set({input: 上下文})（不 submit，预填）  [需求 5(a)]
        │                 或暂存到 api.kv，等 /eab paste                  [需求 5(b)]
        ├─ api.ui.toast / api.ui.Slot(widget)：反馈与状态                [需求 6, 原生最强]
        └─ api.command.register({slash:{name:"eab"}})：/eab paste 等     [需求 7, 原生]
```

理由：
1. **走 TUI 原生输入通道**（`ref.set`+`submit`），server 完全无感，不依赖任何实验性 hook，最稳。
2. **需求 #5 重新设计为"预填输入框 / slash 粘贴"**，比隐式注入更透明，UX 更好（§6.5）。
3. **UI 与 slash 命令是原生能力**，比 server 插件的反向控制/硬拦干净得多。
4. **单进程架构**（§5.1 实测）：TUI 在即 opencode 在，socket lifecycle 不是问题。
5. **零改动协议**：socket + 注册表 + JSON 报文与 `架构设计.md` 完全一致。

**（不设 server 插件备选——见 §6.6，无头模式是伪需求）**

**与 pi 接收端的关系**：两者都是 TS、都起 socket、都按 prompt 有无分流，逻辑高度同构；差异仅在"发送"落点——pi 用扩展 API 的 `sendUserMessage`，opencode 用 `promptRef.set+submit`。可抽公共 socket/报文/上下文格式化模块。

### 7.2 对 `editor-agent-bridge-plan.md` 的反作用

| 计划文档里的内容 | 是否需要改 | 说明 |
|----------------|-----------|------|
| **协议（注册表 + JSON 报文）** | ❌ 不改 | opencode 用同样的协议，agent 无关性设计得到验证 |
| **Unix socket 作为 IPC** | ❌ 不改 | ACP/HTTP 都不如 socket 通用；opencode 端在插件内起 socket 即可 |
| **接收端"暂存 + 注入"模型**（架构设计 §7.3） | ✅ opencode 端重新设计 | 不再隐式注入。TUI 插件：带 prompt→`ref.set+submit`；纯上下文→预填输入框(不 submit) 或 `/eab paste`。建议在架构设计里把"注入"措辞改为"递送"，避免暗示隐式改写 |
| **pi 端实现章节（§7）** | ➕ 建议补 opencode 小节 | 在 §7.7（opencode 等其他 agent）处展开：**用 TUI 插件**（`promptRef.set+submit`） |
| **Phase 4 "opencode 接收端"** | ➕ 可前移 | opencode 端与 pi 同构，可从 Phase 4 提到 Phase 2/3 并行做 |
| **ACP 提及** | ➕ 建议在"开放问题"或附录加一条 | 明确"评估过 ACP，不适用（stdio 子进程方向相反）"，免得后续重复怀疑 |

### 7.3 实现成本估计
- **TUI 插件**：与 pi 端扩展逻辑同构，差异仅在"发送"落点——pi 用 `sendUserMessage`，opencode 用 `promptRef.set+submit`。其余（socket server、注册表、报文解析、上下文格式化、按 prompt 有无分流）几乎一样。
- 建议把"接收端通用逻辑"抽成一个纯 TS 模块，pi 扩展和 opencode 插件各自包一层 adapter。

---

## 8. 不确定 / 待验证项清单

| # | 待验证 | 怎么验 | 影响 |
|---|--------|--------|------|
| ~~1~~ ✅已关闭 | ~~`opencode`（TUI 日常模式）内部 HTTP server 的端口是否对外可访问~~ | **已实测（见 §5.1）**：**不监听任何 TCP**；3 个 unix socket 实为本地 MCP 子进程的 stdio 三连。"第三条路"排除，选型确定为 plugin+socket | ✅ 已关闭 |
| ~~2~~ ✅删除 | ~~`client.session.prompt` 的 body schema~~ | 已删：§6.6 判定无头模式是伪需求，server 插件不做了，此验证项随之作废 | ✅ 删除 |
| ~~7~~ ✅删除 | ~~`experimental.chat.messages.transform` 触发粒度~~ | 已删：同上，server 插件不做了 | ✅ 删除 |
| 3 | **`TuiPromptRef.set()`+`submit()` 是否真能发出一条用户消息**（核心验证） | 写最小 TUI 插件：渲染 `session_prompt` 插槽拿 ref，set 一段文本后 submit，看 server 是否收到并触发 LLM | **TUI 方案的命脉**。预期可行（类型明示），但须实测确认 |
| 4 | **多 session 时 ref 绑定**：`session_prompt` 插槽按 `session_id` 渲染，插件如何拿到"当前活动 session"的 ref？用户在 home 页（无 session）时怎么办？ | 查 `api.state.session`；实验 home_prompt vs session_prompt 的 ref 行为 | 影响"发给哪个会话"；home 页可能要先 create session |
| 5 | **预填输入框（set 不 submit）**的可见性：用户切回 opencode 时是否能看到预填内容？会被清掉吗？ | 实验设置后切换窗看输入框是否保留 | 需求 5(a) 的可行性 |
| 6 | ~~server 插件起 socket 的 lifecycle~~ | ~~TUI 退出后 socket 是否还在~~ | **已由 §5.1 解答**：opencode 单进程，TUI 在即在。✅已关闭 |
| 7 | ~~`experimental.chat.messages.transform` 触发粒度~~ | — | 已删：§6.6 判定 server 插件不做，此项作废。✅删除 |

> 以上大部分是**实现阶段**的细化验证。#3/#4/#5 是 TUI 方案的命脉（务必先验）。都不影响"用 TUI 插件做接收端"这一总体结论。

---

## 附录 A. 关键文件/链接索引

**本地（最权威，实现时以此为准）**
- Plugin SDK 类型：`~/.config/opencode/node_modules/@opencode-ai/plugin/dist/{index,tool,tui,shell}.d.ts`
- Plugin 示例：同目录 `example.js`、`example-workspace.js`
- SDK client 全貌：`~/.config/opencode/node_modules/@opencode-ai/sdk/dist/gen/sdk.gen.d.ts`（`class OpencodeClient`）
- Event 类型全集：`.../sdk/dist/gen/types.gen.d.ts`（第 602 行 `export type Event = ...`）
- 配置 schema：`https://opencode.ai/config.json`（本地副本 `/tmp/opencode-config-schema.json`，`$defs` 含 `McpLocalConfig` 等）
- 真实插件范例：`/opt/homebrew/lib/node_modules/@different-ai/opencode-browser/dist/plugin.js`（server 插件，数十个 tool）
- 用户配置：`~/.config/opencode/opencode.json`（`plugin`/`mcp` 实例）、`tui.json`、`package.json`（依赖 `@opencode-ai/plugin`）

**官方文档**
- 插件：`https://opencode.ai/docs/plugins/`
- Server/HTTP API：`https://opencode.ai/docs/server/`
- ACP：`https://opencode.ai/docs/acp/`
- 工具与权限：`https://opencode.ai/docs/tools/`
- SDK：`https://opencode.ai/docs/sdk/`（本次抓取内容为空，待补）
