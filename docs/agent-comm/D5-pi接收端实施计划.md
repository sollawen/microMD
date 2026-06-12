# D5 — pi 接收端完整实施计划

> **性质**：pi 接收端（`eabp-receivers/eabp-pi/index.ts`）从 M1「纯 UI 展示」升级到**完整功能形态**的实施计划。
>
> **本文与既有文档的关系**
> - `D2-通信协议方案.md` —— 协议权威。本文实现其 §六（递送语义）的全部要求
> - `D3-原型验证计划.md` —— M1/M2 原型验证。其 §七 M2 段落是本文的雏形；本文是其**定稿版**（补全格式化、边界、`@path :line` 语法等原型未细化的部分）
> - `D4-notePane发送端.md` —— 发送端实施。本文是其对端
>
> **不做的事**（本文范围之外，见 §七）：持久化、配置开关、读文件补全上下文。

---

## 一、背景与动机

M1 已验证全链路连通（discover → send → notify → 注册表落地 → 清理）。但 `onMessage` 目前只做 `ctx.ui.notify` 一行展示——**上下文根本没进 LLM**。D2 §六 定义的两条递送路径（带消息 / 纯上下文）都未实现。

本计划把接收端补完整：收到报文后**真正递送到 LLM 通道**。

**M1 已验证的事实（可直接依赖，不再质疑）**：
- `pi.on("session_start", ...)` 里起的 `net.Server` 全程存活，`'data'` 回调能正常收到并解析报文
- `session_start` 闭包里捕获的 `ctx` 对象，其 `ctx.ui.*` 全程可调用（M1 的 `notify` 即证）
- 注册表目录算法、socket 权限、清理配对——全部工作正常

**关键参考**：`pi-in-zellij/pane-comm/interceptor.ts` 已验证「把外部文本送进 LLM」的标准路径就是 `pi.sendUserMessage(text)`——顶层 `pi` 方法，可在任意闭包调，立即触发对话。本文带 message 路径直接复用。

---

## 二、现状（M1 代码盘点）

`eabp-receivers/eabp-pi/index.ts` 当前形态：

| 已有 | 状态 |
|------|------|
| 注册表目录计算 + `0700` 创建 | ✅ |
| `session_start` 起 socket server + 写注册文件 | ✅ |
| `session_shutdown` 关 server + 删 socket + 删注册文件 | ✅ |
| 按行分帧 + `JSON.parse` + 信封 `v`/`type` 校验 | ✅ |
| `let pending: any = null` 字段 | 🗑️ 删除（新设计无 pending 概念） |
| `onContext` 只 `notify` 一行（将改名为 `onMessage`） | ❌ 需替换 |
| `formatText` | ❌ 不存在 |

---

## 三、目标形态（完整功能）

实现 D2 §六 的两条递送路径，**仅此而已**：

1. **带 `message` 报文** → 格式化「上下文 + 消息」为一条用户消息 → `pi.sendUserMessage` 立即触发对话回合
2. **纯上下文报文**（无 `message`）→ 格式化上下文块 → `ctx.ui.setEditorText` 填入输入框 → **由用户决定怎么用**

**没有** pending、widget、slash 命令、discard 等任何状态管理。扩展程序只负责「把东西递进去」，用户对输入框内容有完全控制权。

---

## 四、核心实现

### 4.1 `onMessage` 分流（替换 M1 的 notify）

```typescript
function onMessage(env: any, ctx: any) {
  const p = env.payload;
  // text 即完整的待递送文本——有 message 时已拼进去，没有时就是纯上下文块
  const text = formatText(p);

  if (p.message) {
    // —— §6.1 带 message：走 sendUserMessage（永远带 deliverAs:steer，详见 §六）——
    pi.sendUserMessage(text, { deliverAs: "steer" });
  } else {
    // —— §6.4 纯上下文：填入输入框，不触发对话，用户接管 ——
    ctx.ui.setEditorText(text);
    ctx.ui.notify(`📌 已放入输入框，可编辑后发送`, "info");
  }
}
```

**关键点**：
- `pi.sendUserMessage` 是**顶层 `pi`** 的方法，不是 `ctx` 的——可在 socket 回调里直接调（M1 已验证 socket 回调里调 `ctx.ui.notify` 正常，`pi.sendUserMessage` 同属普通异步调用）
- **不需要 `isIdle()` 分流**：永远带 `deliverAs: "steer"` 即可。`sendUserMessage` 内部调 `prompt()`，唯一硬约束是「streaming 时不带会抛错」——只要永远带上，idle/streaming 两种状态都不会抛错，且各自走正确路径（见 §六）
- 纯上下文路径用 `ctx.ui.setEditorText` 填入输入框——**不触发对话**（D2 §6.4「不得自动触发」），把决定权完全交给用户

> **为什么纯上下文用 `setEditorText` 而非暂存 + transform 注入**：参考 `pi-in-zellij/editor/editor.ts` 已验证的用法——外部内容直接填输入框，用户完全可见、可编辑、可删除、可补充。这比任何「暂存 + 隐式注入」都更透明、更简单、更符合 D2 §6.4「由用户决定怎么用」。无状态、无泄漏、无意外。

### 4.2 带 message 路径：拼接逻辑

把「上下文 + 用户消息」**拼成一条用户消息文本**（D2 §6.1：格式化为一条用户消息）。结构：

```
<上下文块>

<用户 message 原文>
```

即上下文在前，用户的话在后，两者合成一条 user message 送进 LLM。这样 LLM 一次看到完整背景 + 提问，单回合响应。

> **为什么是「拼成一条 user message」而不是「context 用 customType + message 分两条」**：D2 §6.1 明确要求「格式化为**一条**用户消息」。一条消息语义最清晰，LLM 不会在两条消息间困惑该回应哪条。这也呼应 `pi-in-zellij/interceptor.ts` 的 `enhancedContent` 拼装模式（前置元信息块 + `---` + 用户内容）。

---

## 五、文本格式化（formatText）

这是接收端递送给 LLM 的**实际文本**。采用全世界 AI agent 通用的 `@path` 文件引用 + ` :line` 行标注语法。

### 5.1 设计原则

1. **`@path` 是世界通用约定**——所有 AI agent（Cursor / Claude / Copilot …）都认识，LLM 天然知道这是文件引用、会用 read 工具去读。pi 展不展开 `@` 无所谓（我们走 `sendUserMessage` / `setEditorText`，不在 CLI 展开路径上）——LLM 自己用工具读
2. **` :line` 是行标注**——空格 + `:line12` 告诉 LLM「重点看第 12 行」；有选区时用 `:line12-line20` 给范围。语法形如 GitHub 的 `#L12-L20`，LLM 熟悉
3. **path 指向磁盘上已保存的文件**——它和用户在 notePane 里打的字是两回事。用户的字已作为 `message` 单独发过来；path 就是个稳定文件引用。存不存盘不是 agent 该操心的事（契约即「文件已存盘」，与 Cursor/Claude 的 `@` 一致）
4. **不内联文件内容、不渲染 `selection.text`**——只给引用 + 行范围，让 LLM 自己 read。避免传输冗余 + 尊重「只递送事实」
5. **不替 LLM 下结论**（D2 原则④）——给 path + 行 + message，不解读「这段代码意思是…」

> **行号 1-based 是隐含约定**：LLM 的工具（sed/awk/ripgrep/read）全是 1-based，整个生态统一，`:line12` 与 `sed -n '12p'` 天然对齐，无需声明。这是协议层的不变量（见 D2 §5.3）。

> **`selection.text` 的去留**：本格式不渲染选区文本，所以接收端会忽略发送端抓取 + 2048 截断的选区内容。协议字段 `selection.text` 保留（兼容 / 其他接收端可能用）；`internal/action/notepane.go` 里的 2048 截断逻辑对**本接收端**是 dead weight——但那属发送端（D4）范围，不在 D5（receiver-only）内，留待将来清理决策。

### 5.2 `formatText(p)` 模板

```typescript
function formatText(p: any): string {
  const focus = p.selection
    ? `line${p.selection.start.line}-line${p.selection.end.line}`
    : `${p.cursor.line}`;
  const base = `@${p.path} :line${focus}`;
  return p.message ? `${base}\n\n${p.message}` : base;
}
```

**四种渲染形态**——以下即 `formatText(p)` 的返回值（也是 LLM 或输入框实际收到的逐字文本）。前两种（有 message）走 `sendUserMessage`，后两种（无 message）走 `setEditorText`：

**有选区 + 有 message**（走 `sendUserMessage`）

```
@/Users/me/a.md :line12-line20

这段内容感觉怪，帮我换个说法
```

**无选区 + 有 message**（走 `sendUserMessage`）

```
@/Users/me/a.md :line12

这句话感觉怪，帮我换个说法
```

**有选区 + 无 message**（走 `setEditorText`）

```
@/Users/me/a.md :line12-line20
```

**无选区 + 无 message**（走 `setEditorText`）

```
@/Users/me/a.md :line12
```

规则：focus = 光标行（无选区）/ `<start>-<end>` 选区范围（有选区）；有 message 时在 `@...` 行后加空行 + message。

### 5.3 仅一个函数：`formatText`

带 message 时，拼接逻辑在 `formatText` 内部完成（见 §5.2）——返回 `上下文块 + 空行 + message` 的完整文本。`onMessage` 不再重复拼接。

---

## 六、流式冲突处理（D2 §6.3）

带 message 报文到达时，agent 可能在 streaming。D2 §6.3 要求「排队等当前回合结束再触发，不打断」。

实现：**永远带 `deliverAs: "steer"`**——一行搞定，不需要 `isIdle()` 分流。

`pi.sendUserMessage(text, { deliverAs: "steer" })`：
- **idle 时**：`steer` 参数被忽略（`prompt()` 的 streaming 分支不进入），走正常立即提交，立即触发新回合
- **streaming 时**：进入 steer 队列，等当前回合工具链跑完、下次 LLM 调用前插入

> **为什么不分流**：`sendUserMessage` 内部调用 `prompt()`，把 `deliverAs` 透传为 `streamingBehavior`。`prompt()` 的唯一硬约束是「streaming 时**不带** `streamingBehavior` 会抛错」。只要永远带上，两种状态都不会抛错，且各自走正确路径。分流是多余的。
>
> 为什么选 `steer` 而不是 `followUp`：`steer` 在「当前回合工具链跑完、下次 LLM 调用前」插入，尽快让用户从 microNeo 发来的问题被处理。`followUp` 会等到 agent 完全空闲（多轮工具全部结束），对一个明确的「用户发来新问题」语义偏慢。换 `deliverAs` 字符串即可切换，将来有体感再调。
>
> 纯上下文路径不存在此问题——它只填输入框，不触发任何对话。

---

## 七、不做的事（明确出界）

| 项 | 为什么不做 |
|----|-----------|
| 输入框内容持久化（`appendEntry`） | 用户没发送就丢了——符合预期（无状态设计）。持久化是过度设计 |
| 配置开关（如「改 deliverAs 策略」） | 当前行为即合理默认。等真实使用反馈再加深配置 |
| pending 暂存 / widget / slash 命令 | 纯上下文用 `setEditorText` 后，用户对输入框有完全控制权——不需要扩展程序再提供「discard / paste / send / status」等管理手段。越俎代庖 |
| `bye` 报文的专门处理 | microNeo 不发 `bye`，且当前无状态可 GC。D2 §7.1 已要求「忽略未知 type」，天然前向兼容——专门写处理逻辑是 YAGNI |
| 接收端读文件补全上下文 | 违反「只递送事实」原则 + 有文件竞态。LLM 需要更多上下文自己用工具读 |
| 未知 `type` 报文的告警 | D2 §7.1 要求「忽略未知 type」（前向兼容），不告警 |

---

## 八、reentrancy 风险与对策

D3 §八 风险 2 提出的核心未知：**从 `net.Socket` 的 `'data'` 回调（裸 Node 异步上下文）调 `pi.sendUserMessage`，是否会 reentrant 出问题**。

**判断**：
- `sendUserMessage` 是顶层 `pi` 的方法，文档明确「可在任意闭包调」
- `'data'` 回调跑在普通 Node event loop tick 上，与 pi 事件回调无本质区别（都是 async scheduling）
- M1 已验证 socket 回调里调 `ctx.ui.notify`（同步触发 TUI 重绘）无问题
- `pi-in-zellij/interceptor.ts` 在 `pi.on('input')` 回调里调 `pi.sendUserMessage` 已实际工作——虽非 socket 回调，但同属「非 pi 事件源的异步上下文」，参考性强
- `sendUserMessage` 内部应是把「触发 agent loop」当作一次异步任务调度，不会同步重入回 socket

**对策**：直接实现 §4.1 的 `sendUserMessage` 调用。验收第 1 条专测它。**若实测出问题**，退路：

> 退路：带 message 也走 `setEditorText` 填输入框——但这样会丢「立即触发」（要等用户手动按回车）。只作为 fallback，不作首选。

---

## 九、验收清单

### 带 message 路径
- [ ] microNeo 发带 message 报文 → pi `sendUserMessage` → **agent 真的开腔回复**（不是只 notify）
- [ ] 回复内容引用了上下文（文件路径 / 选区文本），证明上下文进了 LLM
- [ ] agent streaming 时收到带 message → `steer` 参数使消息排队，当前回合工具链跑完后插入触发新回合，不抛错
- [ ] idle 时收到带 message → `steer` 参数被忽略，立即触发新回合

### 纯上下文路径
- [ ] microNeo 发纯上下文 → pi `setEditorText` 填入输入框 + notify 提示，**agent 不开口**
- [ ] 输入框内容可编辑（用户能改/删/补充）
- [ ] 用户手动发送 → LLM 收到上下文块 + 用户补充内容，回复引用了上下文
- [ ] 连发两份纯上下文 → 后者覆盖前者（输入框显示最新一份）

### 边界
- [ ] 收到未知 `type`（含将来预留的 `bye`）→ 静默忽略，不崩
- [ ] 收到畸形 JSON → 静默跳过该行，不崩
- [ ] 收到 `v` 不匹配 → 静默忽略

### `@path :line` 格式
- [ ] `formatText` 输出形如 `@<path> :line<focus>`，无包装标签、无选区文本内联
- [ ] 无选区时 focus = 光标行；有选区时 focus = `<start>-<end>` 范围
- [ ] 有 message 时 `@...` 行后跟空行 + message；无 message 时只有 `@...` 行
- [ ] 行号为 1-based（与 LLM 的 sed/ripgrep 工具对齐）

---

## 十、执行顺序

1. `formatText`（纯函数，可先写好）
2. `onMessage` 分流（替换 M1 的 notify 两行；删除 `let pending`）
3. `pi` 重载扩展，逐项过验收清单（先带 message 路径，再纯上下文，最后边界）
4. 更新 `eabp-receivers/eabp-pi/README.md`（从「M1 形态」改为完整功能描述）

> **不 commit，等用户 review 后决定**。

---

## 十一、文件改动清单

| 文件 | 改动 |
|------|------|
| `eabp-receivers/eabp-pi/index.ts` | 主要改动：新增 `formatText`、`onMessage` 分流（setEditorText / sendUserMessage）；删除 pending 字段 |
| `eabp-receivers/eabp-pi/README.md` | 功能描述从 M1 升级到完整形态 |

**不改动**：microNeo 侧任何代码（发送端已完整，D4 落地）、D2 协议文档（本文是实现其既定规范）。
