# Step 0 补充：MD 状态缓存方案

> 问题：滚动到代码块中间时，DetectSegments 只看可见区域，不知道当前在代码块内
> 灵感：micro 的语法高亮引擎已用"逐行缓存状态"解决了同样的问题

---

## 一、问题回顾

```
line 9:  普通文本          ← 不在可见区域
line 10: ```               ← 不在可见区域
line 11: 代码内容          ← visibleStart 从这里开始
line 12: 代码内容
line 13: ```
```

DetectSegments 从 line 11 开始扫描，没看到 line 10 的开标记，导致：
- line 11-12 被误判为段落
- line 13 的 ``` 被误判为新代码块开头
- 后续全部乱掉

只有 code block 有这个问题（blockquote、table、list 每行都有特征标记）。

---

## 二、micro 的做法（已验证可行）

micro 的语法高亮引擎（`pkg/highlight/highlighter.go`）对每一行 buffer 缓存一个 `state`：

```
line.state = 渲染完这一行后，当前处于什么区域
```

- **打开文件**：`HighlightStates()` 从 line 0 扫到末尾，每行存 state
- **渲染时**：`HighlightMatches()` 读 `State(i-1)` → O(1) 知道当前行在哪个区域
- **编辑时**：`ReHighlightStates(startline)` 从修改行往下扫，遇到 state 不变就停止

三个操作都是高效的。

---

## 三、我们的方案

### 3.1 核心思路

仿照 micro，给每行缓存一个 MD 状态。渲染时 O(1) 查询，编辑时增量更新。

### 3.2 状态定义

```go
// MDLineState 记录某一行处理完后的 MD 状态。
// 存储在 BufWindow.mdLineState[lineNum] 中。
type MDLineState struct {
    InCodeBlock bool    // true = 当前在代码块内
    FenceChar   byte    // 代码块围栏字符：'`' 或 '~'，0 表示不适用
    FenceLen    int     // 围栏字符数量（3 个或更多）
}
```

为什么只存 code block？因为只有 code block 有"跨行状态"问题。其他结构（blockquote、table、list）每行都有特征标记，不需要跨行状态。

### 3.3 数据存放

在 `BufWindow` 上新增字段：

```go
type BufWindow struct {
    // ... 现有字段
    mdLineState []md.MDLineState  // 逐行 MD 状态缓存，长度 = buffer 行数
}
```

为什么不放到 buffer 上？因为 MD 状态是渲染关注点，不是数据关注点。BufWindow 是渲染层，放这里更合理。

### 3.4 三个操作

#### 操作 A：全量构建（文件打开时）

```go
// BuildMDLineState 扫描整个 buffer，构建逐行 MD 状态。
// 在 BufWindow 创建时调用一次。
func BuildMDLineState(buf BufferReader) []MDLineState {
    states := make([]MDLineState, buf.LinesNum())
    inCodeBlock := false
    var fenceChar byte
    var fenceLen int

    for i := 0; i < buf.LinesNum(); i++ {
        trimmed := strings.TrimSpace(buf.Line(i))
        if !inCodeBlock {
            if hasFence(trimmed) {
                char, length := parseFence(trimmed)
                inCodeBlock = true
                fenceChar = char
                fenceLen = length
            }
        } else {
            if isMatchingFence(trimmed, fenceChar, fenceLen) {
                inCodeBlock = false
                fenceChar = 0
                fenceLen = 0
            }
        }
        states[i].InCodeBlock = inCodeBlock
        states[i].FenceChar = fenceChar
        states[i].FenceLen = fenceLen
    }
    return states
}
```

时间复杂度：O(N)，N = 文件行数。2000 行文件 < 1ms。

#### 操作 B：渲染时查询（每帧）

```go
// DetectSegments 改造：从 mdLineState 读取初始状态
func DetectSegments(buf BufferReader, visibleStart, visibleEnd int, bufWidth int, lineState []MDLineState) []Segment {
    state := stateNormal
    var startLine int

    // O(1) 初始状态查询
    if visibleStart > 0 && len(lineState) > 0 {
        if lineState[visibleStart-1].InCodeBlock {
            state = stateCodeblock
            // 向上找代码块开标记的行号
            for i := visibleStart - 1; i >= 0; i-- {
                if !lineState[i].InCodeBlock {
                    startLine = i + 1
                    break
                }
                if i == 0 {
                    startLine = 0
                }
            }
        }
    }

    // ... 后续扫描逻辑不变
}
```

注意：需要知道 `startLine`（代码块开标记行号）是因为 segment 需要 `BufStartLine`。可以用二分搜索优化，但一般几行回溯即可。

#### 操作 C：增量更新（编辑时）

```go
// RebuildMDLineState 从 startLine 开始重新扫描，直到状态稳定。
// 在文件被修改时调用（检测到 ``` 行的变化时）。
func RebuildMDLineState(buf BufferReader, states []MDLineState, startLine int) int {
    // 获取 startLine 前一行的状态作为起点
    inCodeBlock := false
    var fenceChar byte
    var fenceLen int
    if startLine > 0 && startLine-1 < len(states) {
        inCodeBlock = states[startLine-1].InCodeBlock
        fenceChar = states[startLine-1].FenceChar
        fenceLen = states[startLine-1].FenceLen
    }

    for i := startLine; i < buf.LinesNum(); i++ {
        trimmed := strings.TrimSpace(buf.Line(i))

        // 重新计算这一行的状态
        newInCodeBlock := inCodeBlock
        newFenceChar := fenceChar
        newFenceLen := fenceLen

        if !inCodeBlock {
            if hasFence(trimmed) {
                char, length := parseFence(trimmed)
                newInCodeBlock = true
                newFenceChar = char
                newFenceLen = length
            }
        } else {
            if isMatchingFence(trimmed, fenceChar, fenceLen) {
                newInCodeBlock = false
                newFenceChar = 0
                newFenceLen = 0
            }
        }

        inCodeBlock = newInCodeBlock
        fenceChar = newFenceChar
        fenceLen = newFenceLen

        // 如果状态没变，可以提前停止
        if i < len(states) {
            if states[i].InCodeBlock == inCodeBlock && states[i].FenceChar == fenceChar {
                return i  // 状态已稳定
            }
            states[i].InCodeBlock = inCodeBlock
            states[i].FenceChar = fenceChar
            states[i].FenceLen = fenceLen
        }
    }
    return buf.LinesNum() - 1
}
```

---

## 四、调用时机

### 4.1 全量构建

在 `bufpane.go` 的 `NewBufPaneFromBuf` 中，设置 IsMD 之后：

```go
if md.IsMarkdownFile(buf.Path) {
    w.IsMD = true
    w.mdLineState = md.BuildMDLineState(buf)
    w.SetMDConfig(...)
}
```

### 4.2 增量更新

在 `bufwindow.go` 的 `displayBufferMD` 中，检测到 `ModifiedThisFrame` 时：

```go
if b.ModifiedThisFrame {
    // 现有 diff 逻辑...
    b.ModifiedThisFrame = false

    // MD: 增量更新行状态
    if w.IsMD && len(w.mdLineState) > 0 {
        // 从修改行开始重建（简化：从修改的最小行号开始）
        // Step 1 实现编辑模式时精确定位修改行
        md.RebuildMDLineState(b, w.mdLineState, 0)
    }
}
```

Step 0 阶段（只读浏览）不需要增量更新，因为没有编辑。Step 1 加入编辑模式时再精确实现。

### 4.3 行数变化

当 buffer 插入/删除行时，`mdLineState` 数组长度需要同步调整：
- 插入 N 行：在对应位置插入 N 个零值
- 删除 N 行：删除对应位置的 N 个元素

---

## 五、startLine 回溯优化

`DetectSegments` 需要知道当前 code block 的开标记行号（作为 segment 的 BufStartLine）。

简单方案：向上线性回溯到 `InCodeBlock == false` 的下一行。最坏 O(N)，但通常只回溯几行。

如果未来需要优化，可以在 `MDLineState` 中加一个 `CodeBlockStart int` 字段，直接记录开标记行号，O(1) 查询。

---

## 六、文件改动清单

| 文件 | 改动 |
|------|------|
| `internal/md/md.go` | 新增 `MDLineState` 结构 |
| `internal/md/detect.go` | 新增 `BuildMDLineState`、`RebuildMDLineState`、辅助函数；`DetectSegments` 增加 `lineState` 参数 |
| `internal/display/bufwindow.go` | 新增 `mdLineState` 字段；`displayBufferMD` 传入 `lineState`；ModifiedThisFrame 时调用增量更新 |
| `internal/action/bufpane.go` | `NewBufPaneFromBuf` 中调用 `BuildMDLineState` |

---

## 七、Step 0 vs Step 1 的范围

| 功能 | Step 0 | Step 1 |
|------|--------|--------|
| 全量构建 mdLineState | ✅ | ✅ |
| DetectSegments 用 lineState | ✅ | ✅ |
| 增量更新（精确修改行） | 不需要（只读） | ✅ |
| 行数变化同步 mdLineState | 不需要（只读） | ✅ |

Step 0 只需实现"全量构建 + 渲染时查询"就够了。
