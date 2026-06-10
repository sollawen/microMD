# F1: 三分屏 Split 布局修复方案

> 修复 D1.1 实施后 Alt-i 打开三分屏时分隔线位置错误的问题。

---

## 一、问题现象

在 line 4 按 Alt-i，预期分隔线出现在 line 4 下方，实际出现在屏幕 ~50% 处（line 20 左右）。两个分隔线之间无 input 空间，且 topPane 和 bottomPane 显示相同内容。

## 二、根因

micro 的 split 是**严格二叉树**。两次 `HSplit(bottom=true)` 产生的树结构是**嵌套的**，不是三个平级兄弟：

```
Root (STVert)
├── topPane (buffer B) ← 第1次HSplit分配，占50%
└── innerNode (STVert) ← 第1次HSplit分配，占50%
    ├── inputPane (inputBuf) ← 第2次HSplit分配，占inner的50%=总25%
    └── bottomPane (buffer B) ← 第2次HSplit分配，占inner的50%=总25%
```

三个问题：

1. **topPane 占 50%**：第一次 HSplit 默认 50/50，与光标行号无关
2. **inputPane 只能在 innerNode 内部调大小**：`ResizeSplit` 只在相邻兄弟间借空间，无法影响 topPane
3. **topPane 和 bottomPane 共享 buffer 且视口未独立设置**：两个 pane 显示同一 buffer 的同一区域

## 三、修复策略

**核心思路**：两次 HSplit 构造嵌套二叉树后，用两步 `ResizeSplit` 从外到内精确分配高度。

### 高度分配公式

```
topHeight    = curLine + 1 + gutterOffset              // 光标行 + 1（显示到光标行为止）
inputHeight  = 1                                       // 初始 1 行
bottomHeight = totalHeight - topHeight - inputHeight   // 剩余空间
```

其中 `gutterOffset` 来自 `topPane.GetView().X - topPane.GetView().Y`（即窗口 X 坐标的偏移，对应 split 分隔线占的 1 列）。

### 第一步：ResizeSplit topPane → 把 topPane 从 50% 缩到 `topHeight`

```
ResizeSplit 调用在 topPane 的节点上
topPane 节点的 parent 是 Root(STVert)
→ Root 的两个子节点 [topPane, innerNode] 被重新分配
→ topPane 拿到 topHeight，innerNode 拿到 totalHeight - topHeight
```

调用 `tab.Resize()` 后，innerNode 内部按比例重分配 → inputPane 和 bottomPane 各拿到 `(totalHeight-topHeight)/2`。

### 第二步：ResizeSplit inputPane → 把 inputPane 从 innerNode 的 50% 缩到 1 行

```
ResizeSplit 调用在 inputPane 的节点上
inputPane 节点的 parent 是 innerNode(STVert)
→ innerNode 的两个子节点 [inputPane, bottomPane] 被重新分配
→ inputPane 拿到 1 行，bottomPane 拿到剩余
```

调用 `tab.Resize()` 后完成。

### 第三步：设置各 pane 视口

- **topPane**：`StartLine = {Line: 0, Row: 0}`（从头开始，到 curLine 正好填满 topHeight）
- **bottomPane**：`StartLine = {Line: curLine+1, Row: 0}`（从光标下一行开始）
- **inputPane**：新空 buffer，无需设置

## 四、具体改动

### 4.1 `inlineinput.go` — `open()` 重写步骤 4-6

将原来的：

```go
// ---- 4. 设置焦点锁 ----
ii.active = true
InlineInputInstance = ii

// ---- 5. re-stamp 下半 pane 视口 ----
ii.reStamp()

// ---- 6. 设定初始 input 高度为 1 ----
ii.adjustInputHeight()
```

替换为：

```go
// ---- 4. 调整三分屏高度 ----
ii.layoutPanes()

// ---- 5. 设置焦点锁 ----
ii.active = true
InlineInputInstance = ii
```

### 4.2 `inlineinput.go` — 新增 `layoutPanes()` 方法

```go
// layoutPanes 在两次 HSplit 之后精确分配三层高度。
// 必须在焦点锁生效之前调用（因为内部会触发 SetActive）。
func (ii *InlineInput) layoutPanes() {
	tab := ii.inputPane.tab
	totalHeight := ii.origTotalHeight

	// 计算各层高度
	topHeight := ii.curLine + 1
	inputHeight := 1
	if topHeight+inputHeight > totalHeight-maxBottomReserve {
		// 空间不够，压缩 topHeight
		topHeight = totalHeight - maxBottomReserve - inputHeight
		if topHeight < 1 {
			topHeight = 1
		}
	}

	// 第一步：Resize topPane → 把外层 50/50 改为 topHeight / (rest)
	topNode := tab.GetNode(ii.topPane.splitID)
	if topNode != nil {
		topNode.ResizeSplit(topHeight)
		tab.Resize()
	}

	// 第二步：Resize inputPane → 在 innerNode 内把 50/50 改为 inputHeight / (rest)
	if ii.bottomPane != nil {
		inputNode := tab.GetNode(ii.inputPane.splitID)
		if inputNode != nil {
			inputNode.ResizeSplit(inputHeight)
			tab.Resize()
		}
	}

	// 第三步：设置视口
	// topPane 从头开始显示
	tv := ii.topPane.GetView()
	tv.StartLine = display.SLoc{Line: 0, Row: 0}
	ii.topPane.SetView(tv)

	// bottomPane 从 curLine+1 开始显示
	ii.reStamp()
}
```

### 4.3 `inlineinput.go` — 修改 `adjustInputHeight()`

当前的 `adjustInputHeight()` 用 `ResizeSplit` 调整 input 高度，在嵌套结构下它只在 inputPane 和 bottomPane 之间重新分配，这正是我们需要的（不动 topPane）。但需要改用 `origTotalHeight` 减去 topPane 当前实际高度来计算 inputPane 可用的最大值。

修改计算 `effectiveMax` 的逻辑：

```go
func (ii *InlineInput) adjustInputHeight() {
	if !ii.active || ii.inputPane == nil {
		return
	}

	linesNum := ii.inputPane.Buf.LinesNum()

	// topPane 的实际高度
	topNode := ii.inputPane.tab.GetNode(ii.topPane.splitID)
	if topNode == nil {
		return
	}
	topHeight := topNode.H

	// inputPane + bottomPane 的总可用空间
	availForInput := ii.origTotalHeight - topHeight - maxBottomReserve
	if availForInput < 1 {
		availForInput = 1
	}

	effectiveMax := ii.maxLines
	if availForInput < effectiveMax {
		effectiveMax = availForInput
	}

	desired := linesNum
	if desired < 1 {
		desired = 1
	}
	if desired > effectiveMax {
		desired = effectiveMax
	}

	inputNode := ii.inputPane.tab.GetNode(ii.inputPane.splitID)
	if inputNode != nil {
		currentHeight := inputNode.H
		if desired != currentHeight {
			inputNode.ResizeSplit(desired)
			ii.inputPane.tab.Resize()
			ii.reStamp()
		}
	}
}
```

## 五、改动范围

| 文件 | 改动 |
|------|------|
| `internal/action/inlineinput.go` | `open()` 重写步骤 4-6；新增 `layoutPanes()`；重写 `adjustInputHeight()` |

**零改动原生文件**（`tab.go`、`bufpane.go` 等不需要动）。

## 六、验证

| # | 操作 | 预期 |
|---|------|------|
| 1 | 打开 >20 行文件，光标在 line 4，Alt-i | topPane 显示 line 0-3（4行），input 1行，bottomPane 显示 line 5 起 |
| 2 | Alt-i 关闭 | 恢复单窗口，原文件不变 |
| 3 | 光标在 line 0，Alt-i | topPane 只有 line 0（1行） |
| 4 | 光标在末行，Alt-i | 二分屏（无 bottomPane） |
| 5 | input 中连续回车 | input 区自动增长，到上限后内部滚动 |
| 6 | 终端 resize | 三 pane 重排，topPane 高度不变，bottomPane 至少 1 行 |
