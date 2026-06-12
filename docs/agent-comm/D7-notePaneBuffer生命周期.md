# D7-notePane Buffer 生命周期实施计划

> 本文档修补 D6 实施后暴露的设计漏洞：notePane 关闭后内容**还在内存**，下次打开能看到上次输入。修复方案：**close 时销毁 buffer**，让"buffer 生命周期跟 `isOpen` 绑定"。
>
> **范围**：仅 buffer 创建/销毁时序。**不做**动态行高、**不做**旧 `notes.md` 文件处理、**不**commit。

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

### 1.3 修复方案（最简版）

**核心原则**：**buffer 生命周期跟 `isOpen` 绑定**——`open` 之前/之后、`close` 之后/之前，buffer 状态正确。

| 阶段 | 行为 | `n.BufPane.Buf` 引用状态 |
|---|---|---|
| `NewNotePane()` (启动时一次) | **不动**（D6 已建好正式 buffer） | 指向新 buffer（空 scratch） |
| `open()` 开头 | `if n.BufPane.Buf == nil` → 创建新 buffer | **首次** = 复用（`!= nil`，不走重建）；**后续** = nil → 新 buffer |
| `close()` 末尾 | `Buf.Close()` + 置 nil | nil（GC 候选） |

**关键设计**：

- **NewNotePane 不动**——D6 已建好正式 buffer，D7 不动
- **open 路径统一**为 `if nil 就创建`——首次和后续写法一致，**首次因 != nil 自动复用**（设计正确，不是 bug）
- **close 销毁**——`Buf.Close()` 从 OpenBuffers 移除，引用置 nil 让 GC 回收
- **整体改动**：open 加 9 行（8 行逻辑 + 1 行注释块）+ close 加 5 行（2 行逻辑 + 3 行注释）= **净 +14 行**

---

## 二、核心决策

### 2.1 close 时：用 `Buffer.Close()` 销毁

`internal/buffer/buffer.go:533-555`：

| 方法 | 从 OpenBuffers 移除 | 调 Fini 清理 | 适用场景 |
|---|---|---|---|
| `Close()` (line 533) | ✅ | ✅ | **notePane 关闭用这个** |
| `Fini()` (line 547) | ❌ | ✅ | 已有 buffer 想清理但不从列表移除 |

D7 用 `Close()`，**完整清理**：
- 从 `OpenBuffers` 列表移除
- 触发 Fini() 内部的清理逻辑（CancelBackup 等）
- GC 可立即回收（前提是 `n.BufPane.Buf = nil` 解绑引用）

### 2.2 open 时：先 `NewBufferFromString` 创建新 buffer，再用 `SetBuffer` 切换 BufWindow

D7 的 open 重建走**两步路径**：

```go
if n.BufPane.Buf == nil {
    buf := buffer.NewBufferFromString("", "", buffer.BTScratch)  // 第 1 步：建新 buffer
    buf.SetOptionNative("ruler", false)
    nbw := n.BufPane.BWindow.(*display.BufWindow)
    nbw.SetBuffer(buf)            // 第 2 步：切 BufWindow.Buf + 装 OptionCallback/设 GetVisualX
    n.BufPane.Buf = buf           // 同步 BufPane.Buf 引用
}
```

**为什么需要两步而不是只调 `NewBufferFromString` 然后赋值？**——`SetBuffer()` 在 BufWindow 内部会**装 `OptionCallback`**（处理 softwrap/wordwrap/diffgutter/ruler/scrollbar/statusline 变化，自动 Relocate）和**设 `GetVisualX`**。**不调 `SetBuffer()`，BufWindow 内部状态会不完整**——后续任何 buffer 配置变化都不会自动触发 UI 更新。

**BufWindow.Buf 是独立引用**：BufWindow struct 里 `Buf *buffer.Buffer`（bufwindow.go:24）是独立于 `BufPane.Buf` 的字段。`SetBuffer()` 内部会设 `w.Buf = b`，所以两步路径**只需要调一次** `SetBuffer()` 就把 BufWindow.Buf 切到新 buffer 了。

### 2.3 `NewNotePane()` 不动

D7 **不改** `NewNotePane()`：

- 它的角色是"启动时创建初始 TheNotePane"
- 创建路径已经走 `BTScratch`、已经过 D6 验证
- NewNotePane 时 buffer **留**在那里（不是 nil）
- 首次 open 触发 `if nil` 判断时**自然跳过**（因为 != nil）

**为什么"留 buffer"是 OK 的而不是 hack？**

- 启动时 buffer 是**空的**——用户看不到任何"残留"
- 首次 open 显示空 buffer，行为正确
- 跟"close 销毁"逻辑**不冲突**——close 之后才需要"重建"

### 2.4 两处独立引用都要处理

引用关系：

```
NotePane
  └─ *BufPane
       ├─ Buf *buffer.Buffer     ← D7 要管（设新 / 置 nil）
       └─ *BufWindow
            └─ Buf *buffer.Buffer ← D7 要管（SetBuffer 切换 / 自动随 SetBuffer 变）
```

**close 时**：
```go
n.BufPane.Buf.Close()    // 销毁（从 OpenBuffers 移除 + 清理）
n.BufPane.Buf = nil      // 防 dangling 引用
```

**open 时**：
```go
buf := buffer.NewBufferFromString("", "", buffer.BTScratch)
buf.SetOptionNative("ruler", false)
nbw := n.BufPane.BWindow.(*display.BufWindow)
nbw.SetBuffer(buf)        // 切 BufWindow.Buf
n.BufPane.Buf = buf       // 同步 BufPane.Buf 引用
```

---

## 三、代码改动

### 3.1 改动文件

只改 `internal/action/notepane.go`。

### 3.2 改动 1：`close()` 加销毁逻辑（notepane.go:479）

**改前**（notepane.go:478-486，D6 实施后状态）：

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

> 注：notepane.go:478 原注释为 `// close closes the NotePane and saves the file`，D6 删了 `Buf.Save()` 调用但**未更新注释**——D7 改前先把注释改为 `// close closes the NotePane`，再实施 D7 改后代码。

**改后**：

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

**净变化**：+5 行（2 行逻辑 + 3 行注释）

### 3.3 改动 2：`open()` 开头加重建逻辑（notepane.go:275）

**改前**（notepane.go:275-283）：

```go
func (n *NotePane) open() {
    // Get the current active BufPane
    pane := MainTab().CurPane()
    if pane == nil {
        return
    }

    // Capture file path from the main editor buffer
    n.filePath = pane.Buf.AbsPath
    ...
```

**改后**：

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

**净变化**：+9 行（8 行逻辑 + 1 行注释块）

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

### 3.5 关键时序图

```
启动
  ↓
NewNotePane() → 创建 buffer A（空 scratch，留着）  [D6 行为，D7 不动]
  ↓
Alt-i #1
  ↓
open() #1
  ├─ n.BufPane.Buf != nil（A 还在）→ 跳过重建  ← 首次 open 跳过（这是设计）
  ├─ 算位置、捕获主编辑器上下文
  └─ 显示空 buffer A
  ↓
用户输入内容 → buffer A 持有内容
  ↓
Alt-Enter 发送
  ↓
NotePaneSend() → 读 buffer A 内容发送 → close()
  ↓
close() #1
  ├─ n.isOpen = false
  ├─ A.Close()         ← 从 OpenBuffers 移除
  ├─ A = nil            ← BufPane.Buf 引用 = nil
  └─ 主编辑器 Relocate
  ↓
buffer A 进入 GC 候选
  ↓
Alt-i #2
  ↓
open() #2
  ├─ n.BufPane.Buf == nil → 重建 buffer B    ← 这次走重建分支
  ├─ nbw.SetBuffer(B)    ← BufWindow.Buf 切换
  ├─ n.BufPane.Buf = B   ← BufPane.Buf 引用同步
  ├─ 算位置、捕获主编辑器上下文
  └─ 显示空白 buffer
  ↓
用户输入新内容 → buffer B 持有新内容
  ...
```

**关键观察**：

- **首次 open 跳过重建**（因为 NewNotePane 时已创建，引用非 nil）——这是设计，不是 bug
- **后续每次 open 都重建**（因为 close 置 nil）
- 行为对称：close 销毁 ↔ open 重建
- **NewNotePane 完全不动**——这是改动最小的关键

---

## 四、风险与验证

### 4.1 风险评估

| 风险 | 等级 | 缓解 |
|---|---|---|
| `Buffer.Close()` 副作用未知 | **极低** | micro 所有 buffer 关闭都走它，notePane 用 BTScratch 无特殊行为 |
| `BufWindow.SetBuffer()` 漏装回调 | 极低 | Lisa 调研确认（`bufwindow.go:53-70`）会装 OptionCallback + GetVisualX |
| `n.BufPane.Buf = nil` 后意外访问 | 极低 | `micro.go:499` 的 Display 在 `IsOpen() == true` 时才调，nil 时不可见；`NotePaneSend()` 也只在 isOpen 时触发 |
| `OpenBuffers` 列表残留 | 极低 | `Close()` 明确从列表移除 |
| `BufWindow.Buf` 残留 stale 引用 | 极低 | 下次 open 调 `SetBuffer()` 会自动覆盖；不需要手动清 |
| 编译失败 | 极低 | 改动只加代码，不删/重命名 |
| 行为回归（user-visible 异常） | 低 | 详见 §4.2 验证步骤 5/6 |

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

## 七、变更摘要

| 维度 | 改动 |
|---|---|
| **代码** | 1 个文件（`notepane.go`），2 处改动（`close` + `open`），净 +14 行 |
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
| D6 §六 后续工作 D11 的"自然覆盖"判断 | 本 D7 已修正 | 把 D6 §六 那段从"下次打开自然覆盖"改为"**D7：close 销毁 + open 重建（详见 D7 §三）**" |
| 旧 `notes.md` 文件处理 | D8? | 选项：自动归档 / 启动时提示 / 完全不管 |
| 产品文档同步 | D9? | D0 §六 改一行（"内容不持久化，关闭即销毁"）；用户界面-V2.md 改意象描述 |
| 动态行高 | D7+1? | 5-10 行自适应（边界处理：浮窗越界时向上扩展） |

---

**预计工作量**：单文件改动，5 分钟实施 + 10 分钟验证 + 0 commit。

**风险等级**：极低。改动加法、API 现成（`Buffer.Close` / `BufWindow.SetBuffer` 都在用）、行为对称（close 销毁 ↔ open 重建）。

**改动最小**：D7 实际只动 open/close 两个函数，NewNotePane 完全不动——这是 D6 已经准备好的最干净基础。
