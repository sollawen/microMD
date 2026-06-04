# Step 0 事件驱动 detect 实施方案

> 目标：把 `md.DetectSegments` 从 `displayBufferMD` 每帧重算，改为在 buffer 事件路径上跑一次
> 范围：4 个文件（`internal/md/*`、`internal/buffer/buffer.go`、`internal/display/bufwindow.go`、`internal/action/bufpane.go`），7 步改动
> 依赖：架构设计V1.md §3.6/§4、Step0-实施方案.md、Step0-事件驱动与代码块边界识别.md
> 状态：已 review，可执行

---

## 一、目标与背景

### 1.1 当前问题

`displayBufferMD` 每帧调 `md.DetectSegments(b, visibleStart, visibleEnd, bufWidth)`——

- **BufWidth 透传但 detect 不用**（Step 0 阶段 `detect.go` 函数体里根本没读过 bufWidth）
- **3 个 bug 不可避免**：
  - 滚到 codeblock 中间 → 错位（`codeblockStart = -1` 默认值未覆盖）
  - buffer 打开首帧错（highlighter async 竞态）
  - 滚到 blockquote/table/list 中间 → 多行结构被打散

### 1.2 设计原则

**Detect（分类）和 render（布局）完全解耦**：

| 阶段 | 职责 | 输入 | 触发 |
|------|------|------|------|
| **detect** | 哪个 render 对应哪几行 buffer | buffer 内容 + highlighter state | buffer 开 / 编辑（**均全量**） |
| **render** | lines 铺成 screen cells | segment + 屏宽 | display 每帧 |

**两条 trigger 路径**（与 micro highlighter 完全一致）：

| 触发点 | detect 范围 | 缓存写入 |
|--------|------------|----------|
| 文件打开（`UpdateRules` async） | `[0, End.Y]` 全量 | `b.MDSegments = md.DetectSegments(b, 0, End.Y)` |
| 用户编辑（`MarkModified`） | `[0, End.Y]` 全量 | `b.MDSegments = md.DetectSegments(b, 0, End.Y)` |

**Detect 不需要屏宽**（这跟 micro 的 `b.Match(L)[X]` 不依赖屏宽是同一回事）。

### 1.3 跟 micro 模式对应

| micro | MicroNeo |
|-------|----------|
| `b.Match(L)[X]` 算 group | `b.MDSegments` 算 render 归属 |
| 算：buffer 开/编辑（highlighter） | 算：buffer 开/编辑（detect） |
| 读：display 帧帧读 | 读：display 帧帧读 |
| 不依赖屏宽 | 不依赖屏宽 |
| `bufpane.go:285` `isMarkdownFile` 调用 | `NewBuffer` 里调一次，后续从 `b.IsMD` 读 |

### 1.4 三阶段总览

**为什么分 3 个阶段**：每个阶段都引入新风险点，自然拆分能让"哪里炸了"立刻可定位。

| 阶段 | 任务 | 触动点 | 完成定义 | 验证方式 |
|------|------|--------|----------|----------|
| **P1: Data Layer Foundation** | 1+2+3+4 | 字段搬家 | 字段就位，**行为不变** | `go build`+`go test`+无回归 |
| **P2: Detect on Events** | 5+6 | buffer.go 加 detect | `b.MDSegments` 填好，**display 仍 per-frame** | 调试日志/单元测试 |
| **P3: Display Reads Cache** | 7 | bufwindow.go 改读 | display 读缓存，**3 bug 修** | sample.md 手动 3 场景 |

**关键边界**：
- P1 完成后**没有可观察的行为变化**——只多了字段，display 仍调 per-frame detect
- P2 完成后**display 行为仍不变**——只是 `b.MDSegments` 多了一份"对的"数据，但 display 还在调 per-frame detect
- P3 完成后**才修 3 个 bug**——display 切换到读缓存，per-frame detect 取消

这样如果 P3 写崩了，回滚只损失最后一步的代码；如果 P2 写崩了，回滚只损失 P2 的 ~10 行；如果 P1 写崩了，连数据字段都没启用，不影响功能。

---

## 二、Phase 1 — Data Layer Foundation（行为不变）

**目标**：把 `MDSegments` 字段加到 `SharedBuffer`，把 `IsMD` 投到 `SharedBuffer`，清理 `DetectSegments` 不用的参数。

**行为契约**：完成后用户**感觉不到**任何变化——display 仍调 per-frame detect，所有 bug 仍存在。验证靠"无回归"。

### 任务 1：删 `DetectSegments` 的 `bufWidth` 参数

**文件**：`internal/md/detect.go`
**改动**：4 参数 → 3 参数

**当前签名**（`detect.go:35`）：

```go
func DetectSegments(
    buf BufferReader,
    visibleStart, visibleEnd int,
    bufWidth int,    // ← 删
) []Segment
```

**新签名**：

```go
func DetectSegments(
    buf BufferReader,
    visibleStart, visibleEnd int,
) []Segment
```

**同步改注释**（`detect.go:32-35`）：

```go
// 删除这 3 行：
// bufWidth 是渲染区域宽度（列数）。
//   - Step 0: 透传给 renderer，detect 自身不使用
//   - Step 1+: detect 用于计算各渲染片的视觉行高，输出到 SegmentMeta 缓存，供 Scroll/Diff 查询
```

函数体不需要改——本来就**没用过** `bufWidth`。

### 任务 2：同步改 18 个测试调用点

**文件**：`internal/md/detect_test.go`

**模式**：把 `DetectSegments(buf, X, Y, 80)` → `DetectSegments(buf, X, Y)`，第 4 个参数全删。

**已知 18 处**（来自 `grep -n "DetectSegments" detect_test.go`）：

```
L86,  L107, L127, L149, L163, L182, L201, L221, L239, L257,
L271, L286, L328, L368, L443, L461, L479, L530
```

### 任务 3：SharedBuffer 加 `MDSegments` 字段 + import md

**文件**：`internal/buffer/buffer.go`

**3.1 顶部 import**：

```go
import (
    ...
    "github.com/micro-editor/micro/v2/internal/md"   // ← 新增
    ...
)
```

**3.2 SharedBuffer 结构体加字段**（L68 附近，紧挨 `*LineArray`）：

```go
type SharedBuffer struct {
    *LineArray
    // ... 现有字段（ModTime, Type, Path, ...）...

    // MicroNeo: 检测分类结果，content-static，跟随 buffer 生命周期
    // P1 阶段：字段已声明、永远为 nil（display 仍 per-frame detect）
    // P2 阶段：事件驱动更新（开/编辑时跑全 buffer detect，结果存这里）
    // display 帧帧读
    // 跟 micro 的 b.Match 同属一类——buffer state 的一部分
    MDSegments []md.Segment
    // IsMD 在任务 4 同步添加——任务 3 阶段不设
}
```

> 实施阶段：任务 3 只加 `MDSegments`，任务 4 会同时加 `IsMD` 字段。两个字段在同一次 commit 里提交，避免中间态不完整。

**为什么放 `*SharedBuffer` 而不是 `*Buffer`**：

- `MarkModified` 是 `*SharedBuffer` 的方法（L185），直接 `b.MDSegments = ...` 即可
- `UpdateRules` 是 `*Buffer` 的方法（L820），通过嵌入 `*SharedBuffer` 也能访问 `b.MDSegments`
- micro 原生 `b.Match` / `b.State` 都在 `*LineArray`（嵌入 `*SharedBuffer`），把 `MDSegments` 放 `*SharedBuffer` 是同一层的逻辑
- 不需要在 `SharedBuffer` 加 back-pointer 指向 `Buffer`

**依赖方向确认**：

```
internal/action → internal/display → internal/buffer → internal/md
```

单向，`internal/buffer` import `internal/md` 不产生循环（`md` 只 import `pkg/highlight` 和 `tcell`，不 import `buffer`）。

### 任务 4：同步搬迁 `IsMD`（BufWindow → SharedBuffer）

**背景**：`IsMD` 原本只住在 `BufWindow`（`bufwindow.go:35`），由 `bufpane.go:285` 调 `md.IsMarkdownFile(buf.Path)` 算。但我们要在 `MarkModified` / `UpdateRules` 里门控 detect——那里是 buffer 层，拿不到 `w.IsMD`。

**方案**：把 `IsMD` 投到 `SharedBuffer`（数据的家），让两边都看得到。具体改动 3 个文件、4 处。

**4.1 `internal/buffer/buffer.go` — SharedBuffer 加 `IsMD` 字段**

L68 附近、`MDSegments` 上方加（与任务 3 的 `MDSegments` 同一次 commit 提交）：

```go
type SharedBuffer struct {
    *LineArray
    // ... 现有字段（ModTime, Type, Path, ...）...

    // MicroNeo: 标记该 buffer 是否为 Markdown 文件
    // 唯一真源；在 NewBuffer 里设一次，之后不变
    // 用途：MarkModified / UpdateRules 门控 detect，display 读
    IsMD bool

    // MicroNeo: 检测分类结果，content-static，跟随 buffer 生命周期
    // P1 阶段：字段已声明、永远为 nil（display 仍 per-frame detect）
    // P2 阶段：事件驱动更新（开/编辑时跑全 buffer detect，结果存这里）
    // display 帧帧读
    // 跟 micro 的 b.Match 同属一类——buffer state 的一部分
    MDSegments []md.Segment
}
```

**4.2 `internal/buffer/buffer.go` — `NewBuffer` 末尾设 `b.IsMD`**

L357 `NewBuffer` 末尾、 return 之前加：

```go
func NewBuffer(r io.Reader, size int64, path string, btype BufType, cmd Command) *Buffer {
    // ... 现有代码 ...
    b.IsMD = md.IsMarkdownFile(path)   // ← 新增（单一真源点）
    return b
}
```

**4.3 `internal/display/bufwindow.go:35` — 删 `IsMD` 字段**

原来是：
```go
type BufWindow struct {
    // ... 其他字段 ...
    IsMD     bool           // 由 BufPane 创建时设置
    // ... 其他字段 ...
}
```

**删** `IsMD bool` 那一行。以后从 `w.Buf.IsMD` 读（透过 `BufWindow.Buf *Buffer` 嵌入 `*SharedBuffer`）。

**4.4 `internal/display/bufwindow.go:908` — gate 改读 `w.Buf.IsMD`**

原来是：
```go
if w.IsMD {
    w.displayBufferMD()
} else {
    w.displayBuffer()
}
```

**改为**：
```go
if w.Buf.IsMD {
    w.displayBufferMD()
} else {
    w.displayBuffer()
}
```

**4.5 `internal/action/bufpane.go:285-286` — 取消重复算 `IsMarkdownFile`**

原来是：
```go
if md.IsMarkdownFile(buf.Path) {
    w.IsMD = true
    w.SetMDConfig(md.MDConfig{...})
}
```

**改为**：
```go
if buf.IsMD {                       // ← 从 b.IsMD 读（单一真源）
    w.SetMDConfig(md.MDConfig{...}) // ← 不再设 w.IsMD
}
```

**4.6 任务 4 净效果**

| 路径 | 原来 | 现在 |
|------|------|------|
| `IsMD` 唯一点 | bufpane 调 `md.IsMarkdownFile` | NewBuffer 调 `md.IsMarkdownFile` |
| 数据层读 | 拿不到 | `b.IsMD`（`MarkModified` / `UpdateRules`） |
| 显示层读 | `w.IsMD` | `w.Buf.IsMD` |
| `md.IsMarkdownFile` 调用点 | bufpane 1 处 | NewBuffer 1 处（位移） |

### Phase 1 验证

**build + test**：

```bash
cd /Users/sollawen/pi-dev/microNeo
go build ./...
go test ./internal/md/...   # 18 个 detect 测试全绿
```

**回归（手动）**：

- 打开 `docs/sample.md`（Markdown 文件）—— 视觉行为应**与重构前完全一致**（display 仍 per-frame detect）
- 打开任一非 MD 文件（如 `internal/md/detect.go`）—— 走原生 `displayBuffer`，无 MD 高亮，无报错
- 编辑、滚动、开关文件 —— 行为不变

**字段 sanity check**（临时加 print，或用 debugger）：

- `buf.IsMD` 对 MD 文件为 `true`，对非 MD 文件为 `false`
- `buf.MDSegments` 永远为 `nil`（P1 阶段没人写它）

**预期结论**：

- 编译通过 ✅
- 18 个 detect 测试全绿 ✅
- 用户视觉无变化 ✅
- 字段就位但未启用 ✅
- **3 个 bug 仍存在**（display 路径未动，符合预期）

**commit 建议**：

```bash
git add internal/md/detect.go internal/md/detect_test.go \
        internal/buffer/buffer.go \
        internal/display/bufwindow.go internal/action/bufpane.go
git commit -m "Step0 P1: data layer foundation (MDSegments field + IsMD relocation + DetectSegments signature cleanup)"
```

---

## 三、Phase 2 — Detect on Events（data 填好，display 仍 per-frame）

**目标**：在 `UpdateRules` async goroutine 和 `MarkModified` 里跑 detect，结果存 `b.MDSegments`。

**行为契约**：完成后 `b.MDSegments` 在 buffer 打开/编辑时正确填好。但 display **仍调 per-frame detect**——用户感觉不到任何改善，所有 3 个 bug 仍存在。验证靠"检查 `b.MDSegments` 数据正确"。

### 任务 5：`UpdateRules` 末尾的 async goroutine 加 detect

**文件**：`internal/buffer/buffer.go`
**位置**：`UpdateRules` 函数末尾的 `go func() { ... }()` 块（L1004-1012）

> 文档里习惯叫"SetSyntaxDef"，但 `SetSyntaxDef` 在 buffer.go 里不是独立函数——这个 goroutine 在 `UpdateRules` 函数体内末尾。

**当前代码**：

```go
if b.SyntaxDef != nil {
    b.Highlighter = highlight.NewHighlighter(b.SyntaxDef)
    if b.Settings["syntax"].(bool) {
        go func() {
            b.Highlighter.HighlightStates(b)
            b.Highlighter.HighlightMatches(b, 0, b.End().Y)
            screen.Redraw()       // ← 这里之前
        }()
    }
}
```

**新代码**：

```go
if b.SyntaxDef != nil {
    b.Highlighter = highlight.NewHighlighter(b.SyntaxDef)
    if b.Settings["syntax"].(bool) {
        go func() {
            b.Highlighter.HighlightStates(b)
            b.Highlighter.HighlightMatches(b, 0, b.End().Y)
            // MicroNeo: 事件驱动 detect，仅 MD 文件才进这个分支
            if b.IsMD {                                               // ← IsMD 真源在 SharedBuffer（P1 任务 4）
                b.MDSegments = md.DetectSegments(b, 0, b.End().Y)
            }
            screen.Redraw()
        }()
    }
}
```

**为什么放在 `HighlightMatches` 之后**：

- detect 用 `b.State(L)` 判断 codeblock 边界，必须等 highlighter 填完 state
- 放在 `screen.Redraw()` 之前，确保第一帧 paint 时 `b.MDSegments` 已就绪

**isMD 门控**（与任务 6 共用）：

- 读 `b.IsMD`（P1 任务 4 设在 NewBuffer），不再调 `md.IsMarkdownFile`
- 非 MD 文件：highlighter 仍跑（原生逻辑），detect 不跑——这条路径上 `b.MDSegments` 保持 nil

### 任务 6：`MarkModified` 末尾加 detect（全量 re-detect）

**文件**：`internal/buffer/buffer.go`
**位置**：`MarkModified` 函数（`buffer.go:185`）

**当前代码**：

```go
func (b *SharedBuffer) MarkModified(start, end int) {
    b.ModifiedThisFrame = true

    start = util.Clamp(start, 0, len(b.lines)-1)
    end = util.Clamp(end, 0, len(b.lines)-1)

    if b.Settings["syntax"].(bool) && b.SyntaxDef != nil {
        l := -1
        for i := start; i <= end; i++ {
            l = util.Max(b.Highlighter.ReHighlightStates(b, i), l)
        }
        b.Highlighter.HighlightMatches(b, start, l)
    }

    for i := start; i <= end; i++ {
        b.LineArray.invalidateSearchMatches(i)
    }
}
```

**新代码**：

```go
func (b *SharedBuffer) MarkModified(start, end int) {
    b.ModifiedThisFrame = true

    start = util.Clamp(start, 0, len(b.lines)-1)
    end = util.Clamp(end, 0, len(b.lines)-1)

    if b.Settings["syntax"].(bool) && b.SyntaxDef != nil {
        l := -1
        for i := start; i <= end; i++ {
            l = util.Max(b.Highlighter.ReHighlightStates(b, i), l)
        }
        b.Highlighter.HighlightMatches(b, start, l)
        // MicroNeo: 事件驱动 detect（Step 0 全量 re-detect）
        if b.IsMD {
            b.MDSegments = md.DetectSegments(b, 0, b.End().Y)
        }
    }

    for i := start; i <= end; i++ {
        b.LineArray.invalidateSearchMatches(i)
    }
}
```

**为什么 Step 0 用全量而非增量**：

- `DetectSegments` 增量调用时，blockquote/table/list 状态机从 `stateNormal` 起步，无法恢复上下文——编辑落在多行结构中间会产出错误段
- 全量 detect 是纯 CPU 线性扫描（每行几个字符串比较 + `buf.State(y)` 查询），1000 行 ~ 微秒级，实际无性能影响
- 增量 + splice 方案需新增 ~60 行代码和边界续接逻辑，收益在 Step 0 阶段为零
- **增量优化留给 Step 1+**，届时 detect 函数需增加状态恢复机制

**竞态说明**：`UpdateRules` goroutine 和 `MarkModified` 都写 `b.MDSegments`。Go slice 赋值是原子的，不会 crash。最坏情况是某一帧渲染不完整，下一帧自动修复。这与 micro 原生 `HighlightMatches` 的模式一致——可接受。

### Phase 2 验证

**build + test**：

```bash
go build ./...
go test ./internal/md/...   # 18 个 detect 测试仍全绿
go test ./...               # 整仓测试
```

**data layer sanity check**（临时加 print，或用 debugger，或写集成测试）：

- 打开 `docs/sample.md` 后等 highlighter async 完成
- 检查 `buf.MDSegments != nil`，长度 > 0
- 检查第一段的 `Render` 类型（如 `#` 开头的行应是 `RenderHeading`）
- 编辑一行（如加一个 `#`），触发 `MarkModified`
- 检查 `buf.MDSegments` 长度变化、相关段 `Render` 类型更新

**display 行为检查**（手动）：

- 打开 `docs/sample.md` —— 视觉行为**仍与重构前完全一致**（display 仍 per-frame detect，3 个 bug 仍存在）
- 编辑、滚动 —— 行为不变
- **关键观察**：`b.MDSegments` 已被填好但 display 不读它

**预期结论**：

- 编译通过 ✅
- 18 detect 测试全绿 ✅
- 用户视觉无变化 ✅
- `b.MDSegments` 在 open/edit 时正确填好 ✅
- **3 个 bug 仍存在**（display 路径未动，符合预期）

**commit 建议**：

```bash
git add internal/buffer/buffer.go
git commit -m "Step0 P2: event-driven detect (UpdateRules + MarkModified, full re-detect)"
```

---

## 四、Phase 3 — Display Reads Cache（3 bug 修复）

**目标**：`displayBufferMD` 改读 `b.MDSegments`，不再每帧调 detect。3 个 bug 修复。

**行为契约**：完成后用户能感受到——3 个 bug 修了，display 不再每帧跑 detect。

### 任务 7：`displayBufferMD` 改读 `b.MDSegments`

**文件**：`internal/display/bufwindow.go`
**位置**：`displayBufferMD` 函数（`bufwindow.go:946`）

本任务包含 4 个子动作：① 改读 MDSegments、② 加 filter helper、③ render 副作用填 mdCache、④ 清理 TODO。

---

**7.1 改读 MDSegments**

**当前代码**：

```go
segments := md.DetectSegments(b, visibleStart, visibleEnd, bufWidth)
// TODO(Step 3): 将检测结果写入 w.mdCache 供 Scroll/Diff 查询
```

**新代码**：

```go
// 每帧清空 mdCache（防止无限增长）
w.mdCache = w.mdCache[:0]

// 读 buffer 上的分类结果（事件驱动算好，content-static）
// 注意：非 MD 文件的 b.MDSegments 保持 nil，filterSegmentsToVisible 要处理 nil
allSegs := b.MDSegments
segments := filterSegmentsToVisible(allSegs, visibleStart, visibleEnd)
```

---

**7.2 加 `filterSegmentsToVisible` helper**

`bufwindow.go` 里加（不导出的辅助函数）：

```go
func filterSegmentsToVisible(segs []md.Segment, startY, endY int) []md.Segment {
    if segs == nil {
        return nil  // 非 MD 文件走原生 micro 路径
    }
    var out []md.Segment
    for _, s := range segs {
        if s.BufEndLine < startY {
            continue  // 完全在可视范围之上
        }
        if s.BufStartLine > endY {
            continue  // 完全在可视范围之下
        }
        // 至少部分重叠，截断到可视范围
        if s.BufStartLine < startY {
            s.BufStartLine = startY
        }
        if s.BufEndLine > endY {
            s.BufEndLine = endY
        }
        out = append(out, s)
    }
    return out
}
```

> 截断逻辑里"起点被截断"理论上不应发生（事件驱动 detect 跑全 buffer，不会出现段头在屏外）。这里留截断是防御性写法。

---

**7.3 render 副作用填 `w.mdCache`**

在 `displayBufferMD` 的渲染循环里，每段 render 完顺手填：

```go
for _, seg := range segments {
    ...
    rendered := seg.Render(w.linesFromBuffer(seg.BufStartLine, seg.BufEndLine), bufWidth, w.mdConfig)

    // 写 screen（原有逻辑）
    ...

    // 副作用：填 w.mdCache
    w.mdCache = append(w.mdCache, md.SegmentMeta{
        BufStartLine: seg.BufStartLine,
        BufEndLine:   seg.BufEndLine,
        RowCounts:    computeRowCounts(rendered),   // 每行占几个 screen row
    })
}
```

`computeRowCounts` 实现：

```go
// RenderedSegment.Rows 是 screen 行列表，每行对应 buffer 某行
// 对每个 buffer 行（0..len(lines)-1）数它对应的 screen 行数
func computeRowCounts(rendered *md.RenderedSegment) []int {
    counts := make([]int, 0, len(rendered.Rows))
    var lastBufLine int = -1
    for _, row := range rendered.Rows {
        if row.BufLine != lastBufLine {
            counts = append(counts, 1)
            lastBufLine = row.BufLine
        } else {
            counts[len(counts)-1]++
        }
    }
    return counts
}
```

> `w.mdCache` 现有定义（`bufwindow.go:37`）保持不变，只是从"未填"变"render 时填"。

---

**7.4 清理 TODO 注释**

删除 `bufwindow.go:947` 的 `// TODO(Step 3): 将检测结果写入 w.mdCache 供 Scroll/Diff 查询`。

该 TODO 已被 7.3 的 `w.mdCache = append(...)` 替代。

### Phase 3 验证

**build + test**：

```bash
go build ./...
go test ./...
```

**手动 3-bug 验证**（**这是 P3 才有意义的验证**——P1/P2 阶段这 3 个 bug 仍存在）：

打开 `docs/sample.md`（仓库根有），覆盖以下场景：

| 场景 | 期望 | 对应 bug |
|------|------|----------|
| 打开文件，首屏 | 渲染正常，codeblock/heading/blockquote 都对 | — |
| 滚到 codeblock 中间，codeblock 起点在屏外 | 屏上仍正确显示 codeblock | **bug 1 修** |
| 编辑（增删字符），触发 `MarkModified` | 渲染实时跟随，无闪烁 | **bug 2 修**（首帧由 P2 保证） |
| 滚到 blockquote 中间 | 多行 blockquote 不会被打散 | **bug 3 修** |

**性能 sanity check**：

```bash
# 打开 1000 行 markdown
# 滚动 / 编辑 10 次
# CPU 应该不持续高占用（detect 不再每帧跑）
```

**预期结论**：

- 编译通过 ✅
- 所有测试全绿 ✅
- 3 个 bug 修复 ✅
- display 不再每帧跑 detect（性能改善）✅

**commit 建议**：

```bash
git add internal/display/bufwindow.go
git commit -m "Step0 P3: displayBufferMD reads b.MDSegments (event-driven detect active, 3 bugs fixed)"
```

---

## 五、跨阶段累计 — 状态对照

| 阶段 | `buf.IsMD` | `buf.MDSegments` | display 路径 | 3 个 bug |
|------|-----------|------------------|-------------|----------|
| **初始** | — | — | per-frame detect | 全存在 |
| **P1 完成** | ✅ 已设 | nil（字段未写） | per-frame detect（不变） | 全存在 |
| **P2 完成** | ✅ 已设 | ✅ open/edit 时填好 | per-frame detect（**不变**） | 全存在 |
| **P3 完成** | ✅ 已设 | ✅ open/edit 时填好 | **读 `b.MDSegments`** | **全修** |

P1/P2 的"bug 仍存在"是**设计上的预期**——这两个阶段都是无 user-visible 行为变化的"准备"阶段。如果 P2 完成后 bug 修了，说明 P3 的 display 切换是冗余的；如果 P3 完成后 bug 没修，说明 data layer 写错了。

---

## 六、风险与决策记录

### 6.1 已决定

1. **签名**：删 `bufWidth`，3 参数（决策记录见 `Step0-事件驱动与代码块边界识别.md` 决策 1 + 本次讨论）
2. **存储位置**：`SharedBuffer.MDSegments`（同 micro 的 `b.Match(L)` 归属）
3. **门控真源**：`SharedBuffer.IsMD`（从 `BufWindow.IsMD` 投过来，NewBuffer 算一次，后续唯一真源）。BufPane / display 都从 `b.IsMD` / `w.Buf.IsMD` 读，不再独立调 `md.IsMarkdownFile`
4. **更新策略**：Step 0 全量——文件开和编辑均跑 `DetectSegments(b, 0, End.Y)`（P2 任务 5 `UpdateRules` async + 任务 6 `MarkModified`）。增量优化留给后续 step
5. **mdCache 填充**：render 副作用，display 路径上
6. **三阶段拆分**：每个阶段独立可编译可测试。P1 数据准备、P2 数据填好、P3 消费切换

### 6.2 待办（不在本次范围）

- 增量 `MarkModified` 的优化：Step 0 用全量 re-detect，跳过 splice 边界问题。Step 1+ 需给 `DetectSegments` 增加状态恢复机制（传入 `prevState`），才能正确做增量
- 非 MD 文件的处理：`b.MDSegments` 保持 nil（`displayBufferMD` 里需要 nil check，已记入 P3 任务 7.1）
- `md.IsMarkdownFile` 调用点从 `bufpane.go:285` 搬到 `NewBuffer`——本次不重谈设计，只跟随 `IsMD` 搬迁。原始设计见 `Step0-实施方案.md` 任务 2（`internal/md/config.go:40` 已实现，本计划不重述）
- 跨段边界的"截断后段头"逻辑：filterSegmentsToVisible 里 `s.BufStartLine = startY` 之后，render 端怎么处理"上半段在屏外"的行——Step 1 的 scroll 优化内容，本次只保证 filter 不丢段

### 6.3 已知遗留

- `BufPane` 拦截方向键（PRD 3.1）属于 Step 1，本次不动
- Scroll/Diff 用 `w.mdCache` 的实际查询逻辑属于 Step 3，本次只填 cache，不查 cache
- `bufpane.go:286` 原 `w.IsMD = true` 不再设`w.IsMD`（w.IsMD 字段被删），仅保留 `w.SetMDConfig`。如有他处读 `w.IsMD` 会编译报错，按需调整

---

## 七、改动清单速查（按 phase 分组）

### 7.1 Phase 1 — Data Layer Foundation

| 任务 | 文件 | 关键改动 | 风险 |
|------|------|----------|------|
| 1 | `internal/md/detect.go` | 删 bufWidth 参数 | 🟢 低（参数未使用） |
| 2 | `internal/md/detect_test.go` | 18 处调用删第 4 参数 | 🟢 低（机械替换） |
| 3 | `internal/buffer/buffer.go` | 加 import + `SharedBuffer.MDSegments` 字段 | 🟡 中（首次让 buffer 依赖 md 包） |
| 4a | `internal/buffer/buffer.go` | `SharedBuffer.IsMD` 字段 + `NewBuffer` 算 | 🟡 中（首次让 buffer 依赖 md 包） |
| 4b | `internal/display/bufwindow.go:35` | **删** `IsMD bool` 字段 | 🟢 低（全 MicroNeo 代码） |
| 4c | `internal/display/bufwindow.go:908` | `w.IsMD` → `w.Buf.IsMD` | 🟢 低（全 MicroNeo 代码） |
| 4d | `internal/action/bufpane.go:285-286` | 读 `buf.IsMD`，不设 `w.IsMD` | 🟢 低（全 MicroNeo 代码） |

### 7.2 Phase 2 — Detect on Events

| 任务 | 文件 | 关键改动 | 风险 |
|------|------|----------|------|
| 5 | `internal/buffer/buffer.go` | `UpdateRules` async goroutine 加 detect（`if b.IsMD` 门控） | 🟢 低 |
| 6 | `internal/buffer/buffer.go` | `MarkModified` 加 detect（`if b.IsMD` 门控，全量 re-detect） | 🟢 低（3 行代码） |

### 7.3 Phase 3 — Display Reads Cache

| 任务 | 文件 | 关键改动 | 风险 |
|------|------|----------|------|
| 7 | `internal/display/bufwindow.go` | `displayBufferMD` 改读 `b.MDSegments` + filter helper + render 副作用填 mdCache + 删 TODO | 🟡 中（filterSegmentsToVisible 新增 helper） |

### 7.4 汇总

| 维度 | P1 | P2 | P3 | 合计 |
|------|-----|-----|-----|------|
| 改动文件 | 5 | 2 | 1 | 4（去重） |
| 任务数 | 4 (1+2+3+4) | 2 (5+6) | 1 (7) | 7 |
| `internal/buffer/buffer.go` 净增行 | ~5 | ~5 | 0 | ~10 |
| `internal/display/bufwindow.go` 净增行 | ~0 | 0 | ~15 | ~15 |
| `internal/action/bufpane.go` 净增行 | 0 | 0 | 0 | 0 |
| `internal/md/*` 净增行 | -1 | 0 | 0 | -1 |
| 风险为 🟡 的任务 | 3, 4a | — | 7 | 3 |
| 阶段验证 | 编译+测试+无回归 | 编译+测试+data 检查 | 编译+测试+3 bug 手动 | — |

---

## 八、文件总速查

```
Phase 1:
  internal/md/detect.go                 [任务 1]   🟢 删 bufWidth
  internal/md/detect_test.go            [任务 2]   🟢 删 18 处调用第 4 参数
  internal/buffer/buffer.go             [任务 3, 4a]   🟡 加 2 字段 + import
  internal/display/bufwindow.go         [任务 4b, 4c]   🟢 删 1 字段 + 改 1 行
  internal/action/bufpane.go            [任务 4d]   🟢 改 2 行

Phase 2:
  internal/buffer/buffer.go             [任务 5, 6]   🟢 2 处 detect（均全量）

Phase 3:
  internal/display/bufwindow.go         [任务 7]   🟡 改读 + filter helper + mdCache
```

确认这份计划就开干。
