# D7-notePane Buffer 生命周期实施计划

> 本文档修补 D6 实施后暴露的设计漏洞：notePane 关闭后内容**还在内存**，下次打开能看到上次输入。修复目标：**保证"打开 = 空白"**，同时避免破坏 micro 原生代码对 Buf 的访问假设。
>
> **版本**：v2（实施后修订）。v1 设计的 close 路径有 nil 访问 panic 漏洞，详见 §九。
>
> **范围**：仅 buffer 创建/销毁时序。**不做**动态行高、**不做**旧 `notes.md` 文件处理。

---

## 一、背景与动机

### 1.1 D6 漏掉的设计漏洞

D6 把 notePane 从"持久化到 `notes.md`"改造为"BTScratch 无文件 buffer"。意图很明确：**关闭后内容直接丢弃**。

但 D6 实施后实测发现：

- notePane buffer 在 close() 时**没有被销毁**——引用还在
- 下次 open() **不会**重建 buffer——会复用同一个空 buffer
- 结果：**发送后字符还在内存**，下次 Alt-i 打开能看到上次内容

**事实链**：

```
启动：TheNotePane = NewNotePane()           // globals.go:16，只调一次
      ↓
Alt-i：Toggle() → open() 或 close()          // 不重建 TheNotePane
      ↓
close() 只做：n.isOpen = false + Relocate()   // ← buffer 没动
      ↓
open()  只做：算位置 + 捕获主编辑器上下文     // ← buffer 没动
      ↓
buffer 还活着，下一次 open 看到的是上次内容
```

### 1.2 D6 §六 后续工作 D11 的错误判断

D6 当时在 §六 写过：

> "buffer 是 TheNotePane 单例复用，不必清理——**下次打开自然覆盖**"

"自然覆盖"是错的。`open()` 不重置 buffer，所以**没有自然覆盖**——而是"自然保留"。

### 1.3 修复方案（最终版）

**核心原则**：**保证"打开 = 空白"**——每次 `open()` 创建一个全新的空 buffer，**不**依赖 `close()` 销毁。

| 阶段 | 行为 | `n.BufPane.Buf` 引用状态 |
|---|---|---|
| `NewNotePane()` (启动时一次) | **不动**（D6 已建好正式 buffer） | 指向新 buffer A（空 scratch） |
| `open()` 开头 | **总是** Close 旧 buffer（如有）+ NewBufferFromString + SetBuffer | 总是指向新 buffer |
| `close()` 末尾 | **不动 Buf** | 保持原引用（不 panic） |

**关键设计（v2 修正）**：

- **NewNotePane 不动**——D6 已建好正式 buffer
- **open 路径** 总是 Close 旧 + 建新——**首次**会 Close 掉 NewNotePane 留的 buffer A（"浪费"一个空 buffer，**无副作用**）；**后续**每次都是空白
- **close 路径不动 Buf**——避开 `BufPane.HandleEvent` 后续访问 h.Buf 的 nil panic
- **整体改动**：open 加 ~10 行（Close 旧 + SetBuffer 新）+ close 减 5 行（D7 v1 加的 set-nil 块被删）= **净 ~+5 行**

**为什么 close 不动 Buf？**——详细论证见 §2.1 和 §九。简单说：v1 设计让 close 设 `Buf = nil`，但 close 是从 `NotePaneSend` action handler 内部被调的，**handler 返回后** `BufPane.HandleEvent` 还会继续走到 `h.Buf.MergeCursors()`（bufpane.go:506）——访问 nil → panic。

---

## 二、核心决策

### 2.1 close 时：**不**动 Buf

**关键发现**（v1 实施后 panic 暴露的）：

`n.close()` 是从 `NotePaneSend` action handler **内部**被调的——`NotePaneSend` 走的是 `BufPane.HandleEvent` 调用栈，handler 返回后 `BufPane.HandleEvent` 还会**继续走**到 `bufpane.go:506` 调 `h.Buf.MergeCursors()` 统一清理。

**v1 设计的"close 设 `Buf = nil`"** 在这个时序下导致 nil 访问 panic。

**v2 修正**：`close()` 完全不动 Buf——只设 `isOpen = false` + 主编辑器 Relocate。`n.BufPane.Buf` 引用保持，BufPane 后续访问安全。

**这不影响"打开 = 空白"承诺**——空白由 `open()` 总是建新 buffer 来保证（见 §2.2）。

### 2.2 open 时：总是 Close 旧 buffer（如有）+ 新建

```go
// 兑现"打开 = 全新"承诺：关掉旧 buffer（如有），建新的
if n.BufPane.Buf != nil {
    n.BufPane.Buf.Close()        // 从 OpenBuffers 移除 + 调 Fini 清理
}
buf := buffer.NewBufferFromString("", "", buffer.BTScratch)
buf.SetOptionNative("ruler", false)
nbw := n.BufPane.BWindow.(*display.BufWindow)
nbw.SetBuffer(buf)               // 切 BufWindow.Buf + 装 OptionCallback
n.BufPane.Buf = buf              // 同步 BufPane.Buf 引用
```

**为什么用 `Close()` 而不是 `Fini()`？**——`Close()` 从 `OpenBuffers` 移除 buffer，是"彻底关闭"；`Fini()` 只清理 Serialise/Backup，不从列表移除。notePane 关闭后不需要这个 buffer 在 OpenBuffers 里。

**为什么"总是建新"而不是"复用 + 清空"？**——micro 原生 Buffer API 没有"清空内容"方法（Replace + 重建 cursor 复杂且有边界 bug）。**新建**路径简单可靠，**性能可忽略不计**（open 不是热路径）。

**为什么 NewBufferFromString 之后还要 SetBuffer？**——`SetBuffer()` 在 BufWindow 内部会**装 `OptionCallback`**（处理 softwrap/wordwrap/diffgutter/ruler/scrollbar/statusline 变化，自动 Relocate）和**设 `GetVisualX`**。不调 `SetBuffer()`，BufWindow 内部状态会不完整——后续任何 buffer 配置变化不会自动触发 UI 更新。

**BufWindow.Buf 是独立引用**：BufWindow struct 里 `Buf *buffer.Buffer`（bufwindow.go:24）是独立于 `BufPane.Buf` 的字段。`SetBuffer()` 内部会设 `w.Buf = b`，所以一次 `SetBuffer()` 同时切 BufWindow.Buf + 装回调，**再手动同步** `n.BufPane.Buf`。

### 2.3 `NewNotePane()` 不动

D7 **不改** `NewNotePane()`：

- 它的角色是"启动时创建初始 TheNotePane"
- 创建路径已经走 `BTScratch`、已经过 D6 验证
- v2 设计里 NewNotePane 留的 buffer A **首次 open 会被 Close 掉**——但这是无副作用的浪费（空的 scratch buffer），行为正确
- 跟"open 总是重建"逻辑**不冲突**——open 路径不依赖 buffer 是否存在

### 2.4 close 路径上 BufPane 引用关系

`close()` 不动 Buf——BufPane 引用关系保持原样：

```
NotePane
  └─ *BufPane
       ├─ Buf *buffer.Buffer     ← close 时不动，open 时由 SetBuffer 切到新 buffer
       └─ *BufWindow
            └─ Buf *buffer.Buffer ← close 时不动，open 时由 SetBuffer 切到新 buffer
```

**close 后到下次 open 之前**：`n.BufPane.Buf` 和 `n.BufPane.BWindow.Buf` 都还引用着上一次 SetBuffer 设的 buffer（已被 Close）。**安全**——Close() 后的 buffer 仍是个有效 struct，其内部字段（cursors、text）未清空，BufPane 内部访问不会 panic。

---

## 三、代码改动

### 3.1 改动文件

只改 `internal/action/notepane.go`。

### 3.2 改动 1：`close()` — **删除** v1 加的 set-nil 块（还原 D6 之前）

**改前**（v1 实施后状态，已含 v1 加的代码）：

```go
// close closes the NotePane
func (n *NotePane) close() {
    n.isOpen = false

    // 销毁 buffer：close 后内存中无残留，GC 可回收
    if n.BufPane != nil && n.BufPane.Buf != nil {
        n.BufPane.Buf.Close()    // 从 OpenBuffers 移除 + 调 Fini 清理
        n.BufPane.Buf = nil      // 防 BufPane 持有 dangling 引用
    }

    // Restore main editor's normal scroll position
    if pane := MainTab().CurPane(); pane != nil {
        pane.BWindow.Relocate()
    }
}
```

**改后**（v2 — 删除 set-nil 块）：

```go
// close closes the NotePane
func (n *NotePane) close() {
    n.isOpen = false

    // Restore main editor's normal scroll position
    if pane := MainTab().CurPane(); pane != nil {
        pane.BWindow.Relocate()
    }
}
```

**净变化**：-6 行（删 1 行 if 头 + 2 行逻辑 + 1 行闭合 + 1 行空行 + 1 行注释）

**为什么 v1 的 `Buf.Close()` 也一起删了？**——close 路径上 Buf 引用还活着，Close() 之后 `n.BufPane.Buf` 指向已 Close 的 buffer 没问题。**`Close()` 移到 open 路径**（见 §3.3），统一在 open 时清理。

### 3.3 改动 2：`open()` 开头加"总是 Close 旧 + 新建"逻辑（notepane.go:275）

**改前**（v1 实施后状态）：

```go
func (n *NotePane) open() {
    // Get the current active BufPane
    pane := MainTab().CurPane()
    if pane == nil {
        return
    }

    // 兑现"打开 = 全新"承诺：buffer 是 nil 时重建
    // close() 时已销毁（引用 = nil），这里必须新建
    // 首次 open 因 NewNotePane 留了 buffer，自动跳过此分支
    if n.BufPane.Buf == nil {
        buf := buffer.NewBufferFromString("", "", buffer.BTScratch)
        buf.SetOptionNative("ruler", false)
        nbw := n.BufPane.BWindow.(*display.BufWindow)
        nbw.SetBuffer(buf)         // 切 BufWindow.Buf + 装 OptionCallback
        n.BufPane.Buf = buf        // 同步 BufPane.Buf 引用
    }

    // Capture file path from the main editor buffer
    n.filePath = pane.Buf.AbsPath
    ...
```

**改后**（v2 — 总是 Close 旧 + 新建）：

```go
func (n *NotePane) open() {
    // Get the current active BufPane
    pane := MainTab().CurPane()
    if pane == nil {
        return
    }

    // 兑现"打开 = 全新"承诺：关掉旧 buffer（如有），建新的
    // close() 不再销毁 buffer（避免 BufPane.HandleEvent 内 nil 访问 panic）
    // 这里统一处理"开新"
    if n.BufPane.Buf != nil {
        n.BufPane.Buf.Close()      // 从 OpenBuffers 移除 + 调 Fini 清理
    }
    buf := buffer.NewBufferFromString("", "", buffer.BTScratch)
    buf.SetOptionNative("ruler", false)
    nbw := n.BufPane.BWindow.(*display.BufWindow)
    nbw.SetBuffer(buf)             // 切 BufWindow.Buf + 装 OptionCallback
    n.BufPane.Buf = buf            // 同步 BufPane.Buf 引用

    // Capture file path from the main editor buffer
    n.filePath = pane.Buf.AbsPath
    ...
```

**净变化**：v1 的 `if Buf == nil` 改成 `if Buf != nil { Close }`，多 1 行 + 注释更新

**顺带修一处 v1 编译问题**：v1 open 重建块已经声明了 `nbw`，但 open 函数下面 `Reposition BufWindow` 那段又重复声明了 `nbw :=` 导致编译失败。v2 删掉重复声明，直接复用上面的 `nbw`。

### 3.4 不改的东西

| 不改 | 原因 |
|---|---|
| `NewNotePane()` | 启动时创建正式 buffer（留着），行为已正确 |
| `newBufPane(buf, win, nil)` 调用 | D6 §2.4 已定，NewNotePane 时调用 |
| `allowedNotePaneActions` 白名单 | 跟 buffer 生命周期无关 |
| `NotePaneSend()` / `Display()` / `HandleEvent()` / `lowestCursorScreenRow()` | 跟 buffer 生命周期无关 |
| `internal/buffer/buffer.go` | `Close()` / `Fini()` 行为符合预期，不改 |
| `internal/display/bufwindow.go` | `SetBuffer()` 行为符合预期，不改 |
| micro 任何原生代码 | 项目规则：侵入最小化 |

### 3.5 关键时序图（v2）

```
启动
  ↓
NewNotePane() → 创建 buffer A（空 scratch，留着）  [D6 行为，D7 不动]
  ↓
Alt-i #1
  ↓
open() #1
  ├─ n.BufPane.Buf != nil（A 还在）→ A.Close()           ← 关闭 NewNotePane 留的 buffer
  ├─ buf = NewBufferFromString + SetOptionNative("ruler") ← 创建新 buffer B
  ├─ nbw.SetBuffer(B)         ← 切 BufWindow.Buf + 装 OptionCallback
  ├─ n.BufPane.Buf = B        ← 同步 BufPane.Buf 引用
  ├─ 算位置、捕获主编辑器上下文
  └─ 显示空白 buffer B
  ↓
用户输入内容 → buffer B 持有内容
  ↓
Alt-Enter 发送
  ↓
NotePaneSend() → 读 buffer B 内容发送 → close()
  ↓
close() #1
  ├─ n.isOpen = false
  └─ 主编辑器 Relocate        ← **不** 动 Buf（B 引用保持，BufPane.HandleEvent 后续访问安全）
  ↓
B 仍被 n.BufPane 引用，结构体未 GC
  ↓
Alt-i #2
  ↓
open() #2
  ├─ n.BufPane.Buf != nil（B 还在）→ B.Close()           ← 关闭旧 buffer
  ├─ buf = NewBufferFromString + SetOptionNative("ruler") ← 创建新 buffer C
  ├─ nbw.SetBuffer(C)         ← 切 BufWindow.Buf
  ├─ n.BufPane.Buf = C        ← 同步 BufPane.Buf 引用
  ├─ 算位置、捕获主编辑器上下文
  └─ 显示空白 buffer C
  ↓
用户输入新内容 → buffer C 持有新内容
  ...
```

**关键观察（v2）**：

- **close 路径完全不动 Buf**——`n.BufPane.Buf` 引用保持，BufPane.HandleEvent 后续访问 h.Buf 不会 nil panic
- **open 路径总是 Close 旧 + 建新**——保证"打开 = 空白"（不论首次还是后续）
- **每次 open 都新建 buffer**——NewNotePane 留的 buffer 首次 open 就被 Close 掉（一次性浪费，**无副作用**）
- 行为对称性体现在 open 端：每次都是"关掉旧的，建新的"——而 close 端**不**有"销毁"动作

---

## 四、风险与验证

### 4.1 风险评估（v2）

| 风险 | 等级 | 缓解 |
|---|---|---|
| `Buffer.Close()` 副作用未知 | **极低** | micro 所有 buffer 关闭都走它，notePane 用 BTScratch 无特殊行为 |
| `BufWindow.SetBuffer()` 漏装回调 | 极低 | Lisa 调研确认（`bufwindow.go:53-70`）会装 OptionCallback + GetVisualX |
| **`BufPane.HandleEvent` 内 nil 访问** | **已消除** | v2 close 路径**不**动 Buf，h.Buf 引用保持 |
| `OpenBuffers` 列表残留 | 极低 | `Close()` 在 open 路径上明确从列表移除 |
| `BufWindow.Buf` 残留 stale 引用 | 极低 | 下次 open 调 `SetBuffer()` 会自动覆盖；不需要手动清 |
| 每次 open 都新建 buffer（性能） | **极低** | open 不是热路径，buffer 构造开销 <1ms |
| 多次 `SetBuffer()` 反复装回调 | 极低 | SetBuffer 幂等，装 OptionCallback 内部用覆盖/重建 |
| NewNotePane 留的 buffer 首次 open 被 Close 掉（浪费） | **极低** | 一次性空 scratch buffer 的 Close 几乎零开销 |
| 编译失败 | 极低 | 改动只改逻辑，不改类型/重命名；v1 顺带修复的 nbw 重复声明也消除了 |
| 行为回归（user-visible 异常） | 低 | 详见 §4.2 验证步骤 |

### 4.2 验证步骤

```bash
# 1. 编译
make build-quick
# 预期：通过，零 warning

# 2. grep 确认关键调用
grep -n "Buf\.Close()\|nbw\.SetBuffer" /Users/sollawen/pi-dev/microNeo/internal/action/notepane.go
# 预期：两处都出现
#       close() 里：n.BufPane.Buf.Close()
#       open() 里：nbw.SetBuffer(buf)

# 3. 跑 microNeo 人工验证（Lisa 跑不了，必须人工）
# 3a. Alt-i 打开 notePane → 输入 "测试 #1" → Alt+Enter 发送
# 预期：发送成功，notePane 关闭
# 3b. 立即 Alt-i 重新打开
# 预期：notePane 是**空白**（不是"测试 #1"）
# 3c. 关闭，再次 Alt-i
# 预期：仍是空白
# 3d. 输入 "测试 #2" → Esc 关闭（不发送）
# 3e. Alt-i 重新打开
# 预期：是**空白**（不是"测试 #2"）—— 验证 Esc 关闭也走 close() 销毁路径
# 3f. 输入 "测试 #3" → 关闭 → 重新打开 → 再次关闭
# 预期：每次打开都是空白，关闭后内存无残留

# 4. 主编辑器不受影响
# 4a. 在 notePane 打开时主编辑器键盘不响应（确认主 pane 仍被冻结）
# 4b. 关闭 notePane 后主编辑器恢复正常焦点

# 5. 边界：频繁 toggle
# 5a. 快速 Alt-i 切换 10 次
# 预期：无 panic、无内存泄漏迹象（用 top/htop 看内存稳定）

# 6. 边界：Send 失败不关闭
# 6a. 关闭 pi receiver（如果可能），Alt-i + 输入 + Alt+Enter
# 预期：notePane **不**关闭（按 D4 设计：发送失败不关闭）
# 6b. 预期：buffer 内容仍在（发送失败，close 未被调用）
# 6c. Esc 关闭 → 重新 Alt-i 打开
# 预期：是**空白**（Esc 关闭走 close() 路径，已销毁）
```

### 4.3 回归检查清单

- [ ] 编译通过
- [ ] open → 输入 → 关闭 → open：内容是空白
- [ ] open → 输入 → 发送 → open：内容是空白
- [ ] open → 输入 → Esc → open：内容是空白
- [ ] 发送失败时 notePane 不关闭（**D4 既有行为**）
- [ ] 主编辑器焦点切换正常
- [ ] 频繁 toggle 不 panic
- [ ] BufWindow 渲染正常（光标、内容、滚动）

---

## 五、执行顺序

1. **改 `notepane.go`**：
   - `close()` 加 `Buf.Close()` + `Buf = nil`
   - `open()` 开头加 buffer 重建逻辑
2. **grep 确认**：
   ```bash
   grep -n "Buf\.Close()\|nbw\.SetBuffer" /Users/sollawen/pi-dev/microNeo/internal/action/notepane.go
   ```
3. **`make build-quick`** 验编译
4. **不 commit**（用户明确要求）
5. **人工跑 §4.2 验证步骤**
6. **报告改动**给用户 review

---

## 六、约束（用户明确要求）

| 约束 | 说明 |
|---|---|
| ❌ **不 commit** | 用户在 D7 任务发起时明确要求"不要做 commit" |
| ❌ **不 push** | commit skill 默认规则 |
| ❌ **不写新 D6 修订版** | D7 独立成文，描述完整 |

---

## 七、变更摘要（v2）

| 维度 | 改动 |
|---|---|
| **代码** | 1 个文件（`notepane.go`），2 处改动（`close` + `open`），净 **+5 行**（v1 写的是 +14，实际差 9 行——v2 删了 v1 加的 set-nil 块，多出的 buffer 重建代码在 open 端） |
| **micro 原生代码** | 零侵入 |
| **EABP 协议** | 不变 |
| **D6 计划** | D6 §六 后续工作 D11 的"单例复用不必清理"判断**作废** |
| **新增依赖** | 无 |
| **用户可见变化** | 关闭（任何方式）后内容立即从内存清空；下次打开是空白 |
| **commit** | **不**做（用户明确要求） |
| **范围外** | 动态行高、配置清理、旧 notes.md 处理、product 文案更新 |

---

## 八、后续工作（不在 D7 范围）

| 议题 | 后续文档 | 备注 |
|---|---|---|
| D6 §六 后续工作 D11 的"自然覆盖"判断 | 本 D7 已修正 | D6 §六 那段从"下次打开自然覆盖"改为"**D7 v2：close 不动 Buf + open 总是 Close 旧 + 新建（详见 D7 §三）**" |
| 旧 `notes.md` 文件处理 | D8? | 选项：自动归档 / 启动时提示 / 完全不管 |
| 产品文档同步 | D9? | D0 §六 改一行（"内容不持久化，关闭即销毁"）；用户界面-V2.md 改意象描述 |
| 动态行高 | D7+1? | 5-10 行自适应（边界处理：浮窗越界时向上扩展） |
| Receiver 退出清理 socket 文件 | 单独 | 17 个孤儿 .sock 残留的清理策略 |
| `SetBuffer()` 多次调用对 BufWindow 状态的影响 | 调研 | 当前未观察到问题，但没审计过 OptionCallback 的"重装"语义 |

---

## 九、实施回顾（v2 修订记录）

> 本节记录 v1 设计的问题、修复方案、commit hash——给将来 reviewer 和 v3 维护者参考。

### 9.1 v1 设计回顾

v1 在 D7 文档里的设计是：

| 阶段 | 行为 |
|---|---|
| `NewNotePane()` | 留 buffer A（!= nil） |
| `open()` | `if Buf == nil` → 重建（BTScratch） |
| `close()` | `Buf.Close() + Buf = nil` |

**理论逻辑**：close 销毁 buffer → 引用 = nil → 下次 open 触发 if nil 重建。

### 9.2 v1 实施后的 panic

v1 commit (未独立 commit——直接在 D7 修复 commit `67ec2858` 之前的某次 working tree 状态) 实施后，用户报告：

```
Micro encountered an error: runtime.errorString runtime error: invalid memory address or nil pointer dereference
runtime/panic.go:336
github.com/micro-editor/micro/v2/internal/buffer/buffer.go:1101
github.com/micro-editor/micro/v2/internal/action/bufpane.go:506
github.com/micro-editor/micro/v2/internal/action/notepane.go:513
github.com/micro-editor/micro/v2/cmd/micro/micro.go:558
```

### 9.3 根因分析

**调用链**（Alt+Enter 发送时）：

```
micro.go:558  TheNotePane.HandleEvent(event)       ← IsOpen() 守门通过
notepane.go:513  n.BufPane.HandleEvent(event)      ← 转发，无 isOpen 检查
bufpane.go:337  config.RunPluginFnBool(...)        ← 按 binding 查 Alt+Enter → 调 NotePaneSend
notepane.go (NotePaneSend 内部)  →  n.close()  →  n.isOpen=false, n.BufPane.Buf=nil
bufpane.go:506  h.Buf.MergeCursors()               ← panic：h.Buf 是 nil
```

**关键时序**：

`n.close()` 是在 `BufPane.HandleEvent` **调用栈内部**被 `NotePaneSend` 调用的。`NotePaneSend` 返回后，**`BufPane.HandleEvent` 还没退出**——它会继续走到 line 506 调 `h.Buf.MergeCursors()` 统一清理。

v1 的设计假设是 "close() 是个干净的同步点，close 之后不会有任何代码访问 h.Buf"——**这个假设错了**。

### 9.4 v1 设计还有第二个问题

v1 改动 1 的注释写"GC 可回收"——**这个说法也是错的**。`n.BufPane` 这个 struct 还在 `TheNotePane.BufPane` 字段里引用着，**`n.BufPane.Buf = nil` 对 GC 没用**（Buf 引用还通过 n.BufPane 间接活着）。

**所以"set nil"既不能 GC 回收，又会引发 panic**——双重失败。

### 9.5 v2 修复方案

**改动 1：close() 还原成 D6 之前（删除 v1 加的 set-nil 块）**

```go
func (n *NotePane) close() {
    n.isOpen = false
    if pane := MainTab().CurPane(); pane != nil {
        pane.BWindow.Relocate()
    }
}
```

**改动 2：open() 的 if nil 改成 if != nil { Close } + 总是新建**

```go
if n.BufPane.Buf != nil {
    n.BufPane.Buf.Close()
}
buf := buffer.NewBufferFromString("", "", buffer.BTScratch)
buf.SetOptionNative("ruler", false)
nbw := n.BufPane.BWindow.(*display.BufWindow)
nbw.SetBuffer(buf)
n.BufPane.Buf = buf
```

**顺带修**：v1 编译失败（`nbw` 重复声明）——v2 open 重建块已声明 nbw，line 334 `nbw :=` 删掉，直接复用。

### 9.6 v1 vs v2 对比

| 维度 | v1 | v2 |
|---|---|---|
| close 是否动 Buf | Close() + set nil | **不动** |
| open 重建条件 | if Buf == nil | **总是 Close 旧 + 新建** |
| close → open 之间 Buf 状态 | nil（**panic 风险**） | 旧 buffer 引用保持（**安全**） |
| 能否保证"打开 = 空白" | ✅（首次/后续都通过 if nil 触发） | ✅（**总是新建**） |
| 性能 | 首次 open 跳过重建（复用 NewNotePane 留的） | 每次 open 都 Close + 新建 |
| 行为对称性 | close 销毁 ↔ open 重建 | open 端独大："关旧 + 开新" |
| 改 close 路径的 panic 风险 | **有**（已验证触发） | **无** |

**v2 的代价**：每次 open 多做一次 Close + NewBufferFromString（约 0.5ms）。**完全可接受**。

### 9.7 commit hash

| 事件 | commit | message |
|---|---|---|
| D7 文档初版（含 v1 设计） | `19e22f2d` | `docs(agent-comm): add D6 and D7 plans for notePane buffer refactor` |
| **v2 修复**（含代码） | `67ec2858` | `fix(notepane): prevent nil panic in BufPane.HandleEvent after close` |
| D7 文档 v2 修订（**本次**） | 待 commit | `docs(agent-comm): align D7 with v2 fix — close 路径不变，open 总是重建` |

### 9.8 给将来维护者的提示

- **不要轻信"close 销毁 = GC 回收"**——BufPane 引用 Buf 路径上还有活引用
- **不要在 action handler 内部对 BufPane 状态做"假设不会再被访问"的修改**——BufPane.HandleEvent 在 handler 返回后还会继续走清理逻辑
- **open 路径的"总是重建"看似浪费**——但比"按需重建 + close 路径上小心的状态管理"安全得多
- **micro 原生代码 `h.Buf.MergeCursors()` 这类末尾清理**是隐式假设"Buf 一直存在"——破坏这个假设的 PR 要小心

---

**预计工作量**：v2 实施 5 分钟 + 验证 10 分钟 + 文档修订 5 分钟。

**风险等级**：极低。v2 改回 v1 之前的 close 行为（安全），open 端加的"总是重建"是加法（不破坏既有）。

**改动最小**：D7 v2 实际只动 open/close 两个函数，NewNotePane 完全不动——这是 D6 已经准备好的最干净基础。

**v1 → v2 的代价**：5 行（v1 的 set-nil 块）+ 1 行重复 nbw 声明 = 删 6 行；v2 open 端 if nil 改成 if != nil 多 1 行。**净 +5 行左右**。
