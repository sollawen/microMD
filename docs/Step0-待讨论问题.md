# Step 1：待讨论问题清单

> 目的：把 Lisa review 中"需要用户决策"的剩余问题**集中**记录，方便逐项讨论
>
> 性质：**讨论中的设计盲点 / 集成陷阱**。每项独立，可分开决策
>
> 来源：Lisa 对 `docs/Step0-事件驱动与代码块边界识别.md` 的 review

---

## 总览：4 项待讨论

| 编号 | 等级 | 主题 | 一句话 | 状态 |
|------|------|------|--------|------|
| **C4** | 🔴 Critical | mergeStyle 没落地 | 决策 3 写得很完整，但 `displayBufferMD` **根本没调用** mergeStyle | ❓ |
| **W2** | 🟡 Warning | 内置 yaml 代码块失效 | 内置 yaml 没 codeblock region → state 全 nil → codeblock 检测**完全失效** | ❓ |
| **W3** | 🟡 Warning | 异步 detect 竞态 | async 跑一半用户打字，看到新旧混合的 state | ❓ |
| **W5** | 🟡 Warning | MDRenderIdle 不消费 | 配置项已注册但 Step 1 **不消费** | ❓ |

**不在本文档范围**：
- C1/C2/C3（编译 / panic / 测试 mock）——**已修**，见 Step1 文档
- W1（闭合行归属变化）——**次要**——文末"次要项"列出
- W4（宽字符 BufX 陷阱）——属于 C4 落地细节
- S1-S6（建议）——非阻塞——文末"建议项"列出

---

## C4：mergeStyle 在代码里完全没落地 🔴

### 问题

**Step 1 文档 §3 决策 3 写得很完整**：
- render 不改前景色
- `mergeStyle(bufLine, bufX, baseStyle)` 在 `bufwindow.go` 实现
- 用 `b.Match(L)[X]` 查 group → `config.GetColor(group.String())` 拿前景色
- baseStyle 提供背景色

**但实际代码**：

```go
// internal/md/render_codeblock.go:5
var codeblockBgStyle tcell.Style = tcell.StyleDefault
    .Foreground(tcell.ColorWhite)        // ← 强行设白前景
    .Background(tcell.ColorDimGray)
```

```go
// internal/display/bufwindow.go:917-960 displayBufferMD
// 写入 screen 时直接用 cell.Style
screen.SetContent(screenX, screenY, cell.Rune, cell.Combining, cell.Style)
// ← 没调用任何 mergeStyle
```

**整个代码库里 `mergeStyle` 不存在**——只在两篇设计文档的伪代码里。

### 后果

| 后果 | 严重度 |
|------|--------|
| python 代码块内字符**前景色全是白**（来自 `codeblockBgStyle.Foreground(ColorWhite)`） | 🔴 |
| 跟决策 3「不改变字符前景色」**直接矛盾** | 🔴 |
| `bufwindow.go:947` 唯一 TODO 是 `mdCache` 写入——**mergeStyle 集成连 TODO 都没标** | 🟡 |

### 这是个**任务边界**问题

mergeStyle 实际是 **Step 0 的工作**——Step 0 任务标题就是"合并 syntax 高亮"。但当前 Step 0 实施文档里**没把 mergeStyle 写出来**——只在 Step 1 决策 3 里出现。

### 三个选项

| 选项 | 含义 | 后果 |
|------|------|------|
| **选项 1**：把 mergeStyle **拉回 Step 0** | Step 0 必须完成 mergeStyle 集成才算完结 | Step 0 文档改大——把"7 个 renderer 改 + mergeStyle 集成"都列为 Step 0 |
| **选项 2**：把 mergeStyle **留到 Step 1** | Step 1 明确包含决策 3 的代码落地 | Step 1 文档改小（移走 mergeStyle 决策 3 描述）——但 Step 1 任务变重 |
| **选项 3**：建一个 **Step 0.5** | 专门做 mergeStyle 集成 | 任务切得更细——但文档变多 |

### 待用户决策

- 选 1 / 2 / 3？
- 决策后，相应文档要改（Step 0 实施文档 / Step 1 文档 / 新建 Step 0.5 文档）

---

## W2：内置 yaml 下代码块检测完全失效 🟡

### 问题

**事实**：
- 内置 `runtime/syntax/markdown.yaml` **没有 codeblock region**（见 `Micro-原生高亮机制.md §7.2`）
- 内置 yaml 只有 `- special: "^```$"`（**pattern**）和行内代码 region
- 内置 yaml 下**整篇 buffer 的 state 全是 nil**（没块级 region 可进）

**后果**：
- Step 1 改造后的 detect 扫内置 yaml 的 buffer → 永远拿不到 `curState != nil` → **没有 codeblock 段**
- 这是相对 Step 0 的**完全回归**——Step 0 还能用 ` ``` ` 字符串前缀识别代码块

**Step 0 → Step 1 的实际退化**：

| 配置 | Step 0 | Step 1（当前方案） |
|------|--------|-------------------|
| 内置 yaml | ✅ ` ``` ` 字符串前缀识别 | ❌ **完全失效** |
| 用户 yaml（配 region） | ✅ 字符串前缀 | ✅ state 数组 |

### 三个修复方案

| 方案 | 描述 | 优点 | 缺点 |
|------|------|------|------|
| **A** | detect 加 fallback 字符串匹配 | 内置 yaml 兼容——零退化 | detect 有点重复逻辑 |
| **B** | 明确放弃内置 yaml 兼容 | 简单——代码干净 | 用户切到内置就失效——需文档强警告 |
| **C** | 运行时检查 `b.SyntaxDef` 的 rules 是否含 ` ``` ` region，动态决定 | 通用 | 复杂——需要 parse syntax def |

**Lisa 推荐 A**。

### 方案 A 的具体实现

```go
// 2.4 伪代码补充：
// 伪代码建议在 isCodeblockByState 失败时调用
func isCodeblockByString(lineY int) bool {
    trimmed := strings.TrimSpace(buf.Line(lineY))
    return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// detect 主循环
if isInCodeblockByState(lineY) || isCodeblockByString(lineY) {
    // codeblock 路径
}
```

**或者**更干净——在 `curState == nil` 但 `isCodeblockByString` 为 true 时**进入** codeblock 状态机（保留部分 Step 0 逻辑）：

```go
// codeblock 检测优先级：state 数组 → 字符串 fallback
inCodeblock := curState != nil || isCodeblockByString(y)
```

### 待用户决策

- 选 A / B / C？

---

## W3：异步 detect 真实竞态 🟡

### 问题

**事实**（代码追踪）：
- `Line.lock` 是**每行独立**的 `sync.Mutex`（`line_array.go:51`）
- `HighlightStates` / `HighlightMatches` 每次操作**只锁一行**
- `LineArray.State` / `LineArray.Match` 也只锁一行
- LineArray 没全局锁

**竞态场景**（用户打开大文件，async detect 跑一半用户开始打字）：

| 时刻 | 异步 goroutine | 主线程（用户打字） |
|------|----------------|-------------------|
| t1 | `b.State(0)` 锁 line 0 → nil | — |
| t2 | — | `MarkModified(5, 5)` → `ReHighlightStates(5)` |
| t3 | — | 给 line 5 改 state 为 `PythonRegion` |
| t4 | `b.State(5)` 锁 line 5 → **PythonRegion** | — |
| t5 | detect 把 line 0-4 当非 codeblock、line 5+ 当 codeblock | — |
| t6 | `screen.Redraw()`（detect 完） | 还没真正改完 line 6+ 的 match |

**结果**：detect 看到**新旧混合**的 state，产出错误的代码块边界——用户在同一帧看到不连贯的渲染。

**下一帧**（用户继续打字）`MarkModified` 触发同步 detect → 修正。

### 锁升级方案

```go
// detect 开始时锁整个 buffer
b.Lock()           // LineArray.lock（假设加全局锁）
defer b.Unlock()
```

**代价**：
- 阻塞所有编辑直到 detect 跑完
- detect 跑全文件可能上百毫秒
- **输入会卡顿**——不可接受

### Lisa 的建议

1. **承认不保证单帧一致性**——靠下一帧修正
2. **小文件**（如 < 1000 行）用全局锁
3. **大文件**放弃一致性
4. **`BufWindow.mdCache`** 加 atomic / 互斥保护

### 待用户决策

- 接受"单帧可能不一致，靠下一帧修正"？
- 还是必须加全局锁（接受输入卡顿）？
- 还是加更细粒度的版本号机制（detector 记版本，编辑时 bump 版本，detect 开始时检查版本，版本变了重跑）？

---

## W5：MDRenderIdle 配置项已注册但 Step 1 不消费 🟡

### 问题

**事实**：
- `internal/config/settings.go:147` 已注册 `mdrenderidle: 10`
- `internal/md/config.go:13` 已写入 `MDConfig.MDRenderIdle float64`（注释「Step 1 用」）
- `internal/action/bufpane.go:289` 已读取
- `PRD-产品需求文档.md:172, 476` 解释为「无操作 X 秒后进入渲染模式」

**Step 1 文档完全没有讨论**：
- 什么时候算「无操作」？
- 编辑模式下要不要降级（不 detect / 不 mergeStyle）？
- "0 = 从不自动进入渲染模式"是只在 idle 时不渲染、还是编辑时也不渲染？
- 与 `MDRender`（总开关）的关系？

### 影响

配置项已生效但代码里**没人读**。等 Step 1 写完才发现——可能要回头补 idle 检测机制（timer、事件订阅）。

### 修复方案

**选项 1**：文档**显式声明** "Step 1 只做配置注册，不做实际消费；消费逻辑在 Step 3 滚动优化时一起做"

**选项 2**：Step 1 文档新增一节 "MDRenderIdle 消费设计"——含：
- 什么时候算「无操作」？（LastAction + 1s 阈值）
- 编辑模式下降级策略（关闭 detect / 关闭 mergeStyle / 还是都不关）
- "0 = 从不自动进入" 的精确含义
- 与 `MDRender` 的关系（`MDRender=off` 强制关闭，`MDRenderIdle=0` 仅关闭 idle 自动进入）

### 待用户决策

- 选项 1（先声明延后）还是选项 2（现在写消费设计）？

---

## 次要项汇总（不在本文档主体）

| 编号 | 主题 | 简述 | 处理方式 |
|------|------|------|---------|
| **W1** | 闭合行归属变化 | Step 0 闭合行算 codeblock，Step 1 闭合行算下一段 | 文档 §2.4 加一行说明"闭合行 ` ``` ` 在新设计中归下一段"——**纯文档** |
| **W4** | 宽字符 BufX 陷阱 | mergeStyle 调用时该传 `cell.BufX` 不是 `ci` | 等 C4 修完时一起改——在 displayBufferMD 集成时显式写出来 |
| **S1** | buffer.go 行号引用模糊 | §1.3 引用 `SetSyntaxDef (buffer.go:1006)` 但实际 1006 是 `go func() {` | 文档润色——**纯文档** |
| **S2** | 伪代码用 `int = -1` 哨兵 | Go 不推荐 | 改用 `var inCodeblock bool`——**纯风格** |
| **S3** | §2.3 状态追踪例子只到 L20 | fence 状态变化没画时序图 | 加 ASCII 时序图——**纯文档** |
| **S4** | §2.4 引用 6 个工具函数 | 实际只有 `isListItem` / `isHR` 两个 | 文档注内联表达式——**纯文档** |
| **S5** | 缺一份"Step 0 → Step 1 改动 checklist" | 文档没列具体改的文件 | 文档新增一节——**纯文档** |
| **S6** | 缺"分阶段实施顺序" | 三决策有依赖（决策 2 → 1 → 3） | 文档新增一节——**纯文档** |
| **O1** | §2.6 措辞缩小适用范围 | 改"buffer 末尾未闭合"——**已改** | ✅ |
| **O2** | §3.4 attr 说法略保守 | 实测很多 colorscheme 标 Bold/Underline | 文档润色——**纯文档** |
| **O3** | §3.2 mergeStyle 不取 baseStyle 的 attr 没解释 | 加一行解释 | 文档润色——**纯文档** |
| **O4** | buffer.go:1004-1013 省略了 resolveIncludes | §1.3 引用代码块少一行 | 文档补全——**纯文档** |

---

## 待用户决策清单（4 项）

| # | 选项 | 我倾向 |
|---|------|--------|
| C4 | 1（mergeStyle 拉回 Step 0）/ 2（留到 Step 1）/ 3（建 Step 0.5） | ❓ |
| W2 | A（fallback 字符串匹配）/ B（放弃内置 yaml 兼容）/ C（运行时检查 yaml） | A |
| W3 | 接受单帧不一致 / 全局锁 / 版本号机制 | ❓ |
| W5 | 文档声明延后 / 现在写消费设计 | ❓ |

**讨论顺序建议**：C4（任务边界）→ W2（设计盲点）→ W3 + W5（文档加段话）
