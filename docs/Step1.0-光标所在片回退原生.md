# Step 1.0：光标所在 segment 回退原生显示

> 前置：Step 0 已完成（背景色渲染管线跑通）
> 交付：MD 文件 editMode=true 时，**光标所在 segment 回退 Micro 原生显示**，其余 segment 继续走 MD 渲染
> 验证方式：临时将 MD 文件默认 editMode 设为 true，打开后即可直接观察回退效果

---

## 验收标准

打开 .md 文件（editMode=true）：

- 光标所在 segment 显示原生文本（带 `#`、`**`、`|` 等标记）
- 其余 segment 继续显示 MD 渲染输出（背景色）
- 上下移动光标：回退的 segment 跟随光标移动
- 多个光标在同一个 seg 内：该 seg 走原生（一个 seg 不会被部分原生部分渲染）
- 多个光标跨多个 seg：每个被光标命中的 seg 独立走原生（多块原生段之间夹 MD 渲染段，符合预期）

非 MD 文件、`mdrender=false` 时不受影响。

---

## 回退判定规则

`editMode && 任意一个光标落在 [seg.BufStartLine, seg.BufEndLine]（含两端）` 则走原生路径，否则走 MD 渲染路径。Step 1.0 中 `editMode` 恒为 `true`，条件退化为"光标在 seg 内"。

```go
// hasCursorInside 判定 seg 内是否有任意光标。
//
// 回退判定（displayBufferMD 主循环使用）：
//   - editMode=true（编辑模式）+ 光标在 seg 内 → 走原生
//   - editMode=false（阅读模式）             → 全部走 MD，不回退
func hasCursorInside(seg md.Segment, cursors []*buffer.Cursor) bool {
    for _, c := range cursors {
        if c.Y >= seg.BufStartLine && c.Y <= seg.BufEndLine {
            return true
        }
    }
    return false
}
```

> **注意**：`buffer.GetCursors()` 返回 `[]*Cursor`（指针切片），因此参数类型是 `[]*buffer.Cursor` 而非 `[]buffer.Cursor`。

---

## 当前代码现状（拆分后）

Step 0 完成后，MD 相关代码已拆分到独立文件：

| 文件 | 职责 |
|------|------|
| `internal/display/bufwindow.go` | 原生 BufWindow 实现，`displayBuffer()`（L388-846，~458 行）、`Display()`（L902-） |
| `internal/display/bufwindow_md.go` | MD 渲染扩展：`SetMDConfig()`、`displayBufferMD()`、`linesFromBuffer()`、`filterSegmentsToVisible()`、`computeRowCounts()`、`drawGutterAndLineNumMD()` |
| `internal/action/bufpane_md.go` | `initMDConfig()` —— BufPane 创建时同步 MD 配置 |
| `internal/action/bufpane.go` | `NewBufPaneFromBuf()` 中调用 `initMDConfig(buf, w)`（L284） |

**BufWindow 结构体**（`bufwindow.go` L35-58）中 MD 字段：
- `mdConfig md.MDConfig` — MD 渲染配置（含 `MDRender` 总开关）
- `mdCache []md.SegmentMeta` — 上一帧 segment 元数据缓存

**Display() 分流**（`bufwindow.go` L906-910）：
```go
if w.Buf.IsMD {
    w.displayBufferMD()
} else {
    w.displayBuffer()
}
```

**displayBufferMD()**（`bufwindow_md.go`）当前是 Step 0 实现，无 `editMode` 参数，无原生回退逻辑。

---

## 改动清单

### 1. BufWindow 增加 editMode 字段

**文件**：`internal/display/bufwindow.go`

在 BufWindow 结构体中（L35-58，MD 字段区域 `mdConfig`/`mdCache` 之后）增加：

```go
    editMode bool // Step 1.0 固定为 true（光标片回退原生）
```

Step 1.0 阶段 `editMode` **没有 setter、没有 getter**——只是一个内部标记，在创建后由 `initMDConfig` 设为 `true` 即可。Step 1.1 会增加 `SetEditMode` 等方法和键盘交互。

### 2. Display() 分流增加 MDRender 开关

**文件**：`internal/display/bufwindow.go`（L906-910）

```go
// Step 0：
if w.Buf.IsMD {
    w.displayBufferMD()
} else {
    w.displayBuffer()
}

// Step 1.0 改为：
if !w.Buf.IsMD || !w.mdConfig.MDRender {
    w.displayBuffer()
} else {
    w.displayBufferMD(w.editMode)
}
```

非 MD 文件或总开关 `MDRender=false` 时，**完全不走 MD 路径**，由原生 `displayBuffer()` 处理。`displayBufferMD(editMode)` 内部按"回退判定规则"分发——`editMode` 由 `initMDConfig` 设为 `true`，Step 1.1 会增加切换机制。

### 3. renderSegmentNative 新建 ★ 本步核心改动

**文件**：`internal/display/bufwindow_md.go`（新增函数，约 458 行）

#### 3.1 设计原则：复制而非重构

**`displayBuffer()` 保持原样，0 改动**（符合 `CLAUDE.md`"对 micro 原生代码的侵入越小越好"）。

本步在 `bufwindow_md.go` 中新增一个**独立函数** `renderSegmentNative`，把 `displayBuffer()` 主循环（L388-846，约 458 行）原样复制过来，**只改 5 处**（含 1 处新增返回值）：

1. **bloc.Y 初值改为 `seg.BufStartLine`**（原版用 `w.StartLine.Line`，现在 seg 已被裁剪到可见区，从 seg 起始行开始）
2. **vloc.Y 初值 = `startVY`，仅首 seg 保留 softwrap offset**（见下方详细说明）
3. **循环条件加 `bloc.Y <= seg.BufEndLine`**（双重终止：屏幕满 + buffer 行用完）
4. **末尾 `bloc.Y >= b.LinesNum()` 改为 `bloc.Y > seg.BufEndLine`**
5. **perLineCounts 实时统计每个 buffer 行消耗的 screen 行数**（支持 softwrap）

**不需要** `displayContext` 结构体——`displayBuffer()` 不重构，新函数内部闭包捕获的变量全是 local var，与 `displayBuffer()` 完全对称。

#### 3.2 函数签名

```go
// renderSegmentNative 用原生逻辑渲染一个 segment 到 BufWindow。
// seg 必须已经过 filterSegmentsToVisible 裁剪（BufStartLine/BufEndLine 在可见区内）。
// 不修改 displayBuffer()，从那里复制主循环并按需调整。
//
// startVY : 起始 screen 行（相对窗口顶部）
// 返回:
//   newVY         - 渲染后的 vY（=startVY + 实际消耗的 screen 行数）
//   perLineCounts - 每个 buffer 行占几个 screen row（长度 = seg.BufEndLine-seg.BufStartLine+1）
//                   softwrap on 时一行可占多行 screen row
func (w *BufWindow) renderSegmentNative(
    seg md.Segment, startVY int,
) (newVY int, perLineCounts []int)
```

#### 3.3 从 displayBuffer() 复制的具体清单

**完整复制**（参考 `bufwindow.go` 行号 L388-846；行号会随代码漂移，以实际为准）：

| 内容 | 当前行号 | 说明 |
|------|---------|------|
| 前置：`b := w.Buf`、宽高检查、maxWidth、`modified-this-frame`、`matchingBraces` 解析 | L389-446 | 完整复制 |
| 前置：lineNumStyle/curNumStyle、softwrap/wordwrap、tabsize/colorcolumn、showchars 解析、vloc/bloc 初始化、cursors、curStyle、外层 for 头 | L448-483 | 完整复制 |
| 闭包 `getRuneStyle` | L528-616 | 完整复制 |
| 闭包 `draw` | L617-693 | 完整复制 |
| 闭包 `wrap` | L694-722 | 完整复制 |
| word buffer 初始化 | L723-729 | 完整复制 |
| 内层 rune 解码 + 渲染 + wrap 主循环 | L730-819 | 完整复制 |
| EOL 填充 + newline-in-selection | L820-842 | 完整复制 |
| 末尾 `bloc.Y++` 和 `if bloc.Y >= b.LinesNum()` | L840-842 | 复制 + 调整 |
| 结尾大括号 | L843-846 | 完整复制 |

**复制后修改 5 处**：

**(1) bloc.Y 初值改为 seg.BufStartLine**（原 L467 附近）：

```go
// 原版：
bloc := buffer.Loc{X: -1, Y: w.StartLine.Line}
// 改为：
bloc := buffer.Loc{X: -1, Y: seg.BufStartLine}
```

理由：`renderSegmentNative` 渲染的是单个 seg，应从 `seg.BufStartLine` 开始，而非从视口起始行 `w.StartLine.Line` 开始。`X: -1` 保留不变（后面 `bloc.X = w.StartCol` 会覆盖）。

**(2) vloc.Y 初值改为 startVY，首 seg 保留 softwrap offset**（原 L460-464）：

```go
// 原版：
vloc := buffer.Loc{X: 0, Y: 0}
if softwrap {
    vloc.Y = -w.StartLine.Row
}
// 改为：
vloc := buffer.Loc{X: 0, Y: startVY}
if softwrap && seg.BufStartLine == w.StartLine.Line {
    // 视口可能从该 buffer 行的中间 screen row 开始，
    // 需要和原版一样跳过上方已滚出屏幕的部分
    vloc.Y -= w.StartLine.Row
}
```

理由：`filterSegmentsToVisible` 裁剪后，只有视口起始行所在的 seg 可能有 partial row（`StartLine.Row > 0`）。其余 seg 的首行都是完整的 buffer 行，不需要 offset。`startVY` 由调用方传入，保证 ≥ 0；减去 `StartLine.Row` 后可能 < 0，与原版行为一致（vloc.Y < 0 的行跳过 gutter 绘制，直接渲染到屏幕上方不可见区域）。

**(3) 循环条件加 seg 边界**（原 L456 附近）：

```go
// 原版：
for ; vloc.Y < w.bufHeight; vloc.Y++ {
// 改为：
for ; vloc.Y < w.bufHeight && bloc.Y <= seg.BufEndLine; vloc.Y++ {
```

**(4) 末尾 break 条件改为 seg 边界**（原 L841）：

```go
// 原版：
if bloc.Y >= b.LinesNum() {
    break
}
// 改为：
if bloc.Y > seg.BufEndLine {
    break
}
```

**(5) 循环体内实时统计 perLineCounts，循环结束后返回**：

在外层 for 循环体**开头**（gutter 绘制之前）记录当前行起始 vY：

```go
lineStartVY := vloc.Y
```

在循环体**末尾**（`bloc.Y++` 之前）统计本行消耗的 screen 行数：

```go
perLineCounts = append(perLineCounts, vloc.Y-lineStartVY+1)
```

循环结束后返回：

```go
return vloc.Y, perLineCounts
```

> **说明**：softwrap on 时，wrap 闭包内的 `vloc.Y++` 会让一个 buffer 行消耗多个 screen row。通过记录 `lineStartVY` 并在行结束时做差值，天然得到正确的行数统计。softwrap off 时每行消耗 1 row，差值恒为 1，逻辑自洽。

### 4. hasCursorInside 辅助函数

**文件**：`internal/display/bufwindow_md.go`（新增，约 10 行）

独立函数（非方法），放在 `hasCursorInside` 在 `bufwindow_md.go` 中，供 `displayBufferMD` 主循环调用。

### 5. renderSegmentMD 提取 ★ 从 displayBufferMD 抽出 MD 渲染逻辑

**文件**：`internal/display/bufwindow_md.go`

把现有 `displayBufferMD()` 中的 **per-segment 渲染逻辑**（"取行 → render → 行号绝对化 → 写 screen"）提取为独立方法 `renderSegmentMD`。

`displayBufferMD()` 主循环简化为：遍历 segments → 分发到 `renderSegmentMD` 或 `renderSegmentNative` → 统一写 `mdCache`。

#### 5.1 renderSegmentMD 签名

```go
// renderSegmentMD 渲染单个 MD segment 到 screen。
// 与 renderSegmentNative 对称：纯函数，渲染 + 返回 (newVY, perLineCounts)。
// 不写 mdCache——由 displayBufferMD 主循环统一写。
func (w *BufWindow) renderSegmentMD(
    seg md.Segment, vY int,
) (newVY int, perLineCounts []int)
```

#### 5.2 提取范围

从现有 `displayBufferMD()`（`bufwindow_md.go`）中提取以下代码到 `renderSegmentMD`：

1. `w.linesFromBuffer(seg.BufStartLine, seg.BufEndLine)` — 取行
2. `seg.Render(...)` — 渲染
3. BufLine 绝对化循环
4. 行渲染主循环（gutter + 内容写入 screen）
5. `computeRowCounts(rendered)` — 构造 perLineCounts
6. 返回 `(vY, perLineCounts)`

`mdCache` 写入提到 `displayBufferMD` 主循环统一处理。

### 6. displayBufferMD 重写 ★ 本步主要逻辑改动

**文件**：`internal/display/bufwindow_md.go`

#### 6.1 目标

签名改为 `displayBufferMD(editMode bool)`，主循环里按"回退判定规则"分发到两个路径：
- **MD 路径**（`renderSegmentMD`）：现有逻辑
- **原生路径**（`renderSegmentNative`）

#### 6.2 伪代码

```go
func (w *BufWindow) displayBufferMD(editMode bool) {
    b := w.Buf
    if w.Height <= 0 || w.Width <= 0 { return }

    if b.ModifiedThisFrame {
        if b.Settings["diffgutter"].(bool) { b.UpdateDiff() }
        b.ModifiedThisFrame = false
    }

    visibleStart := w.StartLine.Line
    visibleEnd := visibleStart + w.bufHeight
    if visibleEnd >= b.LinesNum() { visibleEnd = b.LinesNum() - 1 }

    w.mdCache = w.mdCache[:0]
    segments := filterSegmentsToVisible(b.MDSegments, visibleStart, visibleEnd)
    cursors := b.GetCursors()

    vY := 0
    for _, seg := range segments {
        var perLine []int
        if editMode && hasCursorInside(seg, cursors) {
            // ★ 原生路径：renderSegmentNative 内部自己处理 gutter
            vY, perLine = w.renderSegmentNative(seg, vY)
        } else {
            // ★ MD 渲染路径
            vY, perLine = w.renderSegmentMD(seg, vY)
        }
        w.mdCache = append(w.mdCache, md.SegmentMeta{
            BufStartLine: seg.BufStartLine,
            BufEndLine:   seg.BufEndLine,
            RowCounts:    perLine,
        })
    }

    // 填剩余空间
    defStyle := config.DefStyle
    for ; vY < w.bufHeight; vY++ {
        w.drawGutterAndLineNumMD(vY, -1)
        for col := 0; col < w.bufWidth; col++ {
            screen.SetContent(w.X+w.gutterOffset+col, w.Y+vY, ' ', nil, defStyle)
        }
    }
}
```

#### 6.3 关键注意点

1. **gutter 只画一次**：原生路径由 `renderSegmentNative` 内部调 `drawGutter`/`drawLineNum`；MD 路径由 `renderSegmentMD` 调 `drawGutterAndLineNumMD`。**`displayBufferMD` 主循环不再统一调 gutter**——避免画两次。

2. **行号风格一致**：原生 gutter 用 `line-number` / `current-line-number` 配色（`drawLineNum` 内部读取 `config.Colorscheme`），和 MD 路径的 `drawGutterAndLineNumMD` **完全共用同一套配色**——视觉上无差异。

3. **mdCache 写法对称**：两个分支都写 `mdCache`，`BufStartLine/BufEndLine/RowCounts` 字段一致，下游消费代码无需区分路径。

### 7. initMDConfig 中设置 editMode=true

**文件**：`internal/action/bufpane_md.go`

Step 1.0 阶段 **不增加新方法**——在 `initMDConfig()` 末尾直接设置 `editMode`：

```go
func initMDConfig(buf *buffer.Buffer, w *display.BufWindow) {
    if !buf.IsMD {
        return
    }
    w.SetMDConfig(md.MDConfig{...})
    // Step 1.0：MD 文件默认进入编辑模式（光标片回退原生）
    // editMode 是 BufWindow 的未导出字段，同包访问可以直接设置
    // 但 initMDConfig 在 action 包，跨包需要导出方法或字段
}
```

> **跨包问题**：`initMDConfig` 在 `action` 包，`editMode` 是 `display.BufWindow` 的未导出字段。解决方案：
>
> - **方案 A**（推荐）：在 `BufWindow` 上加一个 `SetEditMode(bool)` 方法（~3 行），在 `initMDConfig` 中调用 `w.SetEditMode(true)`。
> - **方案 B**：`editMode` 改为导出字段 `EditMode`。
>
> Step 1.0 建议方案 A：给 `BufWindow` 加 `SetEditMode(bool)` 方法（约 3 行）。Step 1.1 反正要加键盘切换，这个方法刚好复用。

Step 1.1 会：
- 把 `initMDConfig` 里的 `w.SetEditMode(true)` 去掉（默认 `false`，阅读模式）
- 新增键盘拦截调用 `SetEditMode` 切换

---

## 实施顺序

```
1. BufWindow editMode 字段 + SetEditMode 方法（bufwindow.go，~4 行）
2. renderSegmentNative 新建（bufwindow_md.go，从 displayBuffer() 复制 ~458 行 + 改 5 处）
3. hasCursorInside 辅助函数（bufwindow_md.go，~10 行）
4. renderSegmentMD 提取（bufwindow_md.go，从 displayBufferMD 抽出 MD 渲染逻辑，~80 行）
5. displayBufferMD 重写（bufwindow_md.go，签名改为带 editMode 参数 + if/else 分发，~30 行）
6. Display() 分流逻辑（bufwindow.go，增加 MDRender 开关判断 + 传 w.editMode）
7. initMDConfig 调用 w.SetEditMode(true)（bufpane_md.go，1 行）
8. 验证：
   - 打开 MD 文件：非光标所在 seg 仍渲染（Step 0 行为不破坏）
   - 移动光标进入 seg：该 seg 切回原生（带 #、**、| 等标记）
   - 多光标测试：跨 seg 时多个 seg 独立走原生
   - 对比 displayBuffer() 原生输出：行号、gutter、syntax 高亮视觉一致
```

---

## 改动文件

| 文件 | 改动 | 类型 |
|------|------|------|
| `internal/display/bufwindow.go` | `editMode bool` 字段、`SetEditMode(bool)` 方法（~4 行）、`Display()` 分流改动（~4 行） | 微改 |
| `internal/display/bufwindow_md.go` | `renderSegmentNative` 新建（~458 行）、`hasCursorInside` 新建（~10 行）、`renderSegmentMD` 提取（~80 行）、`displayBufferMD` 重写（~30 行） | 主要改动 |
| `internal/action/bufpane_md.go` | `initMDConfig` 末尾加 `w.SetEditMode(true)`（1 行） | 微改 |

**不变**：
- `internal/display/bufwindow.go` 中的 `displayBuffer()` — **0 改动**
- `internal/action/bufpane.go` — **0 改动**（`initMDConfig` 已在 `bufpane_md.go` 中）

---

## 风险

| 风险 | 缓解 |
|------|------|
| **代码克隆 ~458 行**（`renderSegmentNative` 复制自 `displayBuffer`） | 严格遵守 CLAUDE.md "对原生代码侵入最小"；未来 micro 上游升级时需手动 diff 同步，但 Step 1.0 阶段可接受 |
| 复制时漏改某处（如 softwrap offset 逻辑未正确保留） | 6 处修改清单明确列出；特别是改动 (2) 只在首 seg 保留 offset，其余 seg 跳过。测试时可开启 softwrap，验证首行 partial row 和跨行 wrap 的 vY 计数 |
| softwrap on 时原生路径与 MD 路径的 vY 步长不一致 | 不影响：两条路径各自返回 `(newVY, perLineCounts)`，vY 通过返回值串联，天然对齐。perLineCounts 如实反映实际消耗的 screen 行数 |
| 原生路径与 MD 路径的 gutter 风格不一致 | 共用 `line-number` / `current-line-number` 配色（`drawGutter`/`drawLineNum` 本来就是 micro 的），走原生路径实际上就是用 micro 的原生行号绘制——必然一致 |
| 多光标跨多个 seg 时的视觉碎片化 | 设计上接受"多个原生块之间夹 MD 渲染块"；符合 Step 1.0 目标 |
| `hasCursorInside` 在大 seg（占满屏幕）下命中率 100% | 不影响功能：大 seg 全走原生，符合"光标所在的整片都用原生编辑"的语义 |

**估计代码量**：~575 行（renderSegmentNative ~458 + renderSegmentMD ~80 + hasCursorInside ~10 + displayBufferMD 重写 ~30 + editMode/SetEditMode ~4 + Display 分流 ~4 + initMDConfig ~1）
