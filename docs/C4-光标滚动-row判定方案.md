# C4：光标滚动 — viewportRowmap 二维表方案

> 关联：根因见 `docs/C0-光标滚动-现状调研.md`。

## 1. 问题与目标

**问题**：MD 渲染文件中，光标停在屏幕底边按 ↓ 不触发上滚、光标飞出视口底部。

**根因**：原生 `Relocate` 假设"buffer 行 = 屏幕行"的 1:1 关系，MD 装饰和 softwrap 续行打破了这个假设。

**范围**：仅处理"按上下键导致滚动"的场景。鼠标滚轮滚动当前正常，不在范围内。

**目标**：用 `(bufferLine, segmentRow)` 二维表精确记录每个屏行的视觉位置，让滚动判定和动作都基于精确查表，消除累积偏差。

## 2. 关键前提（上下键场景恒成立）

- 方向键是非 ESC 键 → `editMode = true`（`bufpane_md.go:72`）
- → 光标所在段走 `renderSegmentNative`（`bufwindow_md.go:728`）
- → 光标行无装饰，原生 1:1 渲染

这保证光标周围的屏行在 `viewportRowmap` 里都是精确的真实数据。

## 3. 方案设计

### 3.1 数据结构：`viewportRowmap []SLoc`

替换原一维字段 `viewportRowBufLine []int`，新增二维 `viewportRowmap []SLoc`。

> `SLoc` 是 micro 原生类型（`softwrap.go:13`，`{Line, Row}`），表示"buffer 第 Line 行的第 Row 个 softwrap 段"。`StartLine` 本身就是 `SLoc`，复用它零转换。

```go
// viewportRowmap[i] = viewport 第 i 个屏幕行对应的视觉行位置
// Line = -1：装饰行（标题下划线、表格 frame 等）
// Line = -2：空白填充区域（buffer 内容不够填满 viewport）
// Line >= 0：内容行；Row = 该屏行是此 buffer 行的第几个 softwrap 段（0-based）
viewportRowmap []SLoc
```

示例（长行 + 装饰的视口）：

| viewport row | Line | Row | 含义 |
|---|---|---|---|
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

光标下移后 `c = SLocFromLoc(activeC.Loc) = {c.Line, c.Row}`。在 `viewportRowmap` 里精确匹配 `(Line, Row)` 二元组，得到光标屏行 `cursorRow`：

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

精确性来源：光标段原生渲染，其 segmentRow 用 micro softwrap 算法记录；`c.Row` 也用 micro softwrap 算法（`SLocFromLoc`）。两者一致 → 二元组精确命中，不会命中装饰行（-1 ≠ c.Line），不会命中同行的其他 softwrap 段（Row 不同）。

```go
botMarginRow := height - 1 - scrollmargin
if cursorRow > botMarginRow { /* 向下滚 */ }
if cursorRow < scrollmargin    { /* 向上滚 */ }
```

### 3.3 动作端：怎么滚

向下滚：视口上移 `delta = cursorRow - botMarginRow` 行。当前屏行 `delta` 成为新 row 0：

```go
delta := cursorRow - botMarginRow
w.StartLine = w.viewportRowmap[delta]   // 直接读 (Line, Row)，含 segmentRow
```

| `viewportRowmap[delta]` 的值 | 场景 | 新 StartLine |
|---|---|---|
| `{11, 0}` | delta 落在下一行行首 | `{11, 0}` |
| `{10, 3}` | delta 落在长行续段 | `{10, 3}`（旧方案在此丢精度，填 `{10,0}`）|
| `{-1, -1}` | delta 落在装饰行 | 向下找首个 `Line>=0` 的条目（见 §5.5）|

向上滚：沿用原生 `w.StartLine = w.Scroll(c, -scrollmargin)`。`StartLine` 离光标仅 scrollmargin（默认 3）行，几乎总在光标段内（原生渲染、无装饰）→ 1:1 精确。

## 4. 装饰行 BufLine 语义参考表

判断标准：**确实没有对应 buffer 行的，才是 -1 装饰行。** 渲染器的 `row.BufLine` 是唯一真相源，`renderSegmentMD` 直接存 `row.BufLine`，不做覆盖。

> **坐标系说明**：MD 渲染器（`RenderTable`/`RenderCodeblock` 等）内部产出的是 **segment 内相对行号**（基于 `linesFromBuf(BufStartLine, BufEndLine)` 的切片索引）。但 `renderSegmentMD:76-84` 在拿到 rendered 后会统一**绝对化**：`row.BufLine >= 0 的 += seg.BufStartLine`。所以下表中的"真实行号"指的是**进入 viewportRowmap 时的绝对 buffer 行号**；装饰行（-1）不加，保持 -1。

实测分类（逐行验证过渲染器代码）：

| 元素 | `row.BufLine` | 依据 |
|---|---|---|
| codeblock 顶边框 `┌────python` | 真实（0 = ```python 围栏）| `render_codeblock.go:173` |
| codeblock 代码行 | 真实 | `:199/209` |
| codeblock 底边框 `└────` | 真实（len-1 = ``` 闭合围栏）| `:254` |
| 标题文本行 | 真实 | `wrapCells(..., lineIdx)` |
| 标题下划线 `---` | -1 | `makeDecoRow` |
| hr 分隔线 `---` | 真实（0）| `render_hr.go:10` |
| 表格顶边框 | -1 | `makeTableTopBorder:580` |
| 表格 header 行 | 真实（sepIdx-1）| `render_table.go:821` |
| 表格 header 分隔线 `\|---\|` | -1（需修复 → §6）| `makeTableSeparator` |
| 表格 body 行 | 真实 | `:833` |
| 表格行间分隔线 | -1 | `makeTableSeparator:839` |
| 表格底边框 | -1 | `makeTableBottomBorder:645` |

## 5. 实现代码

### 5.1 数据结构（`bufwindow.go`）

删除 `viewportRowBufLine []int`，新增 `viewportRowmap []SLoc`。不并存——`.Line` 已包含原数组的全部信息，保留两个数组是冗余且易失同步。

### 5.2 `renderSegmentNative`（`bufwindow_md.go`，光标段）

- 返回签名从 `(newVY, rowBufLines []int)` 改为只返回 `newVY`
- 渲染时直接写 `w.viewportRowmap[screenRow]`

**循环结构**：外层 `for ...; vloc.Y++`（`:317`）每次处理**一个 buffer 行**（bloc.Y），softwrap 的换屏在内层 `wrap()` 函数（`:592` 的 `vloc.Y++`）里发生。因此 `:672-677` 的 `for screenRow := lineStartVY; screenRow <= vloc.Y` 在**每个 buffer 行末尾执行一次**，一次性写入该行所有 softwrap 屏行。

**segmentRow 公式**：`vloc` 是 `buffer.Loc`（无 SLoc.Row），但 segmentRow 可直接由屏行偏移推算。该 buffer 行（`bloc.Y`）从 `lineStartVY` 开始画，占 `lineStartVY..vloc.Y` 连续屏行：

```go
// 替换原 :672-677 的 for screenRow := lineStartVY... append 块
for screenRow := lineStartVY; screenRow <= vloc.Y; screenRow++ {
	if screenRow >= 0 && screenRow < bufHeight {
		w.viewportRowmap[screenRow] = SLoc{bloc.Y, screenRow - lineStartVY}
	}
}
```

**为什么不需要 rowOffset 分支**：当 `seg.VisibleStart == w.StartLine.Line` 时，`:304` 会执行 `vloc.Y -= w.StartLine.Row`，让该行的 `lineStartVY` 变成负值（`-StartLine.Row`）。于是 `screenRow - lineStartVY` 天然从 `StartLine.Row` 开始——前 `StartLine.Row` 段的负 screenRow 被守卫过滤掉（画在视口外），可见部分自动得到正确的 segmentRow。

验证（StartLine={16,3}，line16 softwrap 6 段）：`:304` 偏移后 lineStartVY=-3，可见 screenRow=0,1,2 → segmentRow = 0-(-3), 1-(-3), 2-(-3) = 3,4,5 ✓。后续行（line17）lineStartVY>=0，segmentRow 从 0 开始 ✓。

> `screenRow >= 0` 守卫的必要性：首行 `vloc.Y -= w.StartLine.Row` 偏移让 lineStartVY 可能为负，负屏行画在视口外、不该进 viewportRowmap（保持初始 -2）。

### 5.3 `renderSegmentMD`（`bufwindow_md.go`，非光标段）

- 返回签名从 `(newVY, rowBufLines []int)` 改为只返回 `newVY`
- 渲染时直接写 `w.viewportRowmap[vY]`，用 `row.BufLine`（非 effectiveLine）

```go
// 替换原 :134 的 rowBufLines = append(rowBufLines, effectiveLine)
// segRow 在循环外初始化为 0
if row.BufLine < 0 {
	segRow = -1                            // 装饰行
} else if row.BufLine == lastBufLine {
	segRow++                               // 同一 buffer 行的续段
} else {
	segRow = 0                             // 新 buffer 行
}
if vY >= 0 && vY < bufHeight {
	w.viewportRowmap[vY] = SLoc{row.BufLine, segRow}
}
lastBufLine = row.BufLine
```

> effectiveLine 逻辑（`:117-122`）仅保留用于可见性判断（`:124`），不影响存储。

### 5.4 `displayBufferMD`（`bufwindow_md.go`）

删除 `rowBufLines` 中转变量与 copy 逻辑，主循环简化为：

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

> 原 Bug2（copy 越界）的根源被清除：没有 copy、没有 `startVY` 反算，渲染函数内部的 `screenRow < bufHeight` / `vY < bufHeight` 守护确保不越界。

### 5.5 `relocateVerticalMD`（`bufwindow_md.go`，重写）

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
		w.StartLine = loc
		return true
	}
	if cursorRow < scrollmargin {
		w.StartLine = w.Scroll(c, -scrollmargin) // 向上：段内 1:1
		return true
	}
	return true
}
```

> **delta 落装饰行的精度说明**：当 delta 行是装饰/空白，代码向下找到首个内容行作为新视口顶，新视口实际起始位置比"理论 delta"靠后 N 行（N = 跳过的装饰行数）。结果是光标落在 `botMarginRow` 上方而非精确贴合 margin。这**不是 bug**——光标仍在 margin 内、不会飞出视口（核心目标达成），只是未精确贴边。该场景仅在 delta 跨入相邻 MD 段的装饰行时发生，属边界情况。

### 5.6 辅助函数：互逆对

两个函数，互为逆操作，命名对称：

| 函数 | 签名 | 方向 | 用途 |
|---|---|---|---|
| `screenRowToLine` | `(screenRow int) (int, bool)` | 屏行 → 行号 | 点击映射（`bufwindow.go:300`）|
| `lineToScreenRow` | `(line, row int) (int, bool)` | (行号, 段) → 屏行 | Relocate 滚动判定 |

- **`screenRowToLine`**：由现有 `screenOffsetToBufferLine`（`:837`）改名，读 `w.viewportRowmap[screenRow].Line`，丢 Row（点击只需行号）。返回 `(0, false)` 当该行是装饰/空白——此时调用方 `LocFromVisual` fallback 到 `w.Scroll` 的默认行为（点击装饰行不跳转，保持光标不动）。
- **`lineToScreenRow`**：新增，见 §3.2。线性扫描 O(bufHeight)，bufHeight 通常 ≤ 终端高度（约 50–100 行），无需哈希表——这是有意的设计选择。
- **删除** `bufferLineToScreenOffset`（`:851`，旧倒序版）。它唯一调用方是旧 `relocateVerticalMD`，重写后无人调用。Go 不支持函数重载，新方案的精确二元组匹配取代了旧的"取最后匹配"近似。
- **删除** `dumpScroll`（临时调试日志）及其 `fmt`/`os` import。

### 5.7 `Relocate` 分发（`bufwindow.go`，不变）

```go
if w.Buf.IsMD && w.mdConfig.MDRender {
	ret = w.relocateVerticalMD(c, scrollmargin, height)
} else {
	// micro 原生垂直 Relocate（原样，仅缩进）
}
```

## 6. 配套修复：table header 分隔线 BufLine（`internal/md/render_table.go`）

表格里有两类分隔线，语义不同但 `makeTableSeparator` 都标成 -1：

| 分隔线 | 位置 | 对应 buffer 行 | 当前 `row.BufLine` | 应该 |
|---|---|---|---|---|
| header 分隔线 `\|---\|` | `:829`（header 行之后）| `pt.sepIdx` | -1 | 真实号 |
| 行间分隔线 | `:839`（body 行之间）| 无（纯装饰）| -1 | -1 |

给 `makeTableSeparator` 加 `bufLine` 参数，由调用方传语义：

```go
func makeTableSeparator(colWidths []int, width int, style, spaceStyle tcell.Style, bufLine int) RenderedRow {
    row := RenderedRow{
        BufLine: bufLine,   // 原为 -1 硬编码
        Cells:   make([]Cell, 0, width),
    }
    // cell 内部 BufLine 仍保持 -1（cell 级别是装饰字符）
}
```

调用点：

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

> cell 内部 `BufLine: -1` 保持不变——cell 是 `─`/`├`/`┼`/`┤` 装饰字符。只有 row 级别的 `BufLine` 决定该屏行归属哪个 buffer 行。

## 7. 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/display/bufwindow.go` | 删除 `viewportRowBufLine []int`；新增 `viewportRowmap []SLoc`；click 映射读 `.Line` |
| `internal/display/bufwindow_md.go` | 两个 render 函数只返回 `newVY` + 直接写 `viewportRowmap`；`displayBufferMD` 删除 `rowBufLines`/copy 逻辑；重写 `relocateVerticalMD`；新增 `lineToScreenRow`；`screenOffsetToBufferLine` 改名 `screenRowToLine`；删除 `bufferLineToScreenOffset`、`dumpScroll` |
| `internal/md/render_table.go` | header 分隔线 `BufLine` 从 -1 改为 `pt.sepIdx`（§6）|

未涉及：`softwrap.go`、`actions.go`、`Relocate` 非 MD 分支、其他 MD 渲染器。

## 8. 实施步骤

1. `bufwindow.go`：字段改名 + click 映射适配
2. `bufwindow_md.go`：两个 render 函数删 `rowBufLines` 返回值，改为直接写 `w.viewportRowmap` + segmentRow 计算
3. `bufwindow_md.go`：`displayBufferMD` 删 `rowBufLines` 局部变量、copy、startVY 逻辑
4. `bufwindow_md.go`：重写 `relocateVerticalMD`；新增 `lineToScreenRow`；`screenOffsetToBufferLine` 改名 `screenRowToLine`；删除 `bufferLineToScreenOffset`、`dumpScroll`、`fmt`/`os` import
5. `render_table.go`：修复 header 分隔线 `BufLine`（§6）
6. `make build-quick` 编译
7. 跑 §10 测试用例

## 9. 正确性论证

**非 MD 零影响**：非 MD 走 else 分支（micro 原生），`viewportRowmap` 不被读写。

**同段移动完全精确（核心场景）**：光标段原生渲染 → `viewportRowmap` 里光标行的 `(Line, Row)` 用 micro softwrap 记录 → 与 `c.Row`（同为 micro softwrap）一致 → `lineToScreenRow` 精确命中 → 判定准；动作读 `viewportRowmap[delta]` → StartLine 精确（含 segmentRow）。无累积偏差。

**装饰行不干扰**：装饰行存 `{Line:-1}` → `lineToScreenRow` 不匹配（-1 ≠ c.Line）；动作端 delta 落装饰行时向下找首个内容行（§5.5）。

## 10. 测试用例

| # | 操作 | 预期 |
|---|---|---|
| 1 | `sample.md` 从 line1 连续按 ↓ 到底 | 光标始终停在底边 margin，不飞出，无累积偏差 |
| 2 | 经过超长段落（line68 区域）按 ↓ 触发滚动 | 光标精确停在 margin |
| 3 | 经过表格区（line54-58）按 ↓ 触发滚动 | 光标精确停在 margin |
| 4 | 到底后连续按 ↑ | 光标不飞出顶部 |
| 5 | 非 MD 文件同样操作 | 与原版完全一致 |
| 6 | `./microneo docs/sample.md` 启动 | 不崩溃（首帧 fallback）|
| 7 | goto-line 跳到远处 | 跳转后视口定位（fallback），下一帧自纠正 |
| 8 | 鼠标滚轮滚动 | 与现状一致（不受影响）|
| 9 | 视口顶不是 segment 起始（`StartLine={16,3}`）后按 ↓ | segmentRow 从 StartLine.Row 开始正确计数，光标精确停在 margin（B1 回归场景）|

## 11. 边界场景与兜底

| 场景 | 处理 |
|---|---|
| 首帧（`viewportRowmap` 未构建）| `n==0` → fallback |
| 光标跳出视口（goto-line/搜索）| `lineToScreenRow` miss → fallback，下一帧自纠正 |
| 光标跨段进入 MD 段 | MD 段 segmentRow 按 `wrapCells` 计数，可能与 `c.Row`（micro softwrap）不一致 → miss → fallback，下一帧光标段变 native，精确 |
| delta 落空白尾部 | 向下找不到内容行 → fallback |

`relocateVerticalNativeFallback`（复刻 micro 原生垂直 Relocate）保留，与旧 C4 相同。

## 12. 已知限制

- **跨段进入 MD 段的首次按键**走 fallback（1:1 近似），下一帧自纠正。这是光标段"原生渲染特权"带来的固有滞后。多数用户操作（段内连续移动）不受影响。
- MD 段的 segmentRow 按 MD 渲染器 softwrap 计数，不参与精确判定（仅光标段 native 路径参与）。
