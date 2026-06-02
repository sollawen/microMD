# 新的 displayBufferMD 架构方案

> 日期：2026-06-02
> 状态：提案
> 取代：`docs/架构设计.md` 中关于 Pipeline / 5 层架构的描述

---

## 0. 这份文档的来历

经过几轮讨论，我们意识到之前的 5 层架构（Host → Pipeline → Registry → Segment → Inline）是**为"在 Micro displayBuffer 里小修小改"这种方式设计的**。

当我们决定**重写 .md 文件的整个渲染主函数**（displayBufferMD）之后，Pipeline 这一层就不再需要——displayBufferMD 自己就是协调者。

这份文档描述新的、更简化的架构。

---

## 1. 核心决策

### 1.1 重写而不是改装

我们**不**在 Micro 原版的 `displayBuffer()` 里加 if 分支。我们**新写一个** `displayBufferMD()` 函数，专门处理 `.md` 文件。

- 非 `.md` 文件：继续用 Micro 原版 `bufwindow.go` 里的 `displayBuffer()`，几乎不动
- `.md` 文件：用我们的 `displayBufferMD()`，完全独立的渲染逻辑

### 1.2 渲染层自由

`displayBufferMD` 接管"buffer → screen"的整条路径。我们可以决定：

- 一行 buffer 渲染成几行屏幕（segment 多行输出）
- 单元格内容怎么 softwrap
- 加多少边框、padding、装饰行
- gutter 长什么样（v0.1 不画）
- 光标怎么显示

不再受 Micro 原版"一个 buffer 行对应一段屏幕内容"的心智模型约束。**这就是渲染层应该有的自由**。

### 1.3 保留 Micro 的以下机制不动

- `buffer`（文件数据、Line.data、Line.match、Line.state）
- 语法高亮（Highlighter，编辑时增量分析，结果缓存在 Line.match）
- `screen`（屏幕缓冲，cell 写入接口）
- 用户交互（action 层、按键、光标移动、滚动）
- 非渲染相关的 display 函数（`displayStatusLine`、`displayScrollBar`、`updateDisplayInfo`）

我们**只换渲染层**。其它一切都是 Micro 的。

---

## 2. 架构：4 个模块 + 1 个工具

```
┌──────────────────────────────────────────────────┐
│  displayBufferMD（渲染主函数）                     │
│  ────────────────────────────                    │
│  - 每帧由 BufWindow.Display() 调用（仅 .md 文件）  │
│  - 主循环：按屏幕行遍历                            │
│  - 自己协调 Registry / Mapper / Segment / Inline  │
│  - 把 RenderCell 写到 screen                       │
└──┬─────────────┬──────────────┬─────────────────┘
   ▼             ▼              ▼
┌────────┐  ┌──────────┐  ┌─────────────────┐
│Registry│  │  Mapper  │  │     Segment      │
│        │  │          │  │  (interface)     │
│ 检测、  │  │ 屏幕行 ↔ │  │  IsEditState     │
│ 索引、  │  │ buffer行 │  │  Render          │
│ 缓存    │  │          │  │                  │
└────────┘  └──────────┘  └────────┬─────────┘
                                    │ uses
                                    ▼
                            ┌──────────────┐
                            │  Inline       │
                            │  Renderer     │
                            │  (粗体/斜体)   │
                            └──────────────┘
```

### 2.1 为什么是 4 个模块而不是 5 层

之前 5 层架构里有 Pipeline 这层。**Pipeline 是"为方式 1（在 Micro displayBuffer 内插入）而生"的封装层**。

重写后，displayBufferMD 自己就是协调者——它直接调 Registry / Mapper / Segment / Inline，不需要再隔一层 Pipeline。Pipeline 的职责（主循环 + 协调）变成 displayBufferMD 的循环体本身。

每个剩下的模块都对应一个**真实的、不可替代的职责**：

| 模块 | 不可替代的原因 |
|------|--------------|
| `displayBufferMD` | 没有它就没法画屏幕 |
| `Registry` | 没有它每帧要扫整个 buffer 找 segment |
| `Mapper` | v0.5+ 必须，否则屏幕行 ↔ buffer 行没法对应 |
| `Segment` | 没有它就没有可扩展性（每种 Markdown 元素一个实现） |
| `Inline Renderer` | 被多个 Segment 复用，去 `**`/`*`/`` ` `` 等标记 |

**没有"为了架构好看而加的层"**。

### 2.2 与 Micro 现有系统的关系

```
Micro 主循环
  └─ BufWindow.Display()                        ← Micro 原版，几乎不动
       ├─ updateDisplayInfo()                   ← Micro 原版
       ├─ displayStatusLine()                   ← Micro 原版
       ├─ displayScrollBar()                    ← Micro 原版
       │
       ├─ if 是.md文件 && mdrender开启 {
       │     md.DisplayBufferMD(w, reg, mp)     ← ★ 我们的渲染
       │ } else {
       │     w.displayBuffer()                  ← Micro 原版（非 .md）
       │ }
       │
       └─ (完)
```

我们的代码只在"渲染 buffer 内容"这一步介入。其它所有显示相关的步骤（状态栏、滚动条、几何参数计算）都还是 Micro 的。

---

## 3. displayBufferMD 的工作流程

### 3.1 主循环（关键设计：按屏幕行遍历，不是按 buffer 行）

```go
func DisplayBufferMD(w *display.BufWindow, reg *Registry, mp *Mapper) {
    buf := w.Buf
    cursors := buf.GetCursors()
    
    // 计算视口、gutter 等
    // v0.1 简化：.md 文件无 gutter、无 softwrap
    
    for screenRow := 0; screenRow < w.bufHeight; screenRow++ {
        // 1. 通过 Mapper 算这一屏幕行对应 buffer 哪里
        bufferLine, rowInSegment := mp.ScreenToBuffer(screenRow)
        
        // 2. 查 Registry 拿 segment
        segment := reg.GetSegmentAt(bufferLine)
        
        // 3. 决定走 segment 还是 Micro 原生 fallback
        var cells []RenderCell
        if segment.IsEditState(cursors) {
            cells = microOriginalRenderLine(buf, bufferLine)   // fallback
        } else {
            cells = segment.Render(bufferLine, rowInSegment)
        }
        
        // 4. 写 screen
        for i, cell := range cells {
            screen.SetContent(screenX, screenRow, cell.Rune, cell.Style)
            screenX += cell.Width
        }
    }
    
    // 5. 画光标（v0.1 复用 Micro 的 showCursor）
}
```

### 3.2 关键设计点

**按屏幕行遍历，不按 buffer 行**：这是和 Micro 原版最大的区别。原版的循环是 `for 每个 buffer 行`，因为原版假设 1 buffer 行 = 1 屏幕行（softwrap 关闭时）。

我们换成 `for 每个屏幕行`，因为 segment 输出可能是多行（表格加边框、代码块加框等）。Mapper 负责把"屏幕行号"翻译成"buffer 行号 + segment 内的子行号"。

**Segment 是被动数据**：Segment 只回答问题，不主动驱动循环。displayBufferMD 问"这一行画什么"，Segment 回答 `[]RenderCell`。

**Edit state 决策在 Segment**：Segment 自己判断"光标是不是在我范围内"。displayBufferMD 只问 `segment.IsEditState(cursors)`，得到 yes 就走 Micro 原生 fallback。

**Fallback 是显式的分支**：edit state → 调 `microOriginalRenderLine`（复用 Micro 的逐 rune 渲染逻辑）。这不是异常路径，是正常分支。

---

## 4. 各模块的职责与接口契约

### 4.1 displayBufferMD（主函数）

**位置**：`md/bufwindow_md.go`

**职责**：渲染 .md 文件的整个 buffer 视图。

**签名**：

```go
func DisplayBufferMD(
    w *display.BufWindow,    // Micro 的视口对象，提供 buf/cursors/width/height 等
    reg *Registry,           // segment 注册中心
    mp *Mapper,              // 坐标映射
)
```

**不做的事**：
- 不修改 buffer
- 不修改 Micro 的 BufWindow 状态（视口位置由 Micro 的 Relocate 维护）
- 不处理状态栏、滚动条（这些是 BufWindow.Display 的事）

### 4.2 Registry（segment 注册中心）

**位置**：`md/registry.go`

**职责**：知道当前 buffer 里有哪些 segment、各自的范围。对外提供"给一行号，告诉我谁负责"。

**接口**：

```go
type Registry struct {
    // 内部维护按 startLine 排序的 segment 列表
    segments []Segment
    detectors []Detector
}

type Detector func(buf *buffer.Buffer, startLine, endLine int) []Segment

func (r *Registry) GetSegmentAt(line int) Segment
func (r *Registry) Rebuild(buf *buffer.Buffer)            // 全量重建
func (r *Registry) Invalidate(startLine, endLine int)     // 部分失效
```

**实现要点**：
- 二分查找 `GetSegmentAt`
- Detector 列表按优先级排序（表格、代码块等 specialty 在前，paragraph 在最后兜底）
- v0.1 阶段：编辑后用 `buffer.ModifiedThisFrame` 触发 `Invalidate`，下一帧重建

### 4.3 Mapper（坐标映射）

**位置**：`md/mapper.go`

**职责**：屏幕行 ↔ buffer 行的双向映射。

**接口**：

```go
type Mapper struct {
    startLoc SLoc   // 视口起点（bufferLine + rowInSegment）
    lineHeights map[int]int   // buffer 行号 → 占几个屏幕行
}

func (m *Mapper) ScreenToBuffer(screenRow int) (bufferLine, rowInSegment int)
func (m *Mapper) BufferToScreen(bufferLine, rowInSegment int) (screenRow int)
func (m *Mapper) RecordLineHeight(bufferLine, height int)   // 每帧渲染时更新
func (m *Mapper) Invalidate(startLine, endLine int)
```

**v0.1 行为**：所有 segment 输出 1:1，`lineHeights` 全是 1，Mapper 是 passthrough。

**v0.2+ 行为**：表格 segment 输出多行（加边框、表头分隔），Mapper 开始真正工作。

### 4.4 Segment（渲染单元）

**位置**：`md/segment.go`

**接口**：

```go
type Segment interface {
    StartLine() int
    EndLine() int
    Contains(line int) bool
    
    IsEditState(cursors []*buffer.Cursor) bool
    
    // 返回这一 buffer 行的某一子行的渲染结果
    // rowInSegment = 0 表示第一行（v0.1 永远是 0，因为输出 1:1）
    Render(bufferLine int, rowInSegment int) []RenderCell
}

type RenderCell struct {
    Rune  rune
    Style tcell.Style
    Width int       // 1 或 2（CJK）
    Kind  CellKind  // Content / Decoration / Padding（v0.5 鼠标点击用）
}
```

**实现**：每种 Markdown 元素一个 Segment 类型——TableSegment、CodeSegment、HeadingSegment、ParagraphSegment 等。

### 4.5 Inline Renderer（行内渲染工具）

**位置**：`md/inline.go`

**职责**：处理行内 markdown（粗体、斜体、行内代码、链接），被 Segment 调用。

**接口**：

```go
type InlineRenderer struct {
    highlighter func(buffer.Loc) tcell.Style   // 适配 Micro 的 getStyle
}

func (r *InlineRenderer) Render(
    bufferLine int,
    text []byte,
) []RenderCell
```

**关键约束**：
- 跨行闭合保守处理（标记未闭合则 passthrough）
- 输出叠加 Micro 语法高亮颜色（通过 highlighter 函数读取 Line.match）

**v0.1 行为**：passthrough，输入啥输出啥。

**v0.3+ 行为**：去 `**`/`*`/`` ` ``/`[]()` 等标记，加粗体/斜体/链接样式。

---

## 5. Segment Registry 的兜底逻辑

### 5.1 ParagraphSegment 作为兜底

任何 buffer 行都会被某个 Segment 接管。如果一行不是表格、不是代码块、不是标题...那它就是普通段落，由 ParagraphSegment 接管。

**ParagraphSegment 的 Render()**：

```go
func (s *ParagraphSegment) Render(bufferLine int, rowInSegment int) []RenderCell {
    text := s.buf.LineBytes(bufferLine)
    return s.inline.Render(bufferLine, text)
}
```

v0.1 阶段 `inline.Render` 是 passthrough，所以 ParagraphSegment 输出 = buffer 原文。

### 5.2 Detector 注册顺序

```go
detectors := []Detector{
    CodeBlockDetector,    // 优先识别代码块（避免代码块里的 `|` 被误识别为表格）
    TableDetector,        // 然后识别表格
    HeadingDetector,
    ListDetector,
    // ... 其他 specialty
    ParagraphDetector,    // 最后兜底
}
```

每个 Detector 扫描 buffer，识别自己负责的范围，剩下的"无人认领"行由 ParagraphDetector 兜底。

---

## 6. 与 Micro 现有代码的边界

### 6.1 我们要改的文件

```
internal/display/bufwindow.go
  - BufWindow.Display()：加入 .md 文件分流
  - 大约 +10 行
```

### 6.2 我们要新增的文件

```
internal/display/md/
  bufwindow_md.go    displayBufferMD 主函数（~300-500 行）
  segment.go         Segment 接口 + RenderCell 类型（~50 行）
  registry.go        Registry（~150 行）
  mapper.go          Mapper（v0.1 ~30 行 passthrough，v0.5+ ~200 行）
  inline.go          Inline Renderer（v0.1 ~30 行 passthrough，v0.3+ ~200 行）
  highlight.go       Micro 高亮适配（~50 行）
  
  table.go           TableSegment（~200 行，复用现有 markdown_table.go 的检测逻辑）
  paragraph.go       ParagraphSegment（~50 行）
```

预计 v0.1 总代码量 **~1000 行**（含从 Micro 原版复制的渲染工具函数）。

### 6.3 我们要废弃的文件

```
internal/display/markdown_table.go
  - 旧的 inline padding 方式
  - v0.1 重写完成后删除（或保留作为参考）
```

### 6.4 我们完全不动的文件

```
internal/buffer/          整个目录（buffer、Line.data、Line.match、Highlighter）
internal/action/          整个目录（按键处理、光标移动）
internal/screen/          整个目录（屏幕缓冲）
internal/display/softwrap.go    （Micro 原版 softwrap，.md 不用）
internal/display/statusline.go  （状态栏）
internal/display/...其他         （InfoWindow、TabWindow 等）
```

---

## 7. 一帧的数据流

```
Micro 主循环 → BufWindow.Display()
                  │
                  ├─ updateDisplayInfo()
                  ├─ displayStatusLine()
                  ├─ displayScrollBar()
                  │
                  └─ if 是.md文件 && mdrender开启 {
                         DisplayBufferMD(w, reg, mp)
                                │
                                ▼
                         ┌─────────────────────────────────┐
                         │ 1. 读 buffer.ModifiedThisFrame   │
                         │    若为 true：                    │
                         │      registry.Invalidate(...)    │
                         │      registry.Rebuild(buf)       │
                         ├─────────────────────────────────┤
                         │ 2. for 每个屏幕行 (i = 0..height)│
                         │      line, row := mp.ScreenToBuffer(i) │
                         │      segment := reg.GetSegmentAt(line) │
                         │      if segment.IsEditState(cursors) { │
                         │        cells = microOriginal(line)     │
                         │      } else {                          │
                         │        cells = segment.Render(line, row)│
                         │      }                                 │
                         │      for cell in cells {               │
                         │        screen.SetContent(...)          │
                         │      }                                 │
                         ├─────────────────────────────────┤
                         │ 3. 画光标                          │
                         └─────────────────────────────────┘
                     } else {
                         w.displayBuffer()   ← Micro 原版
                     }
```

---

## 8. 与之前 5 层架构的差异

| 方面 | 5 层架构（旧） | 4 模块架构（新） |
|------|-------------|---------------|
| 协调者 | Pipeline 这个独立 struct | displayBufferMD 函数本身 |
| 主循环 | Pipeline 内部 | displayBufferMD 内部 |
| 模块数 | 5 层 | 4 模块 + 1 工具 |
| 抽象层数 | 多一层（Pipeline） | 少一层 |
| 主循环遍历 | 按 buffer 行（受 displayBuffer 影响） | **按屏幕行** |
| Segment 多行输出 | 通过 Pipeline 协调 | Mapper 直接支持 |
| 测试 | 需构造 Pipeline 实例 | displayBufferMD 直接测 |

**核心简化**：displayBufferMD 自己就是协调者，不需要再隔一层 Pipeline。

---

## 9. 探照灯模型仍然成立

之前讨论的"探照灯模型"（光标驱动 segment edit state）在新架构下自然成立：

```
用户的光标始终在某一行
  → 该行所在的 segment 进入 edit state
  → 该 segment 走 Micro 原生 fallback
  → 用户看到光标所在行的 markdown 原文
  → 用户移动光标
  → 旧 segment 退出 edit state，恢复渲染
  → 新 segment 进入 edit state，回退原生
```

**没有"渲染模式 vs 编辑模式"的全局状态机**。Micro 的输入处理、光标移动、滚动等所有交互保持原样。

---

## 10. 演进路径

### v0.1（架构骨架）
- displayBufferMD 主函数（按屏幕行遍历）
- Registry + ParagraphSegment 兜底
- TableSegment（输出 1:1）
- Mapper passthrough
- Inline passthrough
- 配置开关 `mdrender`（默认 true）

### v0.2（表格真渲染）
- TableSegment 输出多行（加边框、表头分隔）
- Mapper 开始真正工作（追踪每行高度）
- 状态栏 READING/EDITING 指示

### v0.3（Inline 真渲染）
- Inline Renderer 实现粗体/斜体/行内代码/链接
- 叠加 Micro 语法高亮颜色

### v0.4（其他结构渲染器）
- CodeSegment、HeadingSegment、ListSegment、BlockquoteSegment 等
- 每个 Segment 独立开发，可分别合并

### v0.5（单元格内 softwrap + 鼠标精确定位）
- 表格单元格内容按词 softwrap
- Segment 提供 `MapScreenToBuffer` 接口
- 鼠标点击装饰字符（边框）视为无效点击

### v0.6（可选：纯阅读模式）
- 在 BufWindow.Display() 之上再加一层 Mode Controller
- 控制"光标隐藏 / 方向键滚动 / 10 秒计时器"
- 默认关闭，配置 `mdrenderidle > 0` 启用

---

## 11. 待讨论

### Q1：displayBufferMD 是否需要为每个 pane 创建独立实例？

不需要。displayBufferMD 是无状态函数（输入 → 写 screen），可以在 pane 之间共享。Registry 和 Mapper 也只依赖 buffer，不依赖具体 pane。

但 pane 的视口（StartLine）是独立的，所以同一个 buffer 在多个 pane 里渲染时，每个 pane 用自己的 StartLine、cursors、width 调用 displayBufferMD。

### Q2：Registry 缓存挂在哪？

挂在 Registry 内部。Registry 持有 buffer 的弱引用，缓存按 buffer 行号索引。**不挂在 buffer.Line 上**（Line 不应该知道屏幕宽度等显示信息）。

但实际上 segment 边界（"这几行是表格"）不依赖屏幕宽度——只依赖 buffer 内容。所以理论上 segment 缓存可以挂在 Line 上。我们选择挂在 Registry 上是为了：
- 不污染 Micro 的 buffer 代码
- 失效逻辑集中在 Registry

### Q3：displayBufferMD 怎么和 BufWindow.Display 集成？

最简单的方式：

```go
// internal/display/bufwindow.go（修改后）
func (w *BufWindow) Display() {
    w.updateDisplayInfo()
    w.displayStatusLine()
    w.displayScrollBar()
    
    if w.shouldUseMDRender() {
        md.DisplayBufferMD(w, w.mdRegistry, w.mdMapper)
    } else {
        w.displayBuffer()
    }
}

func (w *BufWindow) shouldUseMDRender() bool {
    return w.Buf.Settings["mdrender"].(bool) && isMarkdownFile(w.Buf)
}
```

BufWindow 持有 mdRegistry 和 mdMapper 实例（每个 pane 一份，因为失效状态可能不同）。或者用一个全局的 mdService 管理（待定）。

### Q4：microOriginalRenderLine 怎么实现？

`displayBufferMD` 的 edit state 分支需要"用 Micro 原生方式渲染这一行"。最简单的方式是从 Micro 原版 `displayBuffer` 里把"逐 rune 渲染一个 buffer 行"那段代码抽出来作为一个独立函数：

```go
// 复用 Micro 原版的渲染逻辑
func microOriginalRenderLine(w *BufWindow, bufferLine int, cursors []*buffer.Cursor) []RenderCell {
    // 从原版 displayBuffer 的循环体里抽出来的"渲染一行"逻辑
    // 包括：tab 展开、宽字符、语法高亮（getStyle）、当前行高亮、brace matching
}
```

这是 displayBufferMD 实现里**最有技术含量**的一步，因为要理解 Micro 原版那段代码做了什么、能不能干净地抽出来。

### Q5：v0.1 阶段 displayBufferMD 的具体行数估计？

- 主循环骨架：~80 行
- 视口计算、gutter 处理（v0.1 简化）：~50 行
- microOriginalRenderLine（从原版复制改造）：~200 行
- 光标渲染：~50 行
- 配置开关、错误处理：~50 行

合计 ~400-500 行。加上 md/ 目录下的其他文件（Registry、Segment、Inline、Mapper 等），v0.1 总代码量约 **1000-1200 行**。

---

## 12. 总结

**核心思想**：

1. **重写而不是改装**——`.md` 文件用我们自己的 displayBufferMD
2. **保留 Micro 的核心机制**——buffer、语法高亮、screen、用户交互都不动
3. **按屏幕行遍历而不是按 buffer 行**——解锁 segment 多行输出的能力
4. **4 个模块平级，没有 Pipeline 中间层**——每个模块都有真实的、不可替代的职责
5. **探照灯模型**——光标驱动 segment edit state，没有全局模式状态机

**对 Lisa review 的回应**：

| Lisa 提的问题 | 新方案下的答案 |
|-------------|-------------|
| P0-1：displayBuffer 改造方式 | **重写**（方式 2） |
| P0-2：gutter 由谁负责 | displayBufferMD 自己决定（v0.1 不画） |
| P0-3：softwrap 策略 | displayBufferMD 自己控制（v0.1 禁用，v0.5+ 按需启用） |
| P0-4：性能基准 | 仍然需要 |
| O1：segment 缓存失效粒度 | 复用 `buffer.ModifiedThisFrame` 触发 Registry.Invalidate |
| O6：edit state fallback 实现 | microOriginalRenderLine（从 Micro 原版抽取） |

---

## 变更记录

| 日期 | 内容 |
|------|------|
| 2026-06-02 | V0 初稿：取代旧的 5 层架构（含 Pipeline）描述 |
