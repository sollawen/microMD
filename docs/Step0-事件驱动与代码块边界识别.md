# Step 1：事件驱动 detect + 代码块边界识别 + 前景色来源

> 目的：把当前 Step 0 之后的"三大决策"独立记录
>
> 性质：**已达成共识的设计文档**。三件事都已决定
>
> **不混着思考**——这是本文档最关键的纪律

---

## 总览：三大独立决策

| # | 决策主题 | 状态 | 关键问题 |
|---|---------|------|---------|
| 1 | detect 触发时机 | ✅ **已决定** | detect 什么时候跑？ |
| 2 | 代码块边界识别 | ✅ **已决定** | detect 怎么知道哪些行是代码块？ |
| 3 | 字符前景色来源 | ✅ **已决定** | 字符前景色用谁的？ |

---

## 决策 1：detect 触发时机 ✅ 已决定

### 1.1 决策内容

**detect 永远跟在 micro highlighter 后面跑**。

| 时点 | micro highlighter | MicroNeo detect |
|------|-------------------|-----------------|
| Buffer 打开 | async goroutine：`HighlightStates(全文)` + `HighlightMatches(0, End.Y)` | async：**等 highlighter 完成后**跑 `DetectSegments(全文)` |
| Buffer 编辑 | sync：`ReHighlightStates(start)` + `HighlightMatches(start, l)` | sync：**紧接着**跑 `DetectSegments(start, l)` |
| 屏幕重绘 | 不算 | 不算 |
| 用户不动 | 不算 | 不算 |

### 1.2 决策理由

- 跟 micro 的 event-driven 机制**协调**——不引入新的触发器
- **不造成冲突**——不会出现"detect 在算时 highlighter 又被改"的情况
- **时序固定**——detect 总在 highlighter 之后，所以**总能从 `b.Match(L)` 读到最新结果**

### 1.3 实现位置

**打开时**（`internal/buffer/buffer.go:1006` `SetSyntaxDef`）：
```go
if b.SyntaxDef != nil {
    b.Highlighter = highlight.NewHighlighter(b.SyntaxDef)
    if b.Settings["syntax"].(bool) {
        go func() {
            b.Highlighter.HighlightStates(b)
            b.Highlighter.HighlightMatches(b, 0, b.End().Y)
            md.DetectSegments(b, 0, b.End().Y)  // ← 新增：等 highlighter 完成后跑
            screen.Redraw()
        }()
    }
}
```

**编辑时**（`internal/buffer/buffer.go:185` `MarkModified`）：
```go
func (b *SharedBuffer) MarkModified(start, end int) {
    ...
    l := -1
    for i := start; i <= end; i++ {
        l = util.Max(b.Highlighter.ReHighlightStates(b, i), l)
    }
    b.Highlighter.HighlightMatches(b, start, l)
    md.DetectSegments(b, start, l)  // ← 新增：紧接 highlighter 增量结果
    ...
}
```

### 1.4 与之前 2.6 节的关系

2.6 节"事件驱动 detect 提案"**就是这个决策**的更详细讨论。本节是**精简版**——明确了实现位置和调用顺序。

---

## 决策 2：代码块边界识别 ✅ 已决定

### 2.1 决策内容

**简化原则**：
- **代码块的起止边界** → **使用 highlighter 产出物**（`b.State(lineY)` 数组）
- **其他 render 的判断** → **detector 自己做字符匹配**

| Render | 识别机制 | 数据来源 |
|--------|---------|---------|
| **Codeblock** | ✅ highlighter 产出物 | `b.State(lineY) != nil` |
| **Blockquote / List / Table / Heading / HR / Paragraph** | ✅ detect 字符串匹配 | 当前 `detect.go` 的逻辑 |

**不混着用**——codeblock **只**用 highlighter 产出物，其他 6 种 **只**用 detect 字符串匹配。

### 2.2 决策理由

- **用户原则**："highlighter 产出物能帮上忙的，就拿来用；帮不上忙的，就自己来"
- **state 数组对 codeblock 完美**：highlighter 依据用户 yaml 里的 region 规则（` ``` ` start/end）扫 buffer，产出 state 数组——state 数组的 nil↔region 转折点**隐式**就是代码块边界
- **其他 6 种 render**：用户的 yaml 里是 pattern 不是 region，state 数组没用；match map 在不同 yaml 下 group 名字不一致（`>` 行在用户 yaml 是 preproc、内置 yaml 是 statement）——不通用；**字符串匹配跨配置稳定**
- **不再纠结 match map 反推 render 类型**——避免"配置差异"问题

### 2.3 严格代码追踪

**用户 `~/.config/micro/syntax/markdown.yaml` 里的 region 规则**：

```yaml
- default:
    start: "^(\`{3,}|~{3,})\\s*python"   # 进了这个 region
    end:   "^(\`{3,}|~{3,})"
    rules:
        - include: "python"
```

**highlighter 扫 buffer 产出 state 数组**：

```
L5  "```python"        b.State(L5) = &PythonRegion   ← 进入
L6  "def foo():"       b.State(L6) = &PythonRegion
L7  "    return 1"     b.State(L7) = &PythonRegion
L8  "```"              b.State(L8) = nil             ← 退出

→ state 数组的转折点（nil → &PythonRegion → nil）**隐式**就是代码块边界
→ L5..L7 是代码块内部，L5 是进入行，L8 是退出行
```

**detect 只需要扫 state 数组的转折点**——零字符匹配。

**其他 6 种 render**（用户的 yaml 里都是 pattern）：
```
L10 "# Heading"        b.State(L10) = nil  → 字符串匹配 → HeadingRender
L12 "> quote"          b.State(L12) = nil  → 字符串匹配 → BlockquoteRender
L14 "- item"           b.State(L14) = nil  → 字符串匹配 → ListRender
L16 "| col | col |"    b.State(L16) = nil  → 字符串匹配 → TableRender
L18 "---"              b.State(L18) = nil  → 字符串匹配 → HRRender
L20 "普通段落"          b.State(L20) = nil  → 字符串匹配 → ParagraphRender
```

### 2.4 detect.go 改造示意

**当前 `detect.go`** 有 5 个 `detectState`（stateNormal / stateCodeblock / stateBlockquote / stateTable / stateList）和 5 个 case 分支。

**改造后**：

```go
// detect.go 顶部 import：
//   "github.com/micro-editor/micro/v2/pkg/highlight"

func DetectSegments(buf BufferReader, startY, endY int) []Segment {
    segments := []Segment{}
    var codeblockStart int = -1
    var lastState highlight.State  // 上一行的 highlighter state
    
    // ── C2 修复：startY == 0 时不能查 State(-1)，会 panic ──
    if startY > 0 {
        lastState = buf.State(startY - 1)
    }
    
    for y := startY; y <= endY; y++ {
        curState := buf.State(y)
        
        // ── Codeblock 边界：用 highlighter state 数组 ──
        if lastState == nil && curState != nil {
            codeblockStart = y  // 进入 codeblock
        }
        if lastState != nil && curState == nil {
            segments = append(segments, Segment{
                BufStartLine: codeblockStart,
                BufEndLine:   y - 1,  // 不含当前行（当前行 state=nil，退出后归下一段）
                Render:       RenderCodeBlock,
            })
            codeblockStart = -1
        }
        
        // ── 非 codeblock 行：保持当前字符串匹配 ──
        if curState == nil {
            line := buf.Line(y)
            trimmed := strings.TrimSpace(line)
            
            if strings.HasPrefix(trimmed, ">") {
                segments = append(segments, Segment{
                    BufStartLine: y, BufEndLine: y,
                    Render: RenderBlockquote,
                })
            } else if isListItem(trimmed) {
                segments = append(segments, Segment{...Render: RenderList})
            } else if strings.Contains(trimmed, "|") && trimmed != "" {
                segments = append(segments, Segment{...Render: RenderTable})
            } else if strings.HasPrefix(trimmed, "#") {
                segments = append(segments, Segment{...Render: RenderHeading})
            } else if isHR(trimmed) {
                segments = append(segments, Segment{...Render: RenderHR})
            } else {
                segments = append(segments, Segment{...Render: RenderParagraph})
            }
        }
        // codeblock 内的行（curState != nil）不产生独立 segment——归入 codeblock segment
        
        lastState = curState
    }
    
    return segments
}
```

**关键变化**：
1. 删掉 `stateCodeblock` 状态机和对应 case 分支
2. 删掉 ` ``` ` / `~~~` 字符串匹配
3. 改用 `b.State(lineY) != nil` 判断 codeblock
4. 保留 6 种 render 的字符串匹配（isBlockquote / isListItem / isTable / isHeading / isHR / Paragraph）

### 2.5 BufferReader 接口扩展

**当前接口**（`internal/md/detect.go:7`）：
```go
type BufferReader interface {
    LinesNum() int
    LineBytes(n int) []byte
    Line(n int) string
}
```

**扩展后**：
```go
// detect.go 顶部 import：
//   "github.com/micro-editor/micro/v2/pkg/highlight"

type BufferReader interface {
    LinesNum() int
    LineBytes(n int) []byte
    Line(n int) string
    
    // 新增：highlighter state 数组
    // 返回类型: highlight.State
    //   = pkg/highlight/highlighter.go:55 的 type State = *region
    //   = *buffer.Buffer.State(n int) 的返回类型
    // 注意：*region 是 pkg/highlight/parser.go 里的未导出类型，
    //      接口里只能用其 type alias highlight.State，否则编译失败。
    State(n int) highlight.State
}
```

**`*buffer.Buffer.State(n int) highlight.State` 已存在**（`internal/buffer/line_array.go:334`）——只需在接口里加一行。

### 2.6 一个边界情况：buffer 末尾未闭合的 codeblock

用户编辑时 ` ``` ` 围栏没打完（buffer 中间或末尾都可能）：

```
L5  "```python"     state = &PythonRegion
L6  "def foo():"    state = &PythonRegion
L7                  state = &PythonRegion  ← buffer 结尾，未闭合
```

**detect 末尾处理**：循环结束后**单独检查** `codeblockStart != -1` → 强制闭合到最后一行

```go
// 循环结束后
if codeblockStart != -1 {
    segments = append(segments, Segment{
        BufStartLine: codeblockStart,
        BufEndLine:   endY,
        Render:       RenderCodeBlock,
    })
}
```

**不采用**"循环到 `y == endY + 1` 时强制 `curState = nil` 触发退出"——会引入越界读 `buf.State(endY+1)`，更危险。

### 2.7 测试 mock 改造

**当前 mock**（`internal/md/detect_test.go`）：
```go
type mockBuffer struct {
    lines []string
}
func (m *mockBuffer) LinesNum() int { return len(m.lines) }
func (m *mockBuffer) LineBytes(n int) []byte { return []byte(m.lines[n]) }
func (m *mockBuffer) Line(n int) string { return m.lines[n] }
// ← 没有 State(n int) highlight.State
```

**`BufferReader` 加 `State` 方法后**，`mockBuffer` 不满足接口——所有 22 个 `DetectSegments` 测试**编译失败**。

**修复**：给 mockBuffer 加一个默认返回 nil 的 `State` 方法：

```go
// internal/md/detect_test.go
import "github.com/micro-editor/micro/v2/pkg/highlight"

func (m *mockBuffer) State(n int) highlight.State { return nil }
```

**mock 返回 nil 等价于"内置 yaml 行为"**（state 全是 nil）——这意味着 mock 测试**只覆盖字符串匹配的 6 种 render**，不覆盖 codeblock state 数组路径。

**如果要测试 codeblock 路径**，需要新的 mock 提供一个能返回非 nil state 的 `State` 方法：
```go
type mockCodeblockBuffer struct{ mockBuffer }
var fakeRegion highlight.State = /* 任意非 nil 值 */
func (m *mockCodeblockBuffer) State(n int) highlight.State {
    if n >= 5 && n <= 7 { return fakeRegion }  // 模拟 5-7 行在 codeblock 内
    return nil
}
```

**优先级**：先加默认 mock（保持 22 个测试不挂），codeblock 路径的专门测试等 Step 1 主功能实现后再补。

---

## 决策 3：字符前景色来源 ✅ 已决定

### 3.1 决策内容

**MicroNeo render 不改变字符前景色**。字符前景色**完全用** micro highlighter 的结果。

### 3.2 实现位置

`internal/display/bufwindow.go` 的 `mergeStyle` 方法：

```go
func (w *BufWindow) mergeStyle(bufLine, bufX int, baseStyle tcell.Style) tcell.Style {
    if bufLine < 0 || bufX < 0 {
        return baseStyle  // 装饰字符：保留 render 风格
    }
    if group, ok := w.Buf.Match(bufLine)[bufX]; ok {
        s := config.GetColor(group.String())  // ← 关键：用 micro 算的 group 拿颜色
        // 背景：保留 render 风格（baseStyle）
        fg, _, attr := s.Decompose()
        _, bg, _ := baseStyle.Decompose()
        return tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attr)
    }
    return baseStyle
}
```

### 3.3 决策理由

- micro highlighter 算的 group 已经覆盖了**所有**字符（行内代码、python 块、bold、italic、链接等）
- 我们的 render 只标**背景色**（代码块背景、引用块背景等）
- **前景色用 micro 的** = 自动复用所有现有微高亮（python 块、js 块、用户配置嵌套 syntax 都能高亮）

### 3.4 不覆盖的细节

**Attribute（粗体/斜体/下划线）** —— **不在 Step 0 处理**。

- 之前设计说"只合并 foreground，不合并 attributes"
- 理由：attribute 处理是 Step 2（inline renderer）的责任
- Step 0 只管背景色 + 前景色

**实现**：mergeStyle 里的 `attr` **取** `s.Decompose()` 的 attr（即 micro 算出来的 attr），不取 baseStyle 的 attr。

如果 micro 没标 attr（大部分情况），`s.Decompose()` 的 attr = 0，`tcell.StyleDefault.Attributes(0)` 等于无 attribute。

### 3.5 与之前 2.7 节"用户自定义 markdown.yaml"的关系

在用户配置下：
- ` ```python ` 块内字符的 group 来自 `python.yaml`（如 `identifier` / `type` / `keyword`）
- mergeStyle 查 group → `config.GetColor(group.String())` → 拿到 python 颜色
- **完全保留**用户期望的 python 高亮

---

## 三件事的边界重申

| 决策 | 不混入的考量 |
|------|--------------|
| 决策 1（时序） | 不该用"内置配置下 state 是 nil"来反驳时序——时序跟 state 是否 nil 无关 |
| 决策 2（边界） | 不该用"时序问题"反驳边界识别方案——时序跟数据来源是两件事 |
| 决策 3（前景色） | 不该用"attribute 处理"反驳前景色方案——属性是另一回事 |

**严格分开思考、严格分开实现、严格分开测试**。
