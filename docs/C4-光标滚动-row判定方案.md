# C4：光标滚动 — viewportRowLoc 二维表方案

> 关联：根因见 `docs/C0-光标滚动-现状调研.md`；旧 C4（已废弃）的 Bug1 诊断见 `docs/C4b-光标滚动-Bug1诊断与方案缺陷.md`。
>
> 本文是 C4 的**重写**。旧 C4 用 `SLoc{line, 0}`（Row 恒 0）做动作，在 softwrap 续行和装饰行处产生累积偏差。本文用 `(line, segmentRow)` 二维表替代。

---

## 0. 为什么重写

旧 C4 的向下动作为：
```go
w.StartLine = SLoc{w.viewportRowBufLine[delta], 0}   // Row 硬编码 0
```

`Row` 永远是 0，意味着"新视口顶永远是某 buffer 行的行首"。但正确的 StartLine 经常是"某长行的第 k 个 softwrap 段"（`{line, k}`）。这个偏差在 softwrap 长行（如超长段落）和装饰密集区（表格/标题）累积，导致光标飞出视口底部——即 Bug1。

**根因**：`viewportRowBufLine` 只存了 buffer 行号（一维），没有存"该屏行是这一行的第几个 softwrap 段"。对"视口顶边恰好落在某长行的第 k 段"这种状态，数组里**没有信息**。

## 1. 问题边界

MD 渲染文件中，光标停在屏幕底边按 ↓ 不触发上滚、光标飞出视口。仅 MD 文档有此问题。

**本方案只处理"用户按上下键导致滚动"的场景**。鼠标滚轮滚动当前工作正常，不在范围内。

关键前提（上下键场景恒成立）：
- 方向键是非 ESC 键 → `editMode = true`（`bufpane_md.go:72`）
- → 光标所在段走 `renderSegmentNative`（`bufwindow_md.go:728`）
- → 光标行无装饰行，原生 1:1 渲染

## 2. 核心洞察

### 2.1 我们手里有准确的信息

| 信息 | 来源 | 准确性 |
|------|------|--------|
| 光标的 buffer line + segmentRow | `SLocFromLoc(activeC.Loc)` = `{c.Line, c.Row}` | 绝对准确（micro softwrap 算法） |
| 每个屏行的 buffer line + segmentRow | 渲染时逐屏行记录 | 绝对准确（渲染器走过每个屏行） |

**这两个信息一对照，光标在哪个屏行、该不该滚、滚到哪，全是直接查表，无需任何反推或近似。**

### 2.2 旧 C4 丢精度的两个点

1. **`viewportRowBufLine` 只存 line，不存 segmentRow** → 动作端 `Row` 只能填 0。
2. **装饰行存的是 effectiveLine（非负）而非 -1**（`renderSegmentMD:134` append 的是 effectiveLine）→ 反查 line 会命中装饰行。

本文同时修这两点。

## 3. 方案

### 3.1 数据结构：`viewportRowmap []SLoc`

把现有的一维 `viewportRowBufLine []int` 升级为二维 `viewportRowmap []SLoc`。

> `SLoc` 是 micro 原生类型（`softwrap.go:13`，`{Line, Row}`），表示"buffer 第 Line 行的第 Row 个 softwrap 段"。`StartLine` 本身就是 `SLoc`，复用它零转换（`w.StartLine = w.viewportRowmap[delta]`）。

```go
// viewportRowmap[i] = viewport 第 i 个屏幕行对应的视觉行位置
// Line = -1：装饰行（标题下划线、表格 frame 等）
// Line = -2：空白填充区域（buffer 内容不够填满 viewport）
// Line >= 0：内容行；Row = 该屏行是此 buffer 行的第几个 softwrap 段（0-based）
viewportRowmap []SLoc
```

示例（一张长行 + 装饰的视口）：

| viewport row | Line | Row (segmentRow) | 含义 |
|--------------|------|------------------|------|
| 0 | 15 | 3 | line15 的第 4 个 softwrap 段（视口顶落在行中间）|
| 1 | 15 | 4 | line15 续段 |
| 2 | -1 | -1 | 装饰行 |
| 3 | 16 | 0 | line16 行首 |
| 4 | -1 | -1 | 装饰行 |
| 5 | 17 | 0 | line17 行首 |
| 6 | 17 | 1 | line17 续段 |
| 7 | 17 | 2 | line17 续段 |
| 8 | 18 | 0 | line18 行首 |

### 3.2 判定端：何时滚

光标下移后 `c = SLocFromLoc(activeC.Loc) = {c.Line, c.Row}`。在 `viewportRowmap` 里**精确匹配 `(Line, Row)` 二元组**，得到光标屏行 `cursorRow`：

```go
func (w *BufWindow) lineToScreenRow(line, row int) (int, bool) {
	for i, v := range w.viewportRowmap {
		if v.Line == line && v.Row == row {
			return i, true
		}
	}
	return 0, false
}
```

为什么精确：光标段原生渲染，其 segmentRow 用 micro softwrap 算法记录；`c.Row` 也用 micro softwrap 算法（`SLocFromLoc`）。两者一致 → 二元组精确命中，**不会命中装饰行**（-1 ≠ c.Line），**不会命中同行的其他 softwrap 段**（Row 不同）。

```go
botMarginRow := height - 1 - scrollmargin
if cursorRow > botMarginRow { /* 向下滚 */ }
if cursorRow < scrollmargin    { /* 向上滚 */ }
```

### 3.3 动作端：怎么滚（三 case 统一）

向下滚：视口上移 `delta = cursorRow - botMarginRow` 行。当前屏行 `delta` 成为新 row 0：

```go
delta := cursorRow - botMarginRow
w.StartLine = w.viewportRowmap[delta]   // 直接读 (Line, Row)
```

这一条规则吸收了旧 C4 需要分支的三个 case：

| `viewportRowmap[delta]` 的值 | 对应场景 | 新 StartLine |
|----------------------------------|----------|--------------|
| `{11, 0}` | delta 落在下一行行首 | `{11, 0}` ✓ |
| `{-1, -1}` | delta 落在装饰行 | 向下找首个 `Line>=0` 的条目（见 §6.5）|
| `{10, 3}` | delta 落在长行续段 | `{10, 3}` ✓ —— **旧 C4 在这里丢精度（填 `{10,0}`）** |

向上滚：沿用原生 `w.StartLine = w.Scroll(c, -scrollmargin)`。`StartLine` 离光标仅 scrollmargin（默认 3）行，几乎总在光标段内（原生渲染、无装饰）→ 1:1 精确。

## 4. segmentRow 的计算与直接写入

**两个渲染函数不再返回 `rowBufLines` / `rowBufLocs`，而是在画屏行时直接写入 `w.viewportRowmap[vloc.Y]`。** 渲染函数只返回 `newVY`。

### 4.1 `renderSegmentNative`（光标段，需精确）

`vloc` 是 `buffer.Loc`（无 SLoc.Row），但 segmentRow 可由屏行相对该行起始的偏移推算。该 buffer 行（`bloc.Y`）从 `lineStartVY` 开始画，占 `lineStartVY..vloc.Y` 连续屏行：

```go
// 该行起始屏行对应的 segmentRow：
//   若该行 == StartLine.Line，前 StartLine.Row 段已滚出顶部，可见起始 = StartLine.Row
//   否则从 0 开始
rowOffset := 0
if bloc.Y == w.StartLine.Line {
	rowOffset = w.StartLine.Row
}
for screenRow := lineStartVY; screenRow <= vloc.Y; screenRow++ {
	if screenRow >= 0 && screenRow < bufHeight {
		w.viewportRowmap[screenRow] = SLoc{bloc.Y, rowOffset + (screenRow - lineStartVY)}
	}
}
```

> 替换原 `:672-674` 的 `rowBufLines = append(rowBufLines, bloc.Y)`。
> `screenRow >= 0` 守护：首段首行有 `vloc.Y -= w.StartLine.Row` 偏移，`lineStartVY` 可能为负，负屏行画在视口外、不该进 `viewportRowmap`（保持 -2）。

### 4.2 `renderSegmentMD`（非光标段，计数法）

渲染器输出的 `rendered.Rows` 有序，同一 BufLine 的折行段连续排列。复用现有的 `softwrapped` 判断（`:132`）：

```go
var segRow int  // 在循环外初始化
// 循环内：
if row.BufLine < 0 {
	segRow = -1                                              // 装饰行
} else if row.BufLine == lastBufLine {
	segRow++                                                 // 同一 buffer 行的续段
} else {
	segRow = 0                                               // 新 buffer 行
}
if vY >= 0 && vY < bufHeight {
	w.viewportRowmap[vY] = SLoc{row.BufLine, segRow}         // ★ 用 row.BufLine（非 effectiveLine）
}
lastBufLine = row.BufLine
```

> 替换原 `:134` 的 `rowBufLines = append(rowBufLines, effectiveLine)`。
> append 用 `row.BufLine`（装饰行=-1），**不再用 effectiveLine**。effectiveLine 逻辑（:117-122）仅保留用于可见性判断（:124），不影响存储。

MD 段的 segmentRow 按 MD 渲染器 softwrap（`wrapCells`）计数，与 micro softwrap 可能不一致。但这只影响"光标跨段进入 MD 段"的判定——此时二元组反查 miss，走 fallback（§10），不影响同段（native）的精确判定。

## 5. 配套修复：信任渲染器的 BufLine

### 5.1 原则

**渲染器的 `row.BufLine` 是唯一真相源。** C4 的修复就是：把 `renderSegmentMD:134` 从 append `effectiveLine` 改为直接写 `viewportRowmap[vY] = SLoc{row.BufLine, segRow}`——渲染器标什么就存什么，不再用 effectiveLine 覆盖。

### 5.2 实测分类（逐行验证过渲染器代码）

判断标准（你的定义）：**确实没有对应 buffer 行的，才是 -1 装饰行**。

| 元素 | `row.BufLine` | 依据 |
|------|---------------|------|
| codeblock 顶边框 `┌────python` | **真实**（0 = ```python 围栏）| `render_codeblock.go:173` |
| codeblock 代码行 | 真实 | `:199/209` |
| codeblock 底边框 `└────` | **真实**（len-1 = ``` 闭合围栏）| `:254` |
| 标题文本行 | 真实 | `wrapCells(..., lineIdx)` |
| 标题下划线 `---` | -1 | `makeDecoRow` |
| hr 分隔线 `---` | **真实**（0）| `render_hr.go:10` |
| 表格顶边框 | -1 | `makeTableTopBorder:580` |
| 表格 header 行 | 真实（sepIdx-1）| `render_table.go:821` |
| 表格 header 分隔线 `\|---\|` | **-1**（渲染器标的，虽源码是真实行）| `makeTableSeparator` |
| 表格 body 行 | 真实 | `:833` |
| 表格行间分隔线 | -1 | `makeTableSeparator:839` |
| 表格底边框 | -1 | `makeTableBottomBorder:645` |

### 5.3 表格 header 分隔线的分歧（仅记录，不影响 C4）

源码里 `|---|` 是真实 buffer 行（`parseTable` 用 `pt.sepIdx` 识别），但 `makeTableSeparator` 把它标成 -1。这与"真实行才非 -1"的直觉不符，是渲染器的选择。

是否改成真实行号是**渲染器层的问题，不属 C4**。C4 信任 `row.BufLine`，无论它标 -1 还是真实号都正常工作——`lineToScreenRow` 只精确匹配光标段（native 渲染、无 -1 行），MD 段的标记值只影响"Line<0 就跳过找下一个内容行"，两种取值都安全。

### 5.4 对判定/动作的影响

修复后 `viewportRowmap` 的装饰行（-1）与真实行（≥0）完全反映渲染器语义。`lineToScreenRow` 不会误命中装饰行（-1 ≠ c.Line）。这是 §3.2 判定端的前提。

## 6. 实现代码

### 6.1 数据结构（`bufwindow.go`）

**删除**原字段 `viewportRowBufLine []int`，**新增** `viewportRowmap []SLoc`。
不是并存——`.Line` 已包含原数组的全部信息，保留两个数组是冗余且易失同步。

### 6.2 `renderSegmentMD`（`bufwindow_md.go`）

- 返回签名 `rowBufLines []int` → 只返回 `newVY`
- 循环内按 §4.2 计算 segRow，直接写 `w.viewportRowmap[vY] = SLoc{row.BufLine, segRow}`
- 保留 effectiveLine 逻辑用于可见性判断

### 6.3 `renderSegmentNative`（`bufwindow_md.go`）

- 返回签名 `rowBufLines []int` → 只返回 `newVY`
- `:672-674` 按 §4.1 计算 segmentRow，直接写 `w.viewportRowmap[screenRow] = SLoc{bloc.Y, segRow}`

### 6.4 `displayBufferMD`（`bufwindow_md.go`）

删除 `rowBufLines` / `rowBufLocs` 中转变量与 copy 逻辑，主循环简化为：

```go
// 重置为 {Line:-2}（空白）
for i := range w.viewportRowmap {
	w.viewportRowmap[i] = SLoc{Line: -2}
}
// ...
for _, seg := range segments {
	if editMode && hasCursorInside(seg, cursors) {
		vY = w.renderSegmentNative(seg, vY)
	} else {
		vY = w.renderSegmentMD(seg, vY)
	}
}
```

> `renderSegmentNative` 末尾的 `vloc.Y++`（首行负偏移 + 末行 softwrap）可能让 vY 超 bufHeight。
> 这与原版一致（原版也画到视口外不崩溃）。直接写方案下，渲染函数内部的 `screenRow < bufHeight` / `vY < bufHeight` 守护确保不越界。原 Bug2（copy 越界）的根源被清除。

### 6.5 `relocateVerticalMD`（重写）

```go
// relocateVerticalMD 是 Relocate 的 MD 垂直滚动分支。
// 判定：lineToScreenRow 精确匹配光标 (Line, Row) → 屏行。
// 动作：向下读 viewportRowmap[delta]（含 segmentRow），向上沿用原生算术。
// 边界场景（首帧 / 光标跳出视口 / delta 落空白尾）走 relocateVerticalNativeFallback。
func (w *BufWindow) relocateVerticalMD(c SLoc, scrollmargin, height int) bool {
	n := len(w.viewportRowmap)
	if n == 0 {
		return w.relocateVerticalNativeFallback(c, scrollmargin, height) // 首帧
	}
	cursorRow, ok := w.lineToScreenRow(c.Line, c.Row)
	if !ok {
		return w.relocateVerticalNativeFallback(c, scrollmargin, height) // 光标跳出视口
	}
	botMarginRow := height - 1 - scrollmargin
	if cursorRow > botMarginRow {
		delta := cursorRow - botMarginRow
		if delta >= n {
			return w.relocateVerticalNativeFallback(c, scrollmargin, height)
		}
		loc := w.viewportRowmap[delta]
		// delta 落装饰/空白：向下找首个内容行作为新视口顶
		for loc.Line < 0 && delta+1 < n {
			delta++
			loc = w.viewportRowmap[delta]
		}
		if loc.Line < 0 {
			return w.relocateVerticalNativeFallback(c, scrollmargin, height)
		}
		w.StartLine = loc // 精确：(Line, segmentRow)
		return true
	}
	if cursorRow < scrollmargin {
		w.StartLine = w.Scroll(c, -scrollmargin) // 向上：段内 1:1
		return true
	}
	return true
}
```

### 6.6 辅助函数：简化命名 + 互逆对

最终两个函数，互为逆操作，命名简洁对称：

| 函数 | 方向 | 用途 |
|---|---|---|
| `screenRowToLine(screenRow int) (int, bool)` | 屏行 → 行号 | 点击映射（`bufwindow.go:300`）|
| `lineToScreenRow(line, row int) (int, bool)` | (行号,段) → 屏行 | Relocate 滚动判定（§3.2）|

- **`screenRowToLine`**：由现有 `screenOffsetToBufferLine`（`:837`）改名，读 `w.viewportRowmap[screenRow].Line`，丢 Row（点击只需行号）。返回 `(0, false)` 当该行是装饰/空白。
- **`lineToScreenRow`**：新增，精确匹配 `(Line, Row)` 二元组（§3.2 代码）。
- **删除** `bufferLineToScreenOffset`（`:851`，旧 C4 的倒序版）。它唯一调用方是旧 `relocateVerticalMD`，后者被重写后无人调用。Go 不支持函数重载，也不需要倒序版——新方案的精确二元组匹配取代了旧的"取最后匹配"近似。
- **删除** `dumpScroll`（临时调试日志）及其 `fmt`/`os` import。

### 6.7 `Relocate` 分发（`bufwindow.go`，不变）

```go
if w.Buf.IsMD && w.mdConfig.MDRender {
	ret = w.relocateVerticalMD(c, scrollmargin, height)
} else {
	// micro 原生垂直 Relocate（原样，仅缩进）
}
```

## 7. 改动文件

| 文件 | 改动 |
|------|------|
| `internal/display/bufwindow.go` | **删除** `viewportRowBufLine []int` 字段，**新增** `viewportRowmap []SLoc`；click 映射读 `.Line` |
| `internal/display/bufwindow_md.go` | 两个 render 函数只返回 `newVY` + 直接写 `viewportRowmap`；`displayBufferMD` 删除 `rowBufLines`/copy 逻辑；重写 `relocateVerticalMD`；新增 `lineToScreenRow`；`screenOffsetToBufferLine` 改名为 `screenRowToLine`；删除 `bufferLineToScreenOffset`、`dumpScroll` |
| `internal/md/render_table.go` | 配套修复：header 分隔线 BufLine 从 -1 改为真实行号（§13） |

**未涉及**：`softwrap.go`、`actions.go`、`Relocate` 非 MD 分支。渲染器 `internal/md/*` 仅 `render_table.go` 有配套修复（见 §13）。

## 8. 实施步骤

1. `bufwindow.go`：字段改名 + click 映射适配。
2. `bufwindow_md.go`：两个 render 函数删 `rowBufLines` 返回值，改为直接写 `w.viewportRowmap` + segmentRow 计算；装饰行写 `row.BufLine`（-1）。
3. `bufwindow_md.go`：`displayBufferMD` 删 `rowBufLines` 局部变量、copy、startVY 逻辑，简化为纯渲染循环。
4. `bufwindow_md.go`：重写 `relocateVerticalMD`（§6.5）；新增 `lineToScreenRow`；`screenOffsetToBufferLine` 改名为 `screenRowToLine`；删除 `bufferLineToScreenOffset`、`dumpScroll`、`fmt`/`os` import。
5. `render_table.go`：按 §13 修复 header 分隔线 BufLine。
6. `make build-quick` 编译。
7. 跑 §11 测试用例。

## 9. 正确性论证

### 9.1 非 MD 零影响
非 MD 时走 else 分支（micro 原生），`viewportRowmap` 不被读写。行为与原版一致。

### 9.2 同段移动完全精确（核心场景）
光标段原生渲染 → `viewportRowmap` 里光标行的 `(Line, Row)` 用 micro softwrap 记录 → 与 `c.Row`（同为 micro softwrap）一致 → `lineToScreenRow` 精确命中 → 判定准；动作读 `viewportRowmap[delta]` 的 `(Line, Row)` → StartLine 精确（含 segmentRow）。**无累积偏差。**

### 9.3 装饰行不再干扰
装饰行存 `{Line:-1}` → `lineToScreenRow` 不会匹配（-1 ≠ c.Line）→ 判定不误命中；动作端 delta 落装饰行时向下找首个内容行（§6.5）。

## 10. 边界场景与兜底

| 场景 | 处理 |
|------|------|
| 首帧（`viewportRowmap` 未构建）| `n==0` → fallback |
| 光标跳出视口（goto-line/搜索）| `lineToScreenRow` miss → fallback，下一帧自纠正 |
| 光标跨段进入 MD 段 | MD 段 segmentRow 按 `wrapCells` 计数，可能与 `c.Row`（micro softwrap）不一致 → miss → fallback。下一帧光标段变 native，精确 |
| delta 落空白尾部 | 向下找不到内容行 → fallback |

`relocateVerticalNativeFallback`（复刻 micro 原生垂直 Relocate）保留，与旧 C4 相同。

## 11. 测试用例

| # | 操作 | 预期 |
|---|------|------|
| 1 | `sample.md` 从 line1 连续按 ↓ 到底 | 光标始终停在底边 margin，**不飞出**，无累积偏差 |
| 2 | 经过超长段落（line68 区域）按 ↓ 触发滚动 | 光标精确停在 margin（旧 C4 在此偏差达 4 行）|
| 3 | 经过表格区（line54-58）按 ↓ 触发滚动 | 光标精确停在 margin（旧 C4 在此飞出）|
| 4 | 到底后连续按 ↑ | 光标不飞出顶部 |
| 5 | 非 MD 文件同样操作 | 与原版完全一致 |
| 6 | `./microneo docs/sample.md` 启动 | 不崩溃（首帧 fallback）|
| 7 | goto-line 跳到远处 | 跳转后视口定位（fallback），下一帧自纠正 |
| 8 | 鼠标滚轮滚动 | 与现状一致（不受影响）|

## 12. 已知限制

- **跨段进入 MD 段的首次按键**走 fallback（1:1 近似），下一帧自纠正。这是光标段"原生渲染特权"带来的固有滞后，非本方案引入。多数用户操作（段内连续移动）不受影响。
- MD 段的 segmentRow 按 MD 渲染器 softwrap 计数，不参与精确判定（仅光标段 native 路径参与）。

## 13. 配套修复：table header 分隔线 BufLine

### 13.1 问题

表格里有两类分隔线，语义不同但 `makeTableSeparator` 把它们都标成 -1：

| 分隔线 | 位置（`render_table.go`）| 对应 buffer 行 | 当前 `row.BufLine` | 应该 |
|---|---|---|---|---|
| **header 分隔线** `\|---\|` | `:829`（header 行之后）| `pt.sepIdx`（源码就是 `\|---\|` 那一行）| -1 | 真实号 |
| **行间分隔线** | `:839`（body 行之间）| 无（纯渲染装饰）| -1 | -1（不变）|

header 分隔线违反 §5 "确实没有对应 buffer 行的才是 -1" 原则——它实际上就是 `\|---\|` 这一行，`parseTable` 用 `pt.sepIdx` 识别它。

### 13.2 修复

给 `makeTableSeparator` 加 `bufLine` 参数，由调用方传语义：

```go
func makeTableSeparator(colWidths []int, width int, style, spaceStyle tcell.Style, bufLine int) RenderedRow {
    row := RenderedRow{
        BufLine: bufLine,   // 原为 -1 硬编码
        Cells:   make([]Cell, 0, width),
    }
    // ... cell 内部 BufLine 仍保持 -1（cell 级别是装饰，行级别由 bufLine 决定）
}
```

两个调用点：

```go
// :829  header 分隔线——对应 buffer 行 pt.sepIdx
result.Rows = append(result.Rows,
    makeTableSeparator(colWidths, width, borderStyle, contentStyle, pt.sepIdx))

// :839  行间分隔线——纯装饰，无对应 buffer 行
if bodyIdx < len(pt.body)-1 {
    result.Rows = append(result.Rows,
        makeTableSeparator(colWidths, width, borderStyle, contentStyle, -1))
}
```

> cell 内部的 `BufLine: -1` **保持不变**——cell 是 `─`/`├`/`┼`/`┤` 装饰字符，本就不对应具体字符位置。只有 **row 级别的 `BufLine`** 决定该屏行归属哪个 buffer 行。

### 13.3 不影响 C4 主流程

此修复是"渲染器语义正确性"问题，与 C4 主流程（`viewportRowmap` / 滚动判定）独立：

- `renderSegmentMD` 信任 `row.BufLine`，修不修都能正确存（修后存的是 `pt.sepIdx`，修前存的是 -1）。
- `lineToScreenRow` 只精确匹配光标段（native 渲染、无任何 -1）。表格走 MD 段，其 `row.BufLine` 是真实号还是 -1 只影响"Line<0 就跳过找下一内容行"，两种取值都安全。

列为 C4 配套修复，是因为它与 §5 的装饰行定义一致化原则同源——保证 `viewportRowmap` 反映的语义与实际 buffer 结构一致。
