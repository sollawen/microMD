# D6-notePane无文件化实施计划

> 本文档把 notePane 的 buffer 从"持久化文件（notes.md）"改造为"无文件 scratch buffer（BTScratch）"，使发送后内容直接丢弃，不写盘。
>
> **范围**：仅 buffer 持久化策略。**不做**动态行高、**不做**产品文档重写、**不做**配置文件清理（保持最小改动）。这些是后续工作。

---

## 一、背景与动机

### 1.1 现状

notePane 当前从 `~/.config/microNeo/notes.md` 加载或创建 buffer，关闭时调用 `Buf.Save()` 写回：

```go
// notepane.go 现状（删减）
n.noteFile = filepath.Join(homeDir, ".config", "microNeo", "notes.md")
os.MkdirAll(dir, 0755)
buf, err := buffer.NewBufferFromFile(n.noteFile, buffer.BTDefault)
if err != nil {
    buf = buffer.NewBufferFromString("", n.noteFile, buffer.BTDefault)
}
...
func (n *NotePane) close() {
    n.BufPane.Buf.Save()  // ← 关闭时写盘
    n.isOpen = false
    ...
}
```

### 1.2 问题

- **无产品价值**：notePane 内容已通过 EABP 发给 agent，agent 那边有完整对话历史；本地这份副本没人翻。
- **隐私风险**：长期积累的笔记文件可能包含敏感问题、token、个人想法。
- **认知负担**：用户会觉得"我之前问过什么来着？"——这种回顾需求应该用 agent 那边的历史，不该是 notePane。
- **实现冗余**：文件路径管理、目录创建、Save 流程、Save action 白名单维护——全都不需要。

> **与 D4 的关系**：D4 把 notePane 改造为 EABP 发送端（让用户能给 agent 发消息），D6 把 notePane 改造为无文件便签（让内容不写盘）。两者**独立可分**：D4 关心"内容怎么发出去"，D6 关心"内容怎么不写出去"。D6 不改 D4 的发送管线、协议契约、白名单 bindings，只动 buffer 创建和 close 时的持久化。

### 1.3 决策（来自与产品讨论）

| 维度 | 现状 | 改为 |
|---|---|---|
| 持久化 | 写到 `notes.md` | **不写盘** |
| Buffer 类型 | `BTDefault` | **`BTScratch`** |
| 文件路径 | `~/.config/microNeo/notes.md` | **无** |
| 关闭行为 | Save → 文件持久化 | **直接丢弃** |

> **范围外**（明确不做，留后续）：
> - 动态行高（5-10 行自适应）
> - 产品文案、配置项清理
> - 旧 `notes.md` 文件的自动迁移或删除逻辑
> - 用户提示"notePane 不再保存"的迁移引导

---

## 二、核心决策

### 2.1 Buffer 类型用 `BTScratch`

`internal/buffer/buffer.go:56-57`：

```go
// BTScratch is a buffer that cannot be saved (for scratch work)
BTScratch = BufType{3, false, true, false}
//                       ^^^^^ ^^^^^^^ ^^^^^^^
//                       Readonly=false  Scratch=true  Syntax=false
```

字段含义：

| 字段 | 值 | 影响 |
|---|---|---|
| `Readonly` | `false` | 用户可正常编辑 |
| **`Scratch`** | **`true`** | **`Save()` 直接返回错误，micro 内置拦截**（`save.go:237`） |
| `Syntax` | `false` | 无语法高亮——与"纯文本"产品定位一致 |

### 2.2 micro 内置拦截 Save 是兜底防线

`internal/buffer/save.go:232-239`：

```go
func (b *Buffer) saveToFile(filename string, withSudo bool, autoSave bool) error {
    var err error
    if b.Type.Readonly {
        return errors.New("Cannot save readonly buffer")
    }
    if b.Type.Scratch {
        return errors.New("Cannot save scratch buffer")  // ← 兜底
    }
    ...
}
```

这意味着：
- 即便用户绑定了 `Save` action，`Save()` 也会被 micro 拒绝
- **当前白名单 `allowedNotePaneActions` 没有 Save 类 action，且 BTScratch 提供兜底拦截**——两道防线
- 未来如果有人把 Save 类加进白名单，也没事——BTScratch 兜底；D6 不会因此失效

### 2.3 用 `NewBufferFromString("", "", BTScratch)` 创建

`internal/info/infobuffer.go:42-46` 是 micro 自己的先例：

```go
func NewBuffer() *InfoBuf {
    ...
    ib.Buffer = buffer.NewBufferFromString("", "", buffer.BTInfo)
    ...
}
```

**BTInfo 和 BTScratch 关键字段相同**（`{_, false, true, false}`），都是不可保存的 scratch buffer。InfoPane 已经在用，notePane 抄同样的写法是零风险。

### 2.4 BufPane 创建路径：保持 `newBufPane`（小写），不动

**D6 明确不改这个调用点。** notePane 的 `NewNotePane()` 现在用的是小写的 `newBufPane`，D6 保持这个选择。

#### micro 提供三个 BufPane 创建入口

源码位置：`internal/action/bufpane.go:259-289`。

| 函数 | 触发什么 | 触发时机 |
|---|---|---|
| `newBufPane`（小写） | 仅 `new(BufPane)` + 设 Buf/BWindow/tab/Cursor/mousePressed 字段 | **不**触发任何后续逻辑 |
| `NewBufPane`（大写） | 调 `newBufPane` 后**立即**调 `finishInitialize()` | 同步、立即 |
| `NewBufPaneFromBuf`（大写） | 调 `newBufPane` **之前**先调 `initMDConfig(buf, w)` | 同步、立即；`finishInitialize` 延后到 Resize |

#### 三个入口各自的风险

**`NewBufPaneFromBuf` 风险：会启用 markdown 渲染**

`initMDConfig(buf, w)` 会按 buffer 文件名判断要不要启 markdown 渲染。如果 buffer 是 `.md`（notePane 之前就是），`initMDConfig` 会把 BufWindow 配成 markdown 模式——这跟 D6 的"纯文本便签"目标直接冲突。

即使换成 `BTScratch`（无文件，Path=""）后，`initMDConfig` 拿到空 Path 的 buffer 行为如何**没有验证过**——D6 不承担这个验证成本，保持原路径。

**`NewBufPane` 风险：BufWindow 尺寸未定就 finishInitialize**

`finishInitialize()` 内部依赖 BufWindow 已有正确的 X/Y/Width/Height（算视图、设 scrollbar、算 gutter 等都依赖）。但 notePane 的 BufWindow 在 `NewNotePane()` 里是以 `(0, 0, 80, 5)` 占位创建的，**真**坐标要等 `open()` 根据光标位置算（"光标下方 1 row"）。

如果走 `NewBufPane`，会基于占位尺寸跑一次 `finishInitialize`，然后 `open()` 调 `Resize` 触发第二次——既浪费又有状态污染。

**`newBufPane`（小写）是唯一对的选择**

它只 new 对象、设字段、设初始 cursor，**不**触发任何后续逻辑。`finishInitialize` 延后到 `open()` 调 `Resize()` 时跑（此时 BufWindow 已有真坐标）——一次、且基于真坐标，安全。

#### D6 实施约束

- ✅ 保留 `newBufPane(buf, win, nil)` 这个调用
- ❌ 不改成 `NewBufPane` 或 `NewBufPaneFromBuf`
- ❌ 不"顺手"加 `initMDConfig` 或 `finishInitialize` 调用

这一节是"反改动提醒"：D6 只动 buffer 类型（§3.1），BufPane 创建路径**互不相关**，不要混合改动。

---

## 三、代码改动

### 3.1 `internal/action/notepane.go`

#### 改动 1：删除 `noteFile` 字段

```go
type NotePane struct {
    *BufPane
    isOpen        bool
    x, y          int
    width         int
    height        int
-   noteFile      string
    filePath      string
    fileCursor    buffer.Loc
    fileSelection    *[2]buffer.Loc
    fileSelectionText string
}
```

#### 改动 2：删除相关 imports

```go
import (
    "encoding/json"
    "net"
-   "os"
-   "path/filepath"
    "strings"
    "time"
    ...
)
```

但 `os` 还在用（`os.Getpid()`），**只删 `path/filepath`**，保留 `os`。

#### 改动 3：改写 `NewNotePane()` 里的 buffer 创建

**改前**（notepane.go:239-273）：

```go
func NewNotePane() *NotePane {
    n := &NotePane{
        height: 5,
    }

    // Set the notes file path
    homeDir, err := os.UserHomeDir()
    if err != nil {
        homeDir = "/tmp"
    }
    n.noteFile = filepath.Join(homeDir, ".config", "microNeo", "notes.md")

    // Ensure directory exists
    dir := filepath.Dir(n.noteFile)
    os.MkdirAll(dir, 0755)

    // Load or create the buffer
    buf, err := buffer.NewBufferFromFile(n.noteFile, buffer.BTDefault)
    if err != nil {
        buf = buffer.NewBufferFromString("", n.noteFile, buffer.BTDefault)
    }

    // Disable line numbers for NotePane
    buf.SetOptionNative("ruler", false)

    // Create BufWindow with initial position (will be adjusted in open())
    win := display.NewBufWindow(0, 0, 80, n.height, buf)
    win.SetHideStatusLine(true)

    // Create BufPane using newBufPane (lowercase, does not trigger finishInitialize)
    n.BufPane = newBufPane(buf, win, nil)
    n.BufPane.bindings = NotePaneBindings

    return n
}
```

**改后**：

```go
func NewNotePane() *NotePane {
    n := &NotePane{
        height: 5,
    }

    // Create an in-memory scratch buffer. Content is discarded on close;
    // see buffer.BTScratch ("Cannot save scratch buffer" in save.go:237).
    buf := buffer.NewBufferFromString("", "", buffer.BTScratch)

    // Disable ruler for NotePane
    buf.SetOptionNative("ruler", false)

    // Create BufWindow with initial position (will be adjusted in open())
    win := display.NewBufWindow(0, 0, 80, n.height, buf)
    win.SetHideStatusLine(true)

    // Create BufPane using newBufPane (lowercase, does not trigger finishInitialize)
    n.BufPane = newBufPane(buf, win, nil)
    n.BufPane.bindings = NotePaneBindings

    return n
}
```

**净变化**：
- 删 19 行（路径、目录、NewBufferFromFile 错误处理）
- 加 1 行（NewBufferFromString scratch）
- 函数从 21 行变到 13 行

#### 改动 4：`close()` 删 `Buf.Save()`

**改前**（notepane.go:494-500）：

```go
func (n *NotePane) close() {
    n.BufPane.Buf.Save()
    n.isOpen = false

    // Restore main editor's normal scroll position
    if pane := MainTab().CurPane(); pane != nil {
        pane.BWindow.Relocate()
    }
}
```

**改后**：

```go
func (n *NotePane) close() {
    n.isOpen = false

    // Restore main editor's normal scroll position
    if pane := MainTab().CurPane(); pane != nil {
        pane.BWindow.Relocate()
    }
}
```

**为什么显式删除而不是保留**：
- `Buf.Save()` 会被 BTScratch 拦截，调用不写盘
- 但保留调用会**误导未来读代码的人**——让人以为"关闭时还会保存"
- 显式删除 = 显式表达"不持久化"的产品意图
- **不抛错**：`Buf.Save()` 在 scratch buffer 上返回 error（"Cannot save scratch buffer"），这个 error 之前是被丢弃的；删掉之后连这个被丢弃的 error 都没有了，更干净

### 3.2 不改的文件

| 文件 | 原因 |
|---|---|
| `internal/display/bufwindow.go` | `hideStatusLine` 机制不变 |
| `cmd/micro/micro.go` | 事件路由不变 |
| `internal/eabp/` | 协议层不涉及持久化 |
| `internal/action/infopane.go` | 是参考对象，不是改动对象 |
| 任何 bindings 配置文件 | 白名单里没有 Save 类 action，无需改 |

### 3.3 不改的逻辑

- `open()`：不变（位置计算、context 捕获、EABP 准备都和 buffer 类型无关）
- `NotePaneSend()`：不变（仍然从 `n.BufPane.Buf.Bytes()` 读字面字符发送）
- `lowestCursorScreenRow()`：不变（操作主编辑器 buffer，不涉及 notePane buffer）
- `HandleEvent()`：不变
- `Display()`：不变
- `allowedNotePaneActions`：不变（本来就没 Save 类）

---

## 四、风险与验证

### 4.1 风险评估

| 风险 | 等级 | 缓解 |
|---|---|---|
| `NewBufferFromString("", "", BTScratch)` 在 BufPane 嵌入时 panic | **极低** | InfoPane 用 BTInfo 跑通，BTInfo 和 BTScratch 字段等价 |
| 用户绑定了新 Save 类 action 写进白名单 | 极低 | 白名单改动需要产品明确批准；而且 BTScratch 兜底拦截 |
| `Buf.Save()` 误调 | 极低 | 显式删除调用点（§3.1 改动 4） |
| 旧 `notes.md` 文件遗留 | 低 | D6 不删也不改它；旧文件保留原内容不动，用户可手动删或归档（后续 D8 决定处理策略）。**用户视角**："之前累积的笔记还在，但我之后用 notePane 不会再写盘了" |
| 编译失败 | 极低 | 删字段后所有引用同步删（grep 验证） |

### 4.2 验证步骤

```bash
# 1. 编译
make build-quick
# 预期：通过，零 warning（如果之前有未使用 import 警告，本次正好消掉）

# 2. 启动 microNeo，打开任意文件
# 选一段文字（让 selection 捕获有内容）
# Alt-i 打开 notePane

# 3. 输入一段文字
# 预期：纯文本输入，无 markdown 渲染（buffer Path 为空，initMDConfig 不会启用 markdown）

# 4. 检查磁盘
ls -la ~/.config/microNeo/notes.md
# 预期：文件不存在（这次没创建）
# 如果存在且是新创建的，说明 BTScratch 没生效，回去查

# 5. Alt+Enter 发送
# 预期：notePane 关闭，InfoBar 显示 "✓ sent to <receiver>"

# 6. 再次 Alt-i 打开
# 预期：notePane 是空的（上次内容已丢弃）

# 7. 验证旧文件不受影响
cat ~/.config/microNeo/notes.md 2>/dev/null
# 如果之前有 notes.md：内容保留（不删旧文件，本计划不动它）
# 如果不存在：无输出

# 8. 边界：尝试触发 Save
# 进入命令模式（按 `:`），输入 `save`，回车
# 预期：micro 内部返回 "Cannot save scratch buffer" 错误（来自 save.go:237）
```

### 4.3 回归检查清单

- [ ] 编译通过
- [ ] 打开/关闭 notePane 正常
- [ ] 输入、删除、移动光标、选区都正常
- [ ] Alt+Enter 发送，agent 端收到 message
- [ ] `InfoBar` 显示成功/失败提示
- [ ] 关闭后 `~/.config/microNeo/` 下没有新文件
- [ ] 再次打开，buffer 为空（不残留上次内容）
- [ ] 主编辑器恢复正常焦点（close 里的 Relocate 还需验证）

---

## 五、执行顺序

1. **改 `notepane.go`**：删 `noteFile` 字段、删 `path/filepath` import、改 `NewNotePane()` 的 buffer 创建、删 `close()` 里的 `Buf.Save()`
2. **grep 确认无残留引用**：
   ```bash
   grep -n "noteFile\|notes\.md" /Users/sollawen/pi-dev/microNeo/internal/action/notepane.go
   # 预期：无输出
   ```
3. **`make build-quick`** 验编译
4. **手工跑一遍 §4.2 验证步骤**
5. **commit**（按 commit skill 规范）

---

## 六、后续工作（不在本计划范围）

| 议题 | 后续文档 | 备注 |
|---|---|---|
| 动态行高 5-10 行自适应 | D7? | 边界处理：浮窗越界时向上扩展 |
| 旧 `notes.md` 文件处理 | D8? | 选项：自动归档 / 启动时提示 / 完全不管 |
| 产品文档同步 | D9? | D0 架构设计 §六 改一行（"内容不持久化"）；用户界面-V2.md 改意象描述 |
| 配置文件清理 | D10? | 移除所有 markdown 相关项（实际上从来没用过，但留个 audit） |
| 关闭时的内容清理 | D11? | 当前 close 只是 `isOpen = false`，buffer 内容会保留到下次 NewNotePane；可加 `n.BufPane.Buf.RemoveAllCursors()` 之类的明确清理（实际上 buffer 是 TheNotePane 单例复用，不必清理——下次打开自然覆盖） |

---

## 七、变更摘要

| 维度 | 改动 |
|---|---|
| **代码** | 1 个文件（`notepane.go`），4 处改动，净 -19 行 |
| **配置** | 无 |
| **micro 原生代码** | 无侵入 |
| **EABP 协议** | 不变 |
| **新增依赖** | 无 |
| **删除的东西** | `noteFile` 字段、`path/filepath` import、`Buf.Save()` 调用、notes.md 路径概念 |
| **用户可见变化** | 关闭 notePane 不再写盘；再次打开是空的 |
| **范围外** | 动态行高、配置清理、产品文案、旧文件迁移 |

---

**预计工作量**：单文件改动，10 分钟实施 + 10 分钟验证 + 1 次 commit。

**风险等级**：极低。改动小、模式成熟（InfoPane 先例）、micro 内置兜底（BTScratch 拦截 Save）。
