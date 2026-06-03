# Step 0 详细设计：架构骨架 + 背景色验证

> 阶段：Step 0（架构验证期）
> 依赖：架构设计V1.md 全部决策、PRD V2 第六章
> 目标：渲染管线全跑通，每个渲染模块用**专属背景色**可视化"我接管了哪些 buffer 行"
> 范围：**不**做实际内容渲染，只做架构验证
> 状态：待用户审阅

---

## 〇、Step 0 的本质

架构设计V1 §6.5 已经定义了 Step 0 的目标，本详细设计把每一行落到代码上。

**Step 0 = 渲染片机制**端到端跑通 + **每个渲染模块声明领地**（用背景色），交付物是一张能看出"谁负责渲染哪几行"的可视化地图。

如果这张地图对，Step 1~3 就是按地图逐格填色；如果不对，架构层面的问题在这个阶段就能发现，避免到 Step 2/3 才返工。

---

## 一、目录结构与文件骨架

### 1.1 新建文件清单

```
internal/display/
├── bufwindow.go              (改造: + IsMD 字段、+ displayBufferMD、+ Detect 缓存、Display() 加 if/else)
├── window.go                 (不变)
├── softwrap.go               (不变，Step 0 不动 Scroll/Diff)
├── statusline.go             (不变)
├── tabwindow.go              (不变)
├── termwindow.go             (不变)
├── infowindow.go             (不变)
├── uiwindow.go               (不变)
└── md/                       ★ 新建子包,所有 MD 渲染相关代码
    ├── md.go                 (公共类型:RenderSegment 接口、SegmentType 枚举、Cell 结构、元数据结构)
    ├── detect.go             (检测步骤:NewDetectorChain、bgColor、renderRawRange 等公共函数)
    ├── inline.go             (行内渲染器:粗体/斜体骨架,Step 0 仅占位)
    ├── config.go             (MDConfig 结构、NewMDConfig、isMarkdownFile、defer recover 降级)
    ├── table.go              (表格渲染器:简单识别表格起始行 + Render 输出背景色)
    ├── codeblock.go          (代码块渲染器:Type/Detect/Render + isFenceLine/findFencePair)
    ├── heading.go            (标题渲染器:Type/Detect/Render + isHeadingLine + level 解析)
    ├── list.go               (列表渲染器:Type/Detect/Render + isListItemLine + 有序无序识别)
    ├── link.go               (链接渲染器:Step 0 占位,Type 返回 SEG_PARAGRAPH,Detect 永远 nil)
    ├── blockquote.go         (引用块渲染器:Type/Detect/Render + isBlockQuoteLine)
    ├── hr.go                 (水平分割线渲染器:Type/Detect/Render + isHorizontalRule)
    ├── paragraph.go          (兜底段落渲染器:Type/Detect/Render)
    ├── detect_test.go        (检测步骤的单元测试:11 个 test + 1 个 benchmark)
    └── table_test.go         (表格 renderer 的单元测试:Step 0 内部识别逻辑的测试,docs/markdown_table_test.go 不迁入)
```

预估行数见下文「Step 0 全部新增/改动文件行数预估」表格。

**Step 0 全部新增/改动文件行数预估**(全量):

| 文件 | 类型 | 预估行数 |
|------|------|----------|
| `internal/display/bufwindow.go` | 改 | +250 |
| `internal/action/bufpane.go` | 改 | +30 |
| `internal/config/settings.go` | 改 | +15 |
| `internal/display/md/md.go` | 新 | +200 |
| `internal/display/md/detect.go` | 新 | +150 |
| `internal/display/md/inline.go` | 新 | +50 |
| `internal/display/md/config.go` | 新 | +100 |
| `internal/display/md/table.go` | 新 | +150 |
| `internal/display/md/codeblock.go` | 新 | +120 |
| `internal/display/md/heading.go` | 新 | +80 |
| `internal/display/md/list.go` | 新 | +100 |
| `internal/display/md/link.go` | 新 | +30 |
| `internal/display/md/blockquote.go` | 新 | +80 |
| `internal/display/md/hr.go` | 新 | +70 |
| `internal/display/md/paragraph.go` | 新 | +60 |
| `internal/display/md/detect_test.go` | 新 | +500 |
| `internal/display/md/table_test.go` | 新 | +50 |
| `docs/Step0-测试样本.md` | 新 | +40 |
| `docs/Step0-完成报告.md` | 新 | +50 |
| **新增合计** | | **+2125** |
| **删除合计** | | **−0** |
| **净变化** | | **+2125** |

> **行数估算依据**:
> - md/ 子包内每个 renderer 平均 60-150 行(SegmentType 实现 + Detect + Render + 1-2 个识别 helper)
> - 测试代码量与生产代码量比约 1:1(测试覆盖率目标 ≥80%)
> - 与架构设计V1 §6.2 修正估算"3000-3500 行生产 + 2500 行测试"中 Step 0 占比匹配(Step 0 约占 60% 生产代码)
> - **docs/markdown_table.go 迁入推迟到 Step 2**(Step 0 表格 renderer 只做简单识别,实际列宽计算等留 Step 2)

### 1.2 文件依赖关系

```
bufwindow.go
   └── md/config.go    (mdConfig 字段)
   └── md/md.go        (RenderSegment 接口、Cell 结构)
   └── md/detect.go    (DetectSegments, 接收 bufWidth + 可见行范围,返回 []Segment 边界)

md/detect.go
   └── md/md.go
   └── md/table.go     (Table detector,Step 0 用简单 ~30 行识别,完整 DetectTables 留 Step 2 迁入)
   └── md/codeblock.go (CodeBlock detector)
   └── md/heading.go   (HeadingLine detector)
   └── md/list.go      (ListItem detector)
   └── md/hr.go        (HorizontalRule detector)
   └── md/blockquote.go(BlockQuoteLine detector)
   └── md/link.go      (InlineLink detector,Step 0 暂不实现细节,只占位)

md/table.go
   └── md/md.go
   └── md/config.go    (读 w.mdConfig.MdTableAlign 等)

md/paragraph.go
   └── md/md.go
   └── md/config.go
   └── md/inline.go    (行内渲染器,Step 0 占位)
```

**关键约束**:
- `md/` 子包**不** import `internal/buffer`、`internal/config`、`internal/action` 中的包,只 import 标准库 + tcell + runewidth
- 配置通过 `BufWindow` 上的 `mdConfig` 结构传入,而不是直接读 `config.GetGlobalOption` / `Buf.Settings`
- 这保证 `action → display → buffer` 单向依赖关系不被破坏

### 1.3 docs/markdown_table.go 暂不迁入

`docs/markdown_table.go` / `docs/markdown_table_test.go` **Step 0 保持原状**,不迁入 `internal/display/md/`。

**理由**:
- Step 0 的表格 renderer 只做"识别表格起点 + 填背景色",**不需要** `DetectTables` 里的完整列宽计算、跨表格追踪、代码块避让等复杂逻辑
- `docs/markdown_table.go` 迁入是与 `internal/display/md/` 产生 import 关系的重动作,Step 0 阶段避免重动作
- 迁入的合适时机是 **Step 2**(表格实际渲染时)。Step 2 需要列宽计算时,一次性迁入 + 适配为 `RenderSegment` 接口
- Step 0 期间该文件仍是独立的 `display` 测试包,不链接进主二进制(原状)

**Step 0 的表格检测仅用 ~30 行简单逻辑**:
- 扫描可见区域,识别以 `|` 开头或包含 `|` 的行
- 从第一行 `|` 开始,往下找到连续多行的表块(中间不能有连续空行)
- 实际不区分表头/分隔/数据行,只粗略返回 [BufStart, BufEnd]

完整迁入留 Step 2,详见 §十二.3。

### 1.4 与 SoftWrap 接口的关系

Step 0 **不**动 `softwrap.go`。`displayBufferMD()` 在检测阶段输出"行高",但 Scroll/Diff 仍按 `softwrap` 行为计算。**首帧缓存未命中**(Step 0 的 BufPane 刚创建)按 buffer 行 == screen 行退化为原生行为,第二帧起使用真实检测结果——这个退化方案在 Step 0 早期不存在问题(Scroll 几乎不会被调用),Step 3 滚动优化再做。

详细策略:在 §6.1 详述。

---

## 二、配置项设计

### 2.1 配置项清单

架构设计V1 §5.7 列出 8 个配置项,Step 0 全部注册到 `internal/config/settings.go`,但仅注册默认值,**不**做"配置变更时实时生效"逻辑(Step 0 不需要,后期每 Step 按需补)。

| 配置项 | 类型 | 默认值 | 范围 | 注册位置 |
|--------|------|--------|------|----------|
| `mdrender` | bool | true | 全局 | `DefaultGlobalOnlySettings` |
| `mdrenderidle` | float64 | 10 | 全局 | `DefaultGlobalOnlySettings` |
| `mdtablealign` | bool | true | 公共(可 buffer 本地) | `defaultCommonSettings` |
| `mdtableborder` | bool | false | 公共(可 buffer 本地) | `defaultCommonSettings` |
| `mdbolditalic` | bool | true | 公共(可 buffer 本地) | `defaultCommonSettings` |
| `mdcodeblock` | bool | true | 公共(可 buffer 本地) | `defaultCommonSettings` |
| `mdheading` | bool | true | 公共(可 buffer 本地) | `defaultCommonSettings` |
| `mdlist` | bool | true | 公共(可 buffer 本地) | `defaultCommonSettings` |
| `mdlink` | bool | true | 公共(可 buffer 本地) | `defaultCommonSettings` |

Step 0 内**只用**:
- `mdrender`(决定是否走 displayBufferMD)
- 8 个模块开关的**读取路径**(决定各 renderer 是否被注册到检测器)

### 2.2 settings.go 改动

在 `internal/config/settings.go` 的 `defaultCommonSettings` 末尾追加:

```go
// --- MicroNeo: MD 渲染模块开关 ---
"mdtablealign":      true,
"mdtableborder":     false,
"mdbolditalic":      true,
"mdcodeblock":       true,
"mdheading":         true,
"mdlist":            true,
"mdlink":            true,
```

在 `DefaultGlobalOnlySettings` 末尾追加:

```go
// --- MicroNeo: 全局开关 ---
"mdrender":          true,
"mdrenderidle":      float64(10),
```

**注意**:在 `defaultCommonSettings` 添加项时,必须确认该字段名不在 `LocalSettings` 黑名单(否则会被 ReloadSettings 覆盖)。`LocalSettings` 当前只有 `filetype`、`readonly` 两项,新增的 6 个 MD 开关安全。

**还需要的配置项**:
- 行号策略(架构设计V1 §5.3 推翻了 PRD 5.5,MD 文件显示行号):复用 softwrap 设置,无需新增配置
- 段落渲染器相关配置:Step 0 全部硬编码,Step 2/3 按需新增

### 2.3 配置传递路径(架构设计V1 §5.7 决策落实)

```
config.GetGlobalOption("mdrender")     ─┐
config.GetGlobalOption("mdrenderidle") ─┤
Buf.Settings["mdtablealign"]            │
Buf.Settings["mdtableborder"]           ├─→  BufPane 构造时一次性读取
Buf.Settings["mdbolditalic"]           │     塞到 BufWindow.mdConfig
Buf.Settings["mdcodeblock"]            │     ───────────────────→
Buf.Settings["mdheading"]              │     internal/display/md/config.go
Buf.Settings["mdlist"]                 │     定义 struct MDConfig
Buf.Settings["mdlink"]                 │     持有这些字段
                                       ─┘
```

具体时机:在 `NewBufPaneFromBuf`(`internal/action/bufpane.go:280`)中,创建 `BufWindow` 之后、`newBufPane` 之前,读取配置并塞入。

```go
// internal/action/bufpane.go - 伪代码
func NewBufPaneFromBuf(buf *buffer.Buffer, tab *Tab) *BufPane {
    w := display.NewBufWindow(0, 0, 0, 0, buf)
    w.IsMD = isMarkdownFile(buf.Path)
    w.MDConfig = display.NewMDConfig(
        config.GetGlobalOption("mdrender").(bool),
        config.GetGlobalOption("mdrenderidle").(float64),
        isMDField(buf.Settings, "mdtablealign"),
        // ... 其他 6 个字段
    )
    h := newBufPane(buf, w, tab)
    return h
}
```

`isMDField(b.Settings, name) bool` 是个小工具,封装 `b.Settings[name].(bool)` 的类型断言,避免在调用处铺一堆类型断言。

**架构不变量**(架构设计V1 §4 决策 1 强化):
- `BufWindow.IsMD` 是**文件静态属性**(路径决定)→ 路径变化时才更新
- `BufWindow.MDConfig` 是**启动时快照**(创建 BufPane 时算一次)→ 用户改 `settings.json` 不立即生效,**符合现有 Micro 的行为**(改 settings.json 要重启)

### 2.4 OpenBuffer 时配置不变

`BufPane.OpenBuffer`(bufpane.go:355)切换 buffer 时,`h.BWindow.SetBuffer(b)` 重新绑定 buffer,但**不**改 `IsMD` / `MDConfig`——`OpenBuffer` 调用方是用户主动切 buffer,新 buffer 的 IsMD 仍由 `NewBufPaneFromBuf` 算一次。这是 Step 0 的简化,**留作 Step 1 详细设计**的 P2 问题:**SaveAs 改名后 IsMD 刷新**(问题清单 I9)。

Step 0 行为:SaveAs 改名后,旧 BufPane 的 IsMD 不变(因为是同一个 BufPane,只是 buffer 内容变)。这意味着 `.md` 改名成 `.txt` 后,BufPane 仍走 displayBufferMD。**可接受**:SaveAs 是罕见操作,Step 0 暂不处理。

---

## 三、BufWindow 改造

### 3.1 新增字段

```go
// internal/display/bufwindow.go
type BufWindow struct {
    *View

    Buf *buffer.Buffer
    // ... 现有字段

    // --- MicroNeo ---
    IsMD     bool         // 是否为 Markdown 文件(由 BufPane 在构造时设置)
    MDConfig MDConfigRef  // MD 渲染配置(由 BufPane 在构造时塞入,Step 0 仅此一次)
    // 检测步骤的轻量元数据缓存(每帧由 displayBufferMD 刷新,供 Scroll/Diff 使用)
    // Step 0:此字段不参与 Scroll/Diff 计算,只为"未来 Scroll/Diff 改造"占位
    lastDetection *md.DetectionResult
}
```

`MDConfigRef` 是 `*internal/display/md.MDConfig` 的别名(在 `md` 包内定义),`bufwindow.go` 通过 `md` 包 import 它。`*` 而非值类型,避免 bufwindow.go 必须导入整个 md 包(虽然它本来就要导入)。

### 3.2 Display() 改造

```go
// internal/display/bufwindow.go
func (w *BufWindow) Display() {
    w.updateDisplayInfo()

    w.displayStatusLine()
    w.displayScrollBar()

    if w.IsMD && w.MDConfig.RenderEnabled {
        w.displayBufferMD()
    } else {
        w.displayBuffer()
    }
}
```

`w.MDConfig.RenderEnabled` 即 `mdrender` 的值。两层判断:
- `w.IsMD` 排除非 MD 文件(从不过 displayBufferMD,零开销)
- `w.MDConfig.RenderEnabled` 允许用户**关掉** MD 渲染回退到原生(架构设计V1 §5.7)

`mdrender=false` 的用户仍享受 softwrap、语法高亮等所有 Micro 原生能力,只损失 MD 渲染。

### 3.3 SetBuffer 改造(为 I9 留接口)

```go
func (w *BufWindow) SetBuffer(b *buffer.Buffer) {
    w.Buf = b
    // ... 现有 OptionCallback 逻辑

    // MicroNeo: buffer 切换时,IsMD 也应该刷新
    // Step 0:暂不实现(留 TODO)
    // Step 1:在 OpenBuffer 调用前由调用方更新 w.IsMD
}
```

---

## 四、BufPane 改造(最小集)

### 4.1 配置读取与 IsMD 标记

在 `NewBufPaneFromBuf`(`bufpane.go:280`)中追加两行(见 §2.3)。

新增辅助函数:

```go
// internal/action/bufpane.go
func isMarkdownFile(path string) bool {
    if path == "" {
        return false
    }
    ext := strings.ToLower(filepath.Ext(path))
    return ext == ".md" || ext == ".markdown"
}

func isMDField(settings map[string]any, name string) bool {
    if v, ok := settings[name]; ok {
        if b, ok := v.(bool); ok {
            return b
        }
    }
    return false  // 不存在或类型错误时默认为 false(安全降级)
}
```

`isMDField` 在 `md` 包不存在时不会 panic,降级到 false——这是 §六.3 的"安全降级"策略的具体实现。

### 4.2 不改的部分

- `editMode` 字段(架构设计V1 决策 2):**Step 0 不加**。Step 1 才加,现在加是空字段
- `HandleEvent` 拦截逻辑:Step 0 不动
- 10 秒计时器:Step 1 才有
- OptionCallback:Step 0 不订阅 MD 相关配置的变更

理由:Step 0 目标"打开 md 文件看到每个结构有背景色",**不依赖**任何交互模式,纯渲染管线验证。

### 4.3 文件级 IsMD 不变动的兜底

若用户在 `NewBufPaneFromBuf` 调用前未设置 `IsMD`(`false` 默认),displayBufferMD 不会被调用,程序走原 displayBuffer 路径,完全无侵入。

---

## 五、md/ 子包核心数据结构

### 5.1 SegmentType 枚举

```go
// internal/display/md/md.go
package md

// SegmentType 标识一个渲染片的类型,用于:
//   1. 决定该片由哪个 renderer 负责输出
//   2. 调试和诊断(日志/Stats)
//   3. 模式切换时回退策略(Step 1 详细设计使用)
type SegmentType int

const (
    SEG_PARAGRAPH   SegmentType = iota // 兜底段落
    SEG_HEADING                         // 标题
    SEG_TABLE                           // 表格
    SEG_CODEBLOCK                       // 代码块
    SEG_LIST_ITEM                       // 列表项
    SEG_BLOCKQUOTE                      // 引用块
    SEG_HR                              // 水平分割线
    // SEG_LINK 是行内概念,不存在"整片都是链接"的情况
    // 链接检测在行内渲染器(inline.go)内做
)

// String 用于调试
func (t SegmentType) String() string {
    switch t {
    case SEG_PARAGRAPH:
        return "PARAGRAPH"
    case SEG_HEADING:
        return "HEADING"
    case SEG_TABLE:
        return "TABLE"
    case SEG_CODEBLOCK:
        return "CODEBLOCK"
    case SEG_LIST_ITEM:
        return "LIST_ITEM"
    case SEG_BLOCKQUOTE:
        return "BLOCKQUOTE"
    case SEG_HR:
        return "HR"
    }
    return "UNKNOWN"
}
```

**设计要点**:
- `SEG_PARAGRAPH` 是 iota 的 0 值,作为"未识别"的默认值
- 不给 `SEG_LINK`——链接是行内概念,不是片
- 暂不导出"未渲染/原始"枚举:Step 0 所有 buffer 行都被某个片覆盖,即使是最普通的段落也是 SEG_PARAGRAPH

### 5.2 RenderSegment 接口

```go
// internal/display/md/md.go
package md

import (
    "github.com/gdamore/tcell/v2"
    "github.com/micro-editor/micro/v2/internal/buffer"
)

// RenderSegment 是所有渲染片的统一接口
//
// 渲染片的两阶段:
//   1. Detect(bufWidth, lines ...) → *SegmentMeta
//      轻量,回答"这片占多少 screen row"
//   2. Render(ctx *RenderContext) → []*ScreenRow
//      重活,逐字符写入 screen
type RenderSegment interface {
    // Type 返回该 renderer 处理的片段类型
    Type() SegmentType

    // Detect 在给定的 bufWidth 下,扫描 buffer 行,判断该 renderer 是否能接管
    // 如果能接管,返回非 nil *SegmentMeta,内含 bufStartLine/bufEndLine/rowCount
    // 如果不能接管(不属于该 renderer 的领地),返回 nil
    //
    // input:
    //   buf            - 缓冲区(只读,用于按行访问 LineBytes)
    //   startLine      - 检测起点(通常是屏幕可见首行对应的 buffer 行)
    //   endLine        - 检测终点(通常是屏幕可见末行 + 一些 lookahead)
    //   bufWidth       - 当前 buffer 显示宽度(字符数,不含 gutter)
    //   alreadyInSegment bool - 是否已在该 renderer 的某个片中间(用于跨段连续)
    //
    // output:
    //   *SegmentMeta - 包含 buffer 行的起止、screen 行数
    //   nextStart    - 下次 Detect 应从哪一行开始(实现可自定义扫描推进策略)
    Detect(buf *buffer.Buffer, startLine, endLine, bufWidth int, alreadyInSegment bool) (meta *SegmentMeta, nextStart int)

    // Render 把渲染片输出到 screen
    // ctx 提供必要的渲染上下文(屏幕坐标、bufWidth、style 等)
    // 返回的 ScreenRow 列表由调用方(displayBufferMD)写入 screen
    //
    // Step 0:所有 Render 实现都返回 1 个或多个 "整行填充背景色" 的 ScreenRow
    Render(ctx *RenderContext) []*ScreenRow
}
```

### 5.3 元数据结构

```go
// internal/display/md/md.go

// SegmentMeta 是 Detect 步骤的输出
// 不含具体字符内容,只回答"片边界 + 行高"
type SegmentMeta struct {
    Type     SegmentType
    BufStart int    // 对应 buffer 起始行(0-based,含)
    BufEnd   int    // 对应 buffer 结束行(0-based,含)
    RowCount int    // 在当前 bufWidth 下,渲染后占多少个 screen row
    // 扩展字段(Step 2/3 才有):WordWrap、ColumnWidths 等
}

// DetectionResult 是 displayBufferMD() 第一步的完整输出
// 存到 BufWindow.lastDetection 供 Scroll/Diff 查表(Step 0 不消费,Step 3 才用)
type DetectionResult struct {
    Segments []SegmentMeta
    // Map: buffer line Y → SegmentMeta 索引
    // O(1) 查询:某 buffer 行属于哪个片
    LineToSeg map[int]int
    // Map: buffer line Y → 该 buffer 行在片内的第几行
    // O(1) 查询:某 buffer 行在片内是第几行
    // (用于渲染片内定位、行号显示)
    LineOffsetInSeg map[int]int
}
```

### 5.4 输出 Cell 结构(供 Render 用)

```go
// internal/display/md/md.go

// Cell 是渲染输出的最小单位
// Step 0:绝大多数 Cell 是 ' ' + 背景色(用于填色)
type Cell struct {
    Rune  rune
    Style tcell.Style
}

// ScreenRow 是 1 个 screen row 的所有 Cell
// 长度 = bufWidth
type ScreenRow struct {
    Cells []Cell
    // 该 screen row 对应的 buffer 行号
    // -1 表示装饰行(表格外框、代码块边框等),无对应 buffer 行
    // Step 0:不细分,统一填 0 或 buffer 行号都行
    BufLine int
}

// RenderContext 是 Render 步骤的输入
type RenderContext struct {
    Buf        *buffer.Buffer
    BufWidth   int
    GutterOffset int
    WindowX    int  // screen 起点 X
    WindowY    int  // screen 起点 Y
    SegMeta    SegmentMeta
    // Step 1 才用:EditMode、CursorPos 等
    MDConfig   *MDConfig
}
```

### 5.5 配置结构

```go
// internal/display/md/config.go
package md

// MDConfig 是所有 MD 相关配置的快照
// 由 BufPane 在构造时填一次,BufWindow 持有引用
type MDConfig struct {
    RenderEnabled bool    // mdrender
    IdleSeconds   float64 // mdrenderidle(Step 1 用)

    TableAlign   bool // mdtablealign
    TableBorder  bool // mdtableborder(Step 0 不消费,只读)
    BoldItalic   bool // mdbolditalic(Step 0 不消费)
    CodeBlock    bool // mdcodeblock
    Heading      bool // mdheading
    List         bool // mdlist
    Link         bool // mdlink(Step 0 不消费)
}

// NewMDConfig 由 BufPane 调用,从 config + buffer 读所有字段
func NewMDConfig(renderEnabled bool, idle float64, b *buffer.Buffer) *MDConfig {
    return &MDConfig{
        RenderEnabled: renderEnabled,
        IdleSeconds:   idle,
        TableAlign:    b.Settings["mdtablealign"].(bool),
        TableBorder:   b.Settings["mdtableborder"].(bool),
        BoldItalic:    b.Settings["mdbolditalic"].(bool),
        CodeBlock:     b.Settings["mdcodeblock"].(bool),
        Heading:       b.Settings["mdheading"].(bool),
        List:          b.Settings["mdlist"].(bool),
        Link:          b.Settings["mdlink"].(bool),
    }
}
```

**配置传递方式**:Step 0 简单实现,直接 `b.Settings["xxx"].(bool)`。如果类型断言失败会 panic,**这是已知的 panic 源**——§六.3 详述处理策略。

### 5.6 检测器工厂

```go
// internal/display/md/detect.go
package md

// NewDetectorChain 根据 MDConfig 构造检测器链
// MDConfig 中关闭的模块不参与检测(节省一点 CPU,Step 0 可忽略)
func NewDetectorChain(cfg *MDConfig) []RenderSegment {
    chain := []RenderSegment{}
    if cfg.CodeBlock {
        chain = append(chain, NewCodeBlockSegment())
    }
    if cfg.TableAlign {
        chain = append(chain, NewTableSegment())  // 注意:此名与 mdnames 重复,改用 NewTableRenderer
    }
    if cfg.Heading {
        chain = append(chain, NewHeadingSegment())
    }
    if cfg.List {
        chain = append(chain, NewListSegment())
    }
    if cfg.BlockQuote {  // 此项在 MDConfig 中尚未加,Step 0 默认 true
        chain = append(chain, NewBlockQuoteSegment())
    }
    // HR 检测独立实现(无需 cfg 开关)
    chain = append(chain, NewHRSegment())
    // 段落:兜底
    chain = append(chain, NewParagraphSegment())
    return chain
}
```

**检测器顺序**很关键(架构设计V1 §3.3 决策"每一行都属于某个片"):
1. CodeBlock 优先于 Table(代码块内的 `|---|` 不应被识别为表格)
2. Table 优先于 Heading/List(避免误识别)
3. HR 优先于 Heading(`---` 既是分割线也是可能的 Setext 标题下划线,Step 0 简化处理:看到 `---` 就当 HR)
4. Paragraph 兜底(最后)

### 5.7 各 renderer 骨架(关键代码)

每种 renderer 都有相同的"Detect + Render"模板,只是具体识别规则不同。下面给出**所有 8 种 renderer 的统一骨架**,Step 0 全部按这个模板实现。

#### 5.7.1 Table 渲染器

```go
// internal/display/md/table.go
package md

import (
    "github.com/micro-editor/micro/v2/internal/buffer"
    "github.com/micro-editor/tcell/v2"
)

type TableSegment struct{}

// NewTableRenderer 是工厂函数,避免和 RenderSegment 嵌入冲突
func NewTableSegment() *TableSegment { return &TableSegment{} }

func (s *TableSegment) Type() SegmentType { return SEG_TABLE }

func (s *TableSegment) Detect(buf *buffer.Buffer, startLine, endLine, bufWidth int, alreadyInSegment bool) (*SegmentMeta, int) {
    // Step 0:简单识别,只找连续包含 | 的行
    // 不区分表头/分隔/数据行,不计算列宽
    // 完整版(DetectTables + calcColWidths)从 docs/markdown_table.go 迁入留 Step 2
    for line := startLine; line <= endLine; line++ {
        bline := buf.LineBytes(line)
        if !looksLikeTableLine(bline) {
            continue
        }
        // 找到了表格首行,往下扫到表格结束
        end := line
        for end+1 <= endLine && looksLikeTableLine(buf.LineBytes(end+1)) {
            end++
        }
        return &SegmentMeta{
            Type:     SEG_TABLE,
            BufStart: line,
            BufEnd:   end,
            RowCount: end - line + 1, // Step 0 简化:1 行 buffer = 1 行 screen
        }, end + 1
    }
    return nil, startLine + 1
}

// looksLikeTableLine Step 0 简单判断:首字符是 | 或包含 2 个以上 |
func looksLikeTableLine(line []byte) bool {
    if len(line) == 0 {
        return false
    }
    // 跳过前导空格
    for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
        line = line[1:]
    }
    if len(line) == 0 {
        return false
    }
    if line[0] == '|' {
        return true
    }
    // 否则检查是否含 2+ 个 |
    count := 0
    for _, b := range line {
        if b == '|' {
            count++
        }
    }
    return count >= 2
}

func (s *TableSegment) Render(ctx *RenderContext) []*ScreenRow {
    // Step 0:渲染整片为浅黄色背景,文字用 buffer 原始内容(仅供识别)
    rows := []*ScreenRow{}
    bg := bgColor(ctx.MDConfig, SEG_TABLE)  // 统一的"取背景色"函数
    for bufY := ctx.SegMeta.BufStart; bufY <= ctx.SegMeta.BufEnd; bufY++ {
        row := &ScreenRow{
            BufLine: bufY,
            Cells:   make([]Cell, ctx.BufWidth),
        }
        line := ctx.Buf.LineBytes(bufY)
        x := 0
        for _, b := range line {
            if x >= ctx.BufWidth {
                break
            }
            r := rune(b)
            // Step 0:不解析 Markdown 标记,只把原始字符摆上去
            row.Cells[x] = Cell{Rune: r, Style: bg}
            x++
        }
        // 剩余位置填背景色空格
        for ; x < ctx.BufWidth; x++ {
            row.Cells[x] = Cell{Rune: ' ', Style: bg}
        }
        rows = append(rows, row)
    }
    return rows
}
```

`looksLikeTableLine` 是 Step 0 表格识别的唯一辅助函数,逻辑极简(不区分表头/分隔行,只看是否以 `|` 开头或含 ≥2 个 `|`)。Step 2 迁入 docs/markdown_table.go 后会被替换为 `DetectTables` + `calcColWidths` 的完整实现。

#### 5.7.2 CodeBlock 渲染器

```go
// internal/display/md/codeblock.go
type CodeBlockSegment struct{}

func (s *CodeBlockSegment) Type() SegmentType { return SEG_CODEBLOCK }

func (s *CodeBlockSegment) Detect(buf *buffer.Buffer, startLine, endLine, bufWidth int, alreadyInSegment bool) (*SegmentMeta, int) {
    // Step 0:简单实现——从 startLine 开始找 ``` 或 ~~~,找配对的
    for line := startLine; line <= endLine; line++ {
        if isFenceLine(buf.LineBytes(line)) {
            // 找配对 fence
            fenceChar := buf.LineBytes(line)[0]
            for end := line + 1; end <= endLine; end++ {
                if isFenceLineWithChar(buf.LineBytes(end), fenceChar) {
                    return &SegmentMeta{
                        Type:     SEG_CODEBLOCK,
                        BufStart: line,
                        BufEnd:   end,
                        RowCount: end - line + 1, // 简单估算,每行 1 个 screen row
                    }, end + 1
                }
            }
        }
    }
    return nil, startLine + 1
}

func (s *CodeBlockSegment) Render(ctx *RenderContext) []*ScreenRow {
    // Step 0:深灰色背景 + 原始文本
    bg := bgColor(ctx.MDConfig, SEG_CODEBLOCK)
    return renderRawRange(ctx, bg)  // 公共函数
}
```

#### 5.7.3 Heading 渲染器

```go
// internal/display/md/heading.go
type HeadingSegment struct{}

func (s *HeadingSegment) Detect(buf *buffer.Buffer, startLine, endLine, bufWidth int, alreadyInSegment bool) (*SegmentMeta, int) {
    // Step 0:识别以 # 开头的行(1~6 个 #)
    for line := startLine; line <= endLine; line++ {
        if isHeadingLine(buf.LineBytes(line)) {
            return &SegmentMeta{
                Type:     SEG_HEADING,
                BufStart: line,
                BufEnd:   line,
                RowCount: 1,
            }, line + 1
        }
    }
    return nil, endLine + 1
}

func isHeadingLine(line []byte) bool {
    if len(line) == 0 || line[0] != '#' {
        return false
    }
    count := 0
    for _, b := range line {
        if b == '#' {
            count++
        } else if b == ' ' && count >= 1 && count <= 6 {
            return true
        } else {
            return false
        }
    }
    return false
}
```

#### 5.7.4 List 渲染器

```go
// internal/display/md/list.go
type ListSegment struct{}

func (s *ListSegment) Detect(...) (*SegmentMeta, int) {
    // Step 0:简单实现——识别 `- `、`* `、`+ ` 或 `数字. ` 开头的行
    // 连续多行属于同一列表项的延伸(同缩进),都合并成 1 个片
    // Step 0 简化:每个列表项就是 1 行(不处理多行列表项)
    for line := startLine; line <= endLine; line++ {
        if isListItemLine(buf.LineBytes(line)) {
            return &SegmentMeta{
                Type:     SEG_LIST_ITEM,
                BufStart: line,
                BufEnd:   line,
                RowCount: 1,
            }, line + 1
        }
    }
    return nil, endLine + 1
}
```

#### 5.7.5 BlockQuote 渲染器

```go
// internal/display/md/blockquote.go
type BlockQuoteSegment struct{}

func (s *BlockQuoteSegment) Detect(...) (*SegmentMeta, int) {
    // 识别以 `> ` 开头的行(允许前导空格)
    for line := startLine; line <= endLine; line++ {
        if isBlockQuoteLine(buf.LineBytes(line)) {
            // Step 0:每个引用行单独成片(不处理连续引用行)
            return &SegmentMeta{
                Type:     SEG_BLOCKQUOTE,
                BufStart: line,
                BufEnd:   line,
                RowCount: 1,
            }, line + 1
        }
    }
    return nil, endLine + 1
}
```

#### 5.7.6 HR 渲染器

```go
// internal/display/md/hr.go
type HRSegment struct{}

func (s *HRSegment) Detect(...) (*SegmentMeta, int) {
    // 识别 3 个或以上连续的 `-`、`*`、`_`(可包含空格)
    for line := startLine; line <= endLine; line++ {
        if isHorizontalRule(buf.LineBytes(line)) {
            return &SegmentMeta{
                Type:     SEG_HR,
                BufStart: line,
                BufEnd:   line,
                RowCount: 1,
            }, line + 1
        }
    }
    return nil, endLine + 1
}

func isHorizontalRule(line []byte) bool {
    if len(line) == 0 {
        return false
    }
    // 简化:要求 3+ 个相同字符
    if line[0] != '-' && line[0] != '*' && line[0] != '_' {
        return false
    }
    count := 0
    for _, b := range line {
        if b == line[0] {
            count++
        } else if b != ' ' {
            return false
        }
    }
    return count >= 3
}
```

#### 5.7.7 Paragraph 渲染器(兜底)

```go
// internal/display/md/paragraph.go
type ParagraphSegment struct{}

func (s *ParagraphSegment) Detect(buf *buffer.Buffer, startLine, endLine, bufWidth int, alreadyInSegment bool) (*SegmentMeta, int) {
    // 段落渲染器是兜底:从 startLine 开始,直接接管所有剩余行
    // 但要避免和已经在其他片内的行冲突
    // 调用方保证 Paragraph 是检测链最后一环,且 startLine 不在已识别的片内
    if startLine > endLine {
        return nil, startLine + 1
    }
    return &SegmentMeta{
        Type:     SEG_PARAGRAPH,
        BufStart: startLine,
        BufEnd:   endLine,
        RowCount: endLine - startLine + 1, // Step 0:每行 1 个 screen row
    }, endLine + 1
}

func (s *ParagraphSegment) Render(ctx *RenderContext) []*ScreenRow {
    bg := bgColor(ctx.MDConfig, SEG_PARAGRAPH)
    return renderRawRange(ctx, bg)
}
```

#### 5.7.8 Link 渲染器(Step 0 暂时无独立片)

架构设计V1 §3.4 已明确:链接是行内概念,不构成单独的片。

Step 0:
- `internal/display/md/link.go` 创建但**只放占位**:`Type()` 返回 `SEG_PARAGRAPH`(链接所在行就是段落)
- `Detect` 永远返回 nil
- 不在 `NewDetectorChain` 中注册
- Step 2 由 inline 渲染器负责处理

**这是关键决策**:link renderer 留接口,不在 Step 0 链路中占位。

### 5.8 公共工具函数

```go
// internal/display/md/detect.go (公共部分)

// bgColor 给出每种 SegmentType 的背景色
// Step 0:硬编码 8 种颜色,Step 1+ 改为从 colorscheme 读
func bgColor(cfg *MDConfig, t SegmentType) tcell.Style {
    var bgColorName string
    switch t {
    case SEG_TABLE:
        bgColorName = "yellow"  // 浅黄
    case SEG_CODEBLOCK:
        bgColorName = "darkgray"  // 深灰
    case SEG_HEADING:
        bgColorName = "blue"  // 浅蓝
    case SEG_LIST_ITEM:
        bgColorName = "green"  // 浅绿
    case SEG_BLOCKQUOTE:
        bgColorName = "magenta"  // 浅紫
    case SEG_HR:
        bgColorName = "red"  // 浅红
    case SEG_PARAGRAPH:
        bgColorName = "default"  // 无背景
    }
    s, _ := config.GetColor(bgColorName)
    return s
}

// renderRawRange 把 buffer 的某段行原样输出(每行用 bg 填充)
// 公共函数,CodeBlock/Paragraph/Table 等都复用
func renderRawRange(ctx *RenderContext, bg tcell.Style) []*ScreenRow {
    rows := []*ScreenRow{}
    for bufY := ctx.SegMeta.BufStart; bufY <= ctx.SegMeta.BufEnd; bufY++ {
        row := &ScreenRow{
            BufLine: bufY,
            Cells:   make([]Cell, ctx.BufWidth),
        }
        line := ctx.Buf.LineBytes(bufY)
        x := 0
        for _, b := range line {
            if x >= ctx.BufWidth {
                break
            }
            r := rune(b)
            // Step 0:不解析 Markdown 标记
            // Step 1 之后:这里调用 inline renderer 处理粗体斜体
            row.Cells[x] = Cell{Rune: r, Style: bg}
            x++
            // 简单处理:CJK 等宽字符占 2 列
            // Step 0:暂不处理(测试用例都是英文)
        }
        for ; x < ctx.BufWidth; x++ {
            row.Cells[x] = Cell{Rune: ' ', Style: bg}
        }
        rows = append(rows, row)
    }
    return rows
}
```

**问题清单 M7 落地**:Step 0 的 colorscheme 关键词是临时硬编码,Step 2 改为从 `config.Colorscheme` 读,定义正式关键词("md-table-bg"、"md-codeblock-bg" 等)。

**问题清单 M6 落地**:CJK 字符宽度 Step 0 不处理,Step 2 在 inline renderer 内处理(表格 cell 内部换行那时再考虑)。

---

## 六、displayBufferMD() 主体

### 6.1 函数结构

```go
// internal/display/bufwindow.go
func (w *BufWindow) displayBufferMD() {
    if w.Height <= 0 || w.Width <= 0 {
        return
    }

    // === 第 1 步:检测(轻量)===
    detection := w.detectSegments()
    w.lastDetection = detection

    // === 第 2 步:渲染(重活)===
    for _, seg := range detection.Segments {
        if seg.BufEnd < w.StartLine.Line || seg.BufStart > w.StartLine.Line+w.bufHeight-1 {
            continue  // 不可见,跳过
        }
        // 找到该 seg 对应的 renderer
        renderer := w.findRenderer(seg.Type)
        if renderer == nil {
            continue  // 不应发生(检测器都来自同一个链)
        }
        // 计算该 seg 在屏幕上的位置
        ctx := w.buildRenderContext(seg)
        rows := renderer.Render(ctx)
        // 写入 screen
        for i, row := range rows {
            for x, cell := range row.Cells {
                screen.SetContent(ctx.WindowX+x, ctx.WindowY+i, cell.Rune, nil, cell.Style)
            }
        }
    }

    // === 第 3 步:行号区(架构设计V1 §5.3b 决策)===
    // 复用 softwrap 多行逻辑:首行显示,续行留空
    w.drawGutterForMD(detection)

    // === 第 4 步:滚动条 + 状态栏(沿用 displayBuffer)===
    // 已在 Display() 中调用,不在 displayBufferMD 重复
}
```

**Step 0 简化**:
- `findRenderer` 通过 `SegmentType` 查表
- `buildRenderContext` 填入 WindowX/WindowY(从 `w.X + w.gutterOffset`、`w.Y` 算)
- 写 screen 用 `screen.SetContent`,和 `displayBuffer` 一致

**Step 0 不做**:
- 行号区:Step 0 不画行号(简化),Step 1 加上
- 滚动条:复用 `displayScrollBar`(已独立)
- 状态栏:复用 `displayStatusLine`(已独立)
- 鼠标 click 定位:Step 0 不实现

### 6.2 检测步骤

```go
// internal/display/bufwindow.go
func (w *BufWindow) detectSegments() *md.DetectionResult {
    buf := w.Buf
    bufWidth := w.bufWidth
    totalLines := buf.LinesNum()

    // Step 0:扫描范围 = 可见行 + 一些 lookahead
    // visible range: StartLine.Line ~ StartLine.Line + bufHeight - 1
    // lookahead: + 10 行(表格可能跨多行)
    startLine := w.StartLine.Line
    if startLine < 0 {
        startLine = 0
    }
    endLine := startLine + w.bufHeight + 10  // +10 lookahead
    if endLine >= totalLines {
        endLine = totalLines - 1
    }

    // Step 0:每次都重建 chain(配置变更时由 BufPane 通知;Step 0 暂不实现)
    // 简化:每次都从全局读
    chain := md.NewDetectorChain(w.MDConfig)

    // 串行扫描
    var segments []md.SegmentMeta
    lineToSeg := make(map[int]int)
    lineOffsetInSeg := make(map[int]int)

    cursor := startLine
    for cursor <= endLine {
        matched := false
        for _, det := range chain {
            meta, next := det.Detect(buf, cursor, endLine, bufWidth, false)
            if meta != nil {
                segments = append(segments, *meta)
                // 填充 lineToSeg 和 lineOffsetInSeg
                idx := len(segments) - 1
                for i := meta.BufStart; i <= meta.BufEnd; i++ {
                    lineToSeg[i] = idx
                    lineOffsetInSeg[i] = i - meta.BufStart
                }
                cursor = next
                matched = true
                break
            }
        }
        if !matched {
            // 不应发生(Paragraph 兜底总会命中),但保险起见
            // 跳过 1 行,避免死循环
            cursor++
        }
    }

    return &md.DetectionResult{
        Segments:        segments,
        LineToSeg:       lineToSeg,
        LineOffsetInSeg: lineOffsetInSeg,
    }
}
```

**关键设计**:
- `cursor` 串行推进,每个 detector 命中后跳到 `nextStart`,未命中则 1 行 1 行扫描
- `chain` 是按顺序的检测器链,Paragraph 兜底保证总有 1 个命中
- `LineToSeg` 和 `LineOffsetInSeg` 是 O(1) 查询表,Step 3 Scroll/Diff 改造时用

### 6.3 渲染上下文构建

```go
func (w *BufWindow) buildRenderContext(seg md.SegmentMeta) *md.RenderContext {
    return &md.RenderContext{
        Buf:         w.Buf,
        BufWidth:    w.bufWidth,
        GutterOffset: w.gutterOffset,
        WindowX:     w.X + w.gutterOffset,
        WindowY:     w.Y,  // Step 0 简化:从窗口顶部开始,不考虑 seg 的屏幕 Y 偏移
        SegMeta:     seg,
        MDConfig:    w.MDConfig,
    }
}
```

**Step 0 简化**:`WindowY` 总是 `w.Y`,即所有 seg 从窗口顶部开始绘制。**这意味着 Step 0 的多 seg 渲染是错的——会重叠**。

但 Step 0 关心的是"每种结构用不同背景色",可见区域通常只显示 1 个 seg(或几个连续的同类型 seg),重叠不严重。

**Step 0 的妥协**:为了早期能看到背景色效果,接受重叠。等 Scroll/Diff 改造(Step 3)时再修"seg 的准确屏幕 Y 计算"。

详细取舍见 §六.4。

### 6.4 seg 的屏幕 Y 偏移(Step 0 简化方案)

**问题**:可见区域可能包含多个 seg,每个 seg 应该在屏幕的不同行,而不是都从窗口顶部开始。

**Step 0 简化**:
- 仅支持"可见区域只有 1 个 seg"或"多个相同类型的 seg"
- 多个不同类型的 seg 出现时,按 detection.Segments 顺序,从窗口顶部往下叠(可能有视觉错位)
- 用户在 Step 0 主要看"背景色是否对",不在意准确位置

**正确方案(Step 1 详细设计)**:
- 计算 `StartLine.Line` 在 StartLine 哪个 seg 内
- 该 seg 的 BufStart 距离 StartLine 有 N 行
- N 行对应的 screen row 数 = ∑ (前面所有 seg 的行高)
- 起始 WindowY = w.Y - N 行的视觉偏移

**Step 0 暂不实现**:Step 0 用户感知不强,先确认 detection 和 renderer 链正确。

---

## 七、背景色方案详述

### 7.1 颜色表

| SegmentType | 背景色(tcell 颜色名) | 颜色含义 |
|-------------|---------------------|----------|
| SEG_TABLE | `color:yellow` | 表格专属(浅黄) |
| SEG_CODEBLOCK | `color:darkgray` | 代码块(深灰) |
| SEG_HEADING | `color:blue` | 标题(浅蓝) |
| SEG_LIST_ITEM | `color:green` | 列表项(浅绿) |
| SEG_BLOCKQUOTE | `color:magenta` | 引用(浅紫) |
| SEG_HR | `color:red` | 分割线(浅红) |
| SEG_PARAGRAPH | 默认(无背景) | 普通段落 |

通过 `config.GetColor(name)` 拿 tcell.Style(在 8 色终端自动降级)。

### 7.2 测试样本.md(Step 0 验收用)

```markdown
# 一级标题

这是普通段落。

## 二级标题

- 列表项 1
- 列表项 2
- 列表项 3

> 引用块
> 继续引用

```
这是代码块
跨多行
```

| 列1 | 列2 |
|-----|-----|
| 单元格1 | 单元格2 |
| 单元格3 | 单元格4 |

---

最后的段落。
```

预期视觉效果:7 种结构各有不同背景色,普通段落无背景。

### 7.3 行号区(Step 0 暂不画)

Step 0 简化:不画行号、不画 diff gutter、不画 statusline(micro 原 statusline 仍画,因为它在 displayBuffer 路径之外的 displayStatusLine)。

Step 1 在 `displayBufferMD` 末尾补 `drawGutterForMD`。

---

## 八、安全降级与 Panic Fallback(问题清单 M5)

### 8.1 已知 panic 源

1. `config.GetGlobalOption("mdrender")` 返回 nil(用户在 settings.json 写了未注册的配置,但 mdrender 不太可能)
2. `b.Settings["mdtablealign"]` 字段不存在(版本不一致)
3. 类型断言失败(mdrenderidle 应该是 float64,但用户写了字符串)
4. renderer.Render() 内 bug 导致 panic

### 8.2 降级策略

```go
// internal/display/md/config.go
func NewMDConfig(buf *buffer.Buffer) *MDConfig {
    cfg := &MDConfig{
        RenderEnabled: true,  // 默认开启
        IdleSeconds:   10.0,
        TableAlign:    true,
        TableBorder:   false,
        BoldItalic:    true,
        CodeBlock:     true,
        Heading:       true,
        List:          true,
        Link:          true,
    }
    defer func() {
        if r := recover(); r != nil {
            // 任意 panic → 全部用默认值(不渲染)
            cfg.RenderEnabled = false
        }
    }()
    if v := config.GetGlobalOption("mdrender"); v != nil {
        if b, ok := v.(bool); ok {
            cfg.RenderEnabled = b
        }
    }
    if v := config.GetGlobalOption("mdrenderidle"); v != nil {
        if f, ok := v.(float64); ok {
            cfg.IdleSeconds = f
        }
    }
    if v, ok := buf.Settings["mdtablealign"]; ok {
        if b, ok := v.(bool); ok {
            cfg.TableAlign = b
        }
    }
    // ... 其他字段同理
    return cfg
}
```

**核心策略**:构造 MDConfig 时套一层 defer recover(),任何配置读取失败 → 全部走默认(关闭渲染,回退到 Micro 原生)。

### 8.3 renderer.Render panic 隔离

```go
// internal/display/bufwindow.go - displayBufferMD 内
for i, row := range rows {
    for x, cell := range row.Cells {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    // 单个 cell 写入失败 → 用空格 + 默认 style 兜底
                    screen.SetContent(ctx.WindowX+x, ctx.WindowY+i, ' ', nil, config.DefStyle)
                }
            }()
            screen.SetContent(ctx.WindowX+x, ctx.WindowY+i, cell.Rune, nil, cell.Style)
        }()
    }
}
```

Step 0 简化:每个 `screen.SetContent` 套 defer recover。**过度防护**,但 Step 0 阶段安全第一。

### 8.4 detector panic 隔离

```go
// internal/display/bufwindow.go - detectSegments 内
for _, det := range chain {
    func() {
        defer func() {
            if r := recover(); r != nil {
                // 单个 detector panic → 跳过,继续下一个
                matched = false
            }
        }()
        meta, next := det.Detect(buf, cursor, endLine, bufWidth, false)
        if meta != nil {
            // ... 正常处理
        }
    }()
}
```

任何 detector 抛 panic → 视为"未匹配",继续下一个 detector,最后由 Paragraph 兜底。

---

## 九、测试策略(问题清单 M4)

### 9.1 单元测试(internal/display/md/detect_test.go)

针对 `detectSegments` 链,**不依赖 buffer 实例**,直接构造 mock buffer:

```go
// internal/display/md/detect_test.go
type mockBuffer struct {
    lines [][]byte
}

func (m *mockBuffer) LineBytes(y int) []byte { return m.lines[y] }
func (m *mockBuffer) LinesNum() int { return len(m.lines) }

// 实际:用 buffer.Buffer 内部结构
// 简化:仅测试 detector.Detect 单元
```

但 `buffer.Buffer` 的构造比较重,可以写一个"伪 buffer"接口:

```go
// internal/display/md/md.go
type BufferLineReader interface {
    LineBytes(y int) []byte
    LinesNum() int
}

// RenderSegment.Detect 改成接受 BufferLineReader
func (s *TableSegment) Detect(buf BufferLineReader, startLine, endLine, bufWidth int, ...) (...)
```

这样测试可以传 mock,不用拉起整个 buffer。

**但**这与 `buf *buffer.Buffer` 的实际签名不一致——需要一个 adapter:

```go
// internal/display/bufwindow.go
type bufferLineReaderAdapter struct{ b *buffer.Buffer }
func (a bufferLineReaderAdapter) LineBytes(y int) []byte { return a.b.LineBytes(y) }
func (a bufferLineReaderAdapter) LinesNum() int { return a.b.LinesNum() }
```

Step 0 实现这个 adapter,既不污染 `buffer.Buffer` 内部结构,又让 detector 单元可测。

### 9.2 测试用例清单

`internal/display/md/detect_test.go` 必须覆盖:

| 测试 | 输入 | 期望 |
|------|------|------|
| TestTableDetect | 3 行表格(2 数据行 + 分隔行) | 1 个 SEG_TABLE,BufStart=0,BufEnd=2 |
| TestTableInCodeBlock | 代码块内的 `|---|` | 0 个 SEG_TABLE(代码块优先) |
| TestCodeBlockDetect | 围栏代码块 | 1 个 SEG_CODEBLOCK,覆盖所有代码行 |
| TestHeadingDetect | 6 个 # 开头的行 | 6 个 SEG_HEADING |
| TestListDetect | 3 个 `- ` 开头的行 | 3 个 SEG_LIST_ITEM |
| TestHRDetect | `---` 单独行 | 1 个 SEG_HR |
| TestBlockQuoteDetect | 2 个 `> ` 开头行 | 2 个 SEG_BLOCKQUOTE(Step 0 简化) |
| TestMixedOrder | 混合文档(标题 + 段落 + 列表 + ...) | 按检测器顺序,所有结构被识别 |
| TestParagraphFallback | 1 个空行 | 0 个 SEG_PARAGRAPH(空 buffer 边界处理) |
| TestEmptyBuffer | 空 buffer | 0 个 segments |
| TestPanicRecovery | 故意让 detector panic | 跳过该 detector,继续下一个 |

### 9.3 table_test.go 的内容

`internal/display/md/table_test.go` 是 Step 0 为表格 renderer 写的**新测试**,不迁入 docs/markdown_table_test.go:
- 针对 `looksLikeTableLine` 简单识别逻辑的测试
- 1-2 个 test 即可(输入:包含 `|` 的行,输出:true/false)
- docs/markdown_table_test.go 保持原状继续在 docs/ 下跑

**Step 2 迁入 docs/markdown_table_test.go 时**,再把 12 个测试函数 + 51 个子测试迁入 `internal/display/md/table_test.go`,届时该文件会包含 Step 0 写的简单测试 + Step 2 迁入的完整测试。

### 9.4 集成测试(手工)

Step 0 不写自动化集成测试,只做:
1. 启动 micro 打开 `docs/Step0-测试样本.md`
2. 肉眼检查 7 种结构各有不同背景色
3. 切换到非 MD 文件(`.txt`)→ 走原生路径,无背景色
4. 在 settings.json 设 `mdrender: false` → 即使 MD 文件也走原生路径

**通过条件**:以上 4 项全部符合预期。

### 9.5 性能基准(问题清单 M4)

**Step 0 不做严格基准**。仅在 `internal/display/md/detect_test.go` 加一个 `BenchmarkDetectSegments` 占位:

```go
func BenchmarkDetectSegments(b *testing.B) {
    // 构造 1000 行的混合文档
    buf := makeMockBuffer(1000)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        detectSegmentsForBench(buf)
    }
}
```

Step 0 跑一次,记录基线数字(注释在测试文件里)。Step 1 优化后再跑,对数字。

---

## 十、Step 0 Definition of Done

完成下列全部项,Step 0 视为通过。

### 10.1 代码项

- [ ] `internal/display/md/` 目录创建,8 个 renderer 文件全部存在
- [ ] `internal/display/md/md.go` 定义 `SegmentType`、`RenderSegment`、`Cell`、`ScreenRow`、`RenderContext`、`MDConfig`
- [ ] `internal/display/md/detect.go` 实现 `NewDetectorChain`、`detectSegments`(在 bufwindow 内)、公共工具函数
- [ ] `internal/display/md/table.go` 实现简单识别(不迁 docs/markdown_table.go)
- [ ] 8 个 renderer 各自实现 `Type()` / `Detect()` / `Render()`,Render 输出用各自背景色填充
- [ ] `internal/display/bufwindow.go`:
  - [ ] 加 `IsMD bool` 字段
  - [ ] 加 `MDConfig *md.MDConfig` 字段
  - [ ] 加 `lastDetection *md.DetectionResult` 字段
  - [ ] `Display()` 加 `if w.IsMD && w.MDConfig.RenderEnabled { displayBufferMD() }` 分支
  - [ ] `displayBufferMD()` 实现两阶段(detect + render)
- [ ] `internal/action/bufpane.go`:
  - [ ] `NewBufPaneFromBuf` 中设 `w.IsMD = isMarkdownFile(buf.Path)` 和 `w.MDConfig = md.NewMDConfig(buf)`
  - [ ] `isMarkdownFile()` / `isMDField()` 辅助函数
- [ ] `internal/config/settings.go`:
  - [ ] 8 个新配置项注册到 `defaultCommonSettings` / `DefaultGlobalOnlySettings`
- [ ] `internal/display/md/config.go`:
  - [ ] `NewMDConfig` 实现,带 defer recover 降级
- [ ] **不删** `docs/markdown_table.go` / `docs/markdown_table_test.go`(Step 0 期间保持原状)

### 10.2 测试项

- [ ] `go test ./internal/display/md/...` 通过
- [ ] `go test ./...` 通过(其他包不受影响,docs/markdown_table_test.go 仍按原状跑通)
- [ ] `go build ./cmd/micro` 通过(主二进制编译成功)

### 10.3 行为项

- [ ] 打开 `docs/Step0-测试样本.md`,7 种结构各有不同背景色
- [ ] 打开 `.txt` 文件,无背景色(原生路径)
- [ ] `mdrender: false` 关闭后,`.md` 文件也无背景色(原生路径)
- [ ] 配置异常(mdrenderidle 写成字符串)时,程序不 panic,自动回退默认
- [ ] 单个 detector 抛 panic 时,其他 detector 继续工作,整体不崩

### 10.4 性能项

- [ ] `BenchmarkDetectSegments` 跑通,基线数字记录在 `detect_test.go` 注释
- [ ] 1000 行混合文档,detectSegments < 5ms(粗略目标,Step 1 重新评估)

### 10.5 文档项

- [ ] `docs/Step0-测试样本.md` 创建(包含 §7.2 的样本内容)
- [ ] `docs/Step0-完成报告.md` 简要记录完成情况、踩坑、Step 1 待办

---

## 十一、已知风险与显式不做项

### 11.1 Step 0 显式不做

| 项目 | 不做的原因 | 留给哪个 Step |
|------|------------|---------------|
| 粗体/斜体实际渲染 | Step 0 只做"看见领地",不做"美化文字" | Step 2 |
| 表格实际列对齐 | docs/markdown_table.go 是参考资料,Step 0 不迁入,Step 2 需要时再迁 | Step 2 |
| docs/markdown_table.go 迁入 internal/display/md/ | Step 0 表格 renderer 只做粗粒度识别,不需要 DetectTables | Step 2 |
| 链接 `[text](url)` 解析 | 行内渲染,Step 0 不实现 | Step 2 |
| 行号 + diff gutter | displayBufferMD 末尾的画法,Step 0 暂不画 | Step 1 |
| 装饰行(表格外框)click 定位 | 需要渲染片输出表,Step 0 渲染片输出表为空 | Step 3 |
| editMode / 计时器 | 模式系统,Step 0 不动 | Step 1 |
| Scroll/Diff 改造 | 决定依赖 detection 元数据,Step 0 元数据已存但不被消费 | Step 3 |
| Softwrap 与渲染片并存(editMode) | 模式系统未引入,不需要考虑 | Step 1 |
| SaveAs 后 IsMD 刷新(问题清单 I9) | 罕见操作,Step 0 接受行为退化 | Step 1 |
| CJK 字符宽度(问题清单 M6) | 测试用例都是英文,Step 0 不处理 | Step 2 |
| colorscheme 关键词正式定义(问题清单 M7) | Step 0 硬编码,Step 2 改造 | Step 2 |
| Go 接口嵌入的方法遮蔽(问题清单 I2) | 当前不存在方法名冲突,Step 0 不预判 | 出现问题时处理 |
| Go 嵌入字段的 `w.IsMD` vs `h.IsMD` 命名(问题清单 M9) | BufWindow.IsMD 已定,BufPane 不再加同名字段 | 文档润色 |
| BufPane.ResizePane 后的 IsMD 重新计算(未列入问题清单) | 同一 BufPane 不重新算 | Step 1 视情况 |
| 性能优化(增量检测、缓存) | Step 0 接受每帧全量重算 | Step 3 滚动优化 |

### 11.2 已识别风险

| 风险 | 触发场景 | 缓解 |
|------|----------|------|
| displayBufferMD 屏幕 Y 重叠 | 可见区域有多个不同类型 seg | Step 0 接受(用户主要看背景色),Step 1 修 |
| 检测器链性能 | 1000 行混合文档,每次 display 都全量扫描 | Step 0 接受(< 5ms),Step 3 优化 |
| Micro 上游合并 | 上游改 displayBuffer 时需重新 merge | 暂不处理,Step 0 期间不合并上游 |
| user 的 settings.json 配错(类型错误) | 任意 | defer recover 降级(见 §八) |
| renderer 在空 buffer 上调用 | buffer 刚创建,LinesNum() == 0 | detectSegments 提前 return,见 §6.2 |
| BufWindow 缩到 0 宽/高 | 终端缩小 | 沿用 displayBuffer 的 `if w.Height <= 0` 早返回 |

### 11.3 验收失败的"快速回退"

如果 Step 0 验证发现架构根本走不通,所有改动都可以**5 分钟内回退**:

```bash
# 回退 Step 0 改动
git revert <step0-commit>
# 此时:
#   - internal/display/md/ 目录被删
#   - internal/display/bufwindow.go 恢复
#   - internal/action/bufpane.go 恢复
#   - internal/config/settings.go 恢复
#   - docs/markdown_table.go / docs/markdown_table_test.go 原封未动(Step 0 未迁入)
```

回退后:程序完全等价于现在的 Micro 原版,无任何 MD 渲染能力。

---

## 十二、Step 0 之后的衔接

### 12.1 Step 0 交付物的可视化验证

完成后,打开 `docs/Step0-测试样本.md`,用户看到:

```
[浅黄背景]
| 列1 | 列2 |
|-----|-----|
| 单元格1 | 单元格2 |
| 单元格3 | 单元格4 |

[浅蓝背景]
# 一级标题

[浅蓝背景]
## 二级标题

[浅绿背景]
- 列表项 1
[浅绿背景]
- 列表项 2

[浅紫背景]
> 引用块
[浅紫背景]
> 继续引用

[深灰背景]
```
这是代码块
[深灰背景]
跨多行
[深灰背景]
```

[浅红背景]
---

[无背景]
最后的段落。
```

**这张图就是 Step 0 的全部价值**——一眼看出每个 buffer 行被哪个 renderer 接管。

### 12.2 Step 1 切入点

Step 0 完成后:
- `displayBufferMD` 已有 `detectSegments()` 框架
- `lastDetection` 已存
- `MDConfig` 已就位

Step 1 只需要:
1. BufPane 加 `editMode bool` 字段
2. BufPane `HandleEvent` 加 5 个方向键的拦截
3. BufPane 加 10 秒计时器
4. `displayBufferMD` 在第 2 步渲染前,加 `if editMode && cursorInSeg(seg) → 调原 displayBuffer 逻辑(copy 自 displayBuffer)`

### 12.3 Step 2 切入点

Step 2 把 `TableSegment.Render` 从"填背景色 + 原文本"升级到"列对齐 + 美观边框":

```go
func (s *TableSegment) Render(ctx *RenderContext) []*ScreenRow {
    // Step 0 实现
    return renderRawRange(ctx, bg)

    // Step 2 实现(逐步替换)
    // 1. 一次性迁入 docs/markdown_table.go 的 DetectTables / calcColWidths
    // 2. 适配为 RenderSegment 接口(把 DetectTables 包装为 Detect)
    // 3. 画外框
    // 4. 画 cell 内容(行内渲染器处理粗体斜体)
    // 5. 返回完整的 ScreenRow 列表
}
```

**Step 2 迁入 docs/markdown_table.go 的具体动作**:
- 把 `docs/markdown_table.go` 复制到 `internal/display/md/table.go` 顶部
- 调整 import 路径(去掉 `docs/` 的依赖,改为 `internal/display/md/`)
- `TableBlock` / `codeBlockRange` 类型迁入,作为 `table.go` 内部类型
- `DetectTables` 包装为 `TableSegment.Detect` 的实现
- 同步把 `docs/markdown_table_test.go` 迁入 `internal/display/md/table_test.go`
- 删掉 `docs/` 下两个文件

同样,Heading/List/CodeBlock/BlockQuote 各 renderer 在 Step 2 升级,Paragraph 留到所有 renderer 都做完再升级(架构设计V1 §6.5 阶段 4)。

---

## 十三、关键决策小结

| 决策 | 选择 | 原因 |
|------|------|------|
| 目录结构 | `internal/display/md/` 子包 | 避免污染 display 主包,作为未来 MicroNeo 内部模块的根 |
| 检测器链 vs 注册表 | 检测器链(顺序固定) | 保证 Paragraph 兜底,简化逻辑 |
| 配置传递 | MDConfig struct 一次性快照 | 避免 display 包 import config/buffer,符合依赖方向 |
| IsMD 判断 | 路径扩展名(`.md`、`.markdown`) | 简单可靠,符合"MicroNeo 只服务 MD"定位 |
| Step 0 渲染方式 | 填背景色,文字保留原始 buffer | 用户能立刻看到"我接管了哪些行" |
| 行号区 | Step 0 不画,Step 1 补 | 简化首次实现,Step 0 验收只看背景色 |
| 多 seg 屏幕 Y 重叠 | 接受,Step 1 修 | 用户感知不强,先验证 detection/render 链 |
| 链接 renderer | 留接口但 Step 0 不注册 | 行内概念,不是片 |
| 段落 renderer | 始终存在,作为兜底 | 保证 detection 链 100% 命中,无死循环 |
| CJK 字符 | Step 0 不处理 | 测试用例英文,Step 2 改 |
| 性能优化 | 每帧全量重算 + 检测步骤缓存 | Step 0 简化,Step 3 优化 |
| Panic 隔离 | defer recover 在 3 处(配置读取、detector 调用、cell 写入) | Step 0 安全第一 |
| 性能基准 | 只记录基线,不设硬性 SLA | 留给 Step 1 评估 |
| 回退策略 | 5 分钟内可 git revert 回退 | 验证失败的兜底 |

---

## 十四、Step 0 实施时间线(预估)

| 任务 | 关键文件 |
|------|----------|
| 1. 配置项注册 | `internal/config/settings.go` |
| 2. md/ 子包基础类型 | `md/md.go`、`md/config.go` |
| 3. Table renderer 迁移 | `md/table.go` + `md/table_test.go` |
| 4. CodeBlock renderer | `md/codeblock.go` |
| 5. Heading renderer | `md/heading.go` |
| 6. List renderer | `md/list.go` |
| 7. BlockQuote renderer | `md/blockquote.go` |
| 8. HR renderer | `md/hr.go` |
| 9. Paragraph renderer | `md/paragraph.go` |
| 10. detect.go + 检测链 | `md/detect.go` |
| 11. displayBufferMD 主体 | `bufwindow.go` |
| 12. BufPane 改造 | `bufpane.go` |
| 13. 单元测试补全 | `md/detect_test.go` |
| 14. 手工集成测试 + 修复 | — |

各任务预估行数见 §一.1 「Step 0 全部新增/改动文件行数预估」表格。

**预期工作量**:
- 1 名熟练 Go 开发者,3-4 个工作日完成
- 包括手工集成测试 0.5 天
- 包括修复 1-2 个集成问题 0.5 天

**总代码量验证**:与架构设计V1 §6.2 "3000-3500 行生产代码 + 2500 行测试代码"的修正估算匹配(Step 0 占约 60% 生产代码,40% 测试代码)。

---

## 十五、变更影响范围

变更哪些文件、预估行数、净变化,见「Step 0 全部新增/改动文件行数预估」表格(§一.1)。

---

## 十六、附录:Step 0 测试样本.md

(此文件实际由 §7.2 给出,放这里供实施时参考)

```markdown
# Step 0 验收测试

## 普通段落
这是普通段落,无背景色。

## 列表测试
- 列表项 1
- 列表项 2
  - 嵌套列表项(Step 0 不识别嵌套,作为普通段落处理)
- 列表项 3

## 引用测试
> 引用块第 1 行
> 引用块第 2 行

## 代码块测试
```
这是代码块
第 2 行
第 3 行
```

## 表格测试
| 列1 | 列2 | 列3 |
|-----|-----|-----|
| a   | b   | c   |
| d   | e   | f   |

## 分割线测试

---

## 收尾段落
文档结束。
```

预期:用户打开后,看见 7 种背景色区块(标题、列表、引用、代码块、表格、HR、段落)清晰区分。

---

## 十七、Step 0 完成 = 进入 Step 1 的标志

**架构可信**:
- 渲染片"检测 → 渲染"两步分离可工作
- 8 个 renderer 各自独立,新加 renderer 只需实现接口
- BufWindow.IsMD 触发条件正确
- 配置项传递路径单向、不污染依赖关系

**用户可感**:
- 打开 .md 文件,看见 7 种背景色
- 打开 .txt 文件,无背景色
- `mdrender: false` 关闭后无背景色
- 配置错误不 panic

**代码可测**:
- 12 个表格测试通过(从 docs/ 迁入)
- 11 个新单元测试通过
- 1 个 benchmark 占位通过

满足以上三点,Step 0 完成,可进入 Step 1 模式系统的开发。
