# MicroNeo 架构设计 Review

> 审阅人：Lisa（架构师视角）
> 日期：2026-06-02
> 文档：`docs/架构设计.md`（V2）

---

## 1. 整体评估

### 设计质量：总体好评，具体问题需澄清

这套架构是**务实主义**的产物——不是从理论出发的理想设计，而是从"Micro 的渲染循环是每帧重绘"这个客观事实出发，一步步推导出来的。推导链条清晰（D1–D6 都有理由），没有凭空发明层或抽象。

**优点**：
- 渲染模型（每帧重渲染）选择正确。与 Micro 宿主模型天然契合，集成成本最低。
- 分层原则明确：Pipeline → Registry → Segment → Inline，每层职责单一。
- 横切关注点（Mapper / Config / Syntax Highlight Adapter）单独拎出来是正确的——它们跨越多个渲染层级，强行塞进某一层会造成污染。
- "我们是渲染层"的定位清晰。Micro 原生是 fallback，而不是并列的两条路。这个心智模型对后续所有决策都有指导意义。

**问题**：
- 5 层 + 3 横切的命名和边界在文档里清楚，但实际代码里 `bufwindow.go` 的改造是否真的只动 Layer 5、还是会把 Pipeline 嵌入 `displayBuffer` 内部（成为实际上的一层）？这段"接入点"的细节需要在详细设计阶段写清楚。
- Layer 5（Host Integration）被描述得很薄（"判断 + 委托"），但实际上 `bufwindow.go` 的改造最复杂的部分恰好在这里——它要决定"哪些行走 pipeline，哪些行走 fallback"，以及"每帧输出写到 screen 的循环在哪里终止"。这个控制逻辑如果在 Layer 5 里做，Layer 5 就不薄；如果在 Pipeline 里做，Layer 5 就只是 if 分支。职责边界有歧义。

### 分层与职责划分

**合理**。Pipeline 是协调层，Registry 是索引层，Segment 是执行层，Inline 是工具层——每层都有一个清晰的"做什么、不做什么"。

**一个隐患**：第 4 章数据流描述里，"把 output 写到 screen（逐 cell 调用 screen.SetContent）"这一步放在 Pipeline 循环里。这意味着 Pipeline 既是协调层又是渲染循环的执行体。如果将来需要把渲染结果传给别的 consumer（比如状态栏、tooltip），就要在 Pipeline 内部再加分支。这是架构债务，但 v0.1 阶段可以接受。

### 高内聚低耦合

整体符合。Segment 主动对象设计（D1）让新增渲染器不需要改 Pipeline 和 Registry，耦合点清晰。

**扣分项**：Config 作为横切层被各层共享，但没有定义访问契约。如果 Segment 在 Render() 内部读取配置，配置变更时可能产生"渲染到一半读到了新旧混合的配置"问题。D5 提到"不做实时传播"是缓解，但没有说清楚"在帧内，Segment 拿到的配置快照是一致的"如何保证。

---

## 2. 渲染模型评估

### "每帧重渲染 + 写到 screen 层"

**正确**。理由充分：

1. Micro 本身就是每帧重渲染（`displayBuffer` 每帧跑），没有 pane buffer 的持久化概念。逆着这个模型做缓存是自找麻烦。
2. v0.1–v0.4 阶段 segment 输出都是 1:1（buffer line = screen line），每帧重渲染的开销极低（几十行）。v0.5 起才有行数差，这正好是 Mapper 介入的时机——架构和时间线匹配。
3. 大文件友好：只渲染视口内的行。

**一个被低估的 trade-off**：每帧重渲染意味着 Registry 的 segment 检测每帧都要跑（或至少每帧问一次）。O5 讨论了"每帧跑 Detector" vs "buffer 变更后跑"，但没有给出明确结论。如果 v0.1 真的每帧跑 Detector（哪怕只跑视口范围内的行），Detector 的性能就成了关键约束。表格检测要扫视口行、对每行做 `|` 判断——这个开销在 v0.2 表格真渲染阶段会变高，因为要真正解析列边界。建议在 v0.1 的验收标准里加一条 benchmark：视口 80 行、文件 10K 行时，Pipeline.Render() 的 p99 耗时必须 < 5ms。

### 与 Micro 渲染模型的一致性

确实带来好处。最直接的好处是"没有 pane buffer 要同步"。Micro 原生的 fallback 和 segment 渲染只是两条产生 `(rune, style)` 序列的代码路径，目的地都是 `screen.SetContent`。edit state 的 fallback 没有"切换显示后端"的复杂度。

**但有一个微妙之处**：Micro 的 `displayBuffer` 在做 fallback 时，包含了 gutter（行号、消息）、光标渲染、软换行等完整逻辑。如果我们的 Pipeline 只接管"内容行"的渲染，那么 gutter 和光标是谁画的？文档里说"两条路径最终都汇合到 `screen.SetContent`"，但 Micro 原生 fallback 画的 cell 包括 gutter，光标也在 gutter 里画。如果 Pipeline 也画 gutter，两边会冲突；如果 Pipeline 不画 gutter，Micro 原生 fallback 在走 Pipeline 路径时 gutter 就缺失了。

**这个边界必须在详细设计阶段明确定义**：Pipeline 的输出范围是"仅内容区"还是"包含 gutter"？如果包含 gutter，Pipeline 的 Render() 要接收 gutter 信息作为输入；如果不包含 gutter，gutter 在哪里画？

---

## 3. 探照灯模型评估

### "光标驱动 segment edit state" 取代双模式

**决策正确**，理由在里程碑文档里已经说清楚了——消灭了最大的风险源（输入拦截器、模式状态机）。

**但有一个人机交互上的问题文档没有正面讨论**：PRD 的探照灯模型是"光标所在行回退原生"，但用户看到的不是"漂亮渲染 + 光标在某行临时变回 markdown"，而是"大部分行是渲染效果，光标本身带一个可见的光标形状"。

换句话说：**探照灯模型下，用户始终在看"渲染效果 + 一个原生光标"**。这个体验在语义上是正确的（"光标在表格里，表格临时变回 markdown"），但视觉上可能让用户困惑——光标在渲染后的漂亮内容上跳变回粗糙的 markdown 原稿。

PRD 里说"10 秒规则让大部分阅读时间看到的是纯渲染效果"，但探照灯模型下"纯渲染效果"里仍然有一个原生光标在那里。v0.6 的"纯阅读模式"（隐藏光标）才是真正解决这个视觉问题的方案。文档需要明确承认这一点，而不是把探照灯模型宣传成"用户看到的是纯渲染效果"。

### 双模式作为可选叠加

设计正确。Mode Controller 是独立层，对下层透明。这个分层保证了即使不做双模式，架构不受影响。

**问题**：Mode Controller 在 v0.6 实现时，需要拦截方向键。如果 Micro 的 action 层在 Mode Controller 之前处理了方向键（比如已经在移动光标），Mode Controller 的拦截还有效吗？这需要在 v0.6 之前做 PoC，不能等实现时才发现行为不符预期。

---

## 4. 接口设计

### Segment.IsEditState(cursors) 主动对象设计

**合理**。Segment 知道自己负责哪几行，`IsEditState` 只需要问"这些光标里有没有落在我的范围内"，这是 O(log N) 的二分查找。Segment 不需要维护状态，接口是纯函数式的（给定 cursors 返回 bool），符合"无状态 renderer"的扩展原则。

**一个潜在问题**：`IsEditState(cursors)` 的返回值是 bool，但实际业务需求可能更细——比如光标在 segment 的第 2 行 vs 第 5 行，渲染行为可能有差异（行 2 是表头，需要特殊处理）。目前 `IsEditState` 返回 bool 是够用的，但 `Render(line int)` 的签名是按行调用的，说明 segment 内部已经知道"当前渲染哪一行"。如果将来需要"edit state 但只编辑部分区域"，Segment 接口需要扩展。

### RenderCell 数据结构

```go
type RenderCell struct {
    Rune  rune
    Style tcell.Style
    Kind  CellKind  // Content / Decoration / Padding
}
```

**v0.1 阶段正确，v0.5+ 有问题**。

`Kind` 字段（Content / Decoration / Padding）是为 v0.5 鼠标精确定位准备的。但 `Kind` 是 `CellKind` 枚举，描述的是"渲染产物里这个字符是什么类型"，而不是"这个字符对应 buffer 哪个位置"。鼠标点击定位需要的是反向映射（screen offset → buffer Loc），`Kind` 只是这个映射的辅助信息。

真正需要的接口是 `Segment.MapScreenToBuffer(screenLine, screenOffset int) (buffer.Loc, bool)`（在 O4 里已经提到）。如果 v0.5 的 mapper 依赖 Segment 提供这个接口，那么 `RenderCell.Kind` 就是一个中间表示——它告诉 mapper"哪些 cell 是内容字符"，但 mapper 仍然需要知道"内容字符在 buffer 的哪个位置"。

**建议**：`RenderCell` 里加一个可选的 `BufferOffset` 字段（int，-1 表示装饰字符），或者在 segment 层面提供 `MapOutputToBuffer(segmentLineOutputIndex int, runeOffset int) buffer.Loc`。不要让 mapper 靠 `Kind` 枚举去猜。

### Pipeline.Render 接口

```go
func (p *Pipeline) Render(
    buf *buffer.Buffer,
    viewport Viewport,
    cursors []*buffer.Cursor,
) []RowOutput
```

**问题**：`viewport` 是 `struct { StartLine int; ... }` 还是什么？文档没有定义。`RowOutput` 是"一行屏幕内容的完整描述"，但"完整"到什么程度？如果包含 gutter 信息，这个接口和 Micro 的 `displayBuffer` 就有重叠；如果不包含，调用方需要自己画 gutter。需要在详细设计阶段明确定义 `RowOutput` 的内容。

---

## 5. Mapper 设计

### 独立横切层 vs 嵌入 Pipeline

**正确**。理由充分：mapper 是"屏幕行 ↔ buffer 行"的双向映射，是一个独立的数据变换，不属于 Pipeline 的协调职责。独立成层便于测试、便于替换算法。

### Mapper 与 Segment 的分工

**文档说的清楚，实际实现时需要警惕**。

文档说"跨行结构由 Segment 处理上下文，Mapper 只管行数对应"。但"上下文"和"行数"之间的界限在复杂场景下可能模糊。

考虑这个场景：表格第一行（表头）在 buffer 是 1 行，在 screen 输出 2 行（表头文字行 + 分隔符行）。这个"多输出 1 行"是 Segment 的职责（Segment.Render() 输出 2 行），Mapper 负责记录"buffer 第 N 行 → screen 第 M 行，占 2 行"。

但如果将来要在表格上方加一个标题行（"表格："），这是 Segment 加的装饰行，还是应该算 Mapper 的职责？文档会说"装饰行由 Segment 输出"，但"什么算装饰、什么算 mapper 的职责"没有客观标准。Segment 输出的多出行可能包含：1) 表格边框（装饰）、2) 表头分隔行（结构）、3) 单元格内 softwrap（内容相关）。Mapper 需要记录的是哪些？

**建议**：Mapper 只记录 buffer line → screen line count，不关心"多出来的行是边框还是内容"。Segment.Render() 返回的 `[]RenderCell` 里，`Kind == Decoration` 的行 Mapper 也照常记录 count。这样职责完全分离。

### Mapper 生命周期（每帧重算 + 状态持久）

**合理**，但有一个边界情况需要处理：

如果某一帧渲染时，buffer 没有变化（用户没有编辑，只是鼠标滚动），Mapper 的状态可以直接复用。但如果有变化（用户编辑了一行），Mapper 的状态需要**部分失效**——比如用户在 buffer 第 100 行插入了一行，后面的所有行号 +1。

Mapper 内部维护的是 `map[int]int`（buffer line → screen line count），这个 map 在 buffer 行号偏移时需要整体调整。如果文件有 10K 行，每次插入一行都触发整体调整，开销不小。

**建议**：在 v0.5 实现 Mapper 时，考虑用"起始行 + 增量"的方式存储（类似 Micro 的 StartLine），而不是每个 buffer 行都存一个映射。这样插入行只需要更新插入点之后的 delta，不需要遍历全 map。

---

## 6. 扩展性

### "新增 renderer 只需要改 3 个地方"

**文档声称正确，但实际可能漏了第 4 个地方**：

1. 新增 `md/xxx.go`
2. 在 Registry 注册 `XxxDetector`
3. 加配置项

**第 4 个地方**：如果这个新 renderer 需要在 `bufwindow.go` 里有特殊处理（比如 gutter 不显示、光标特殊行为），Host Integration（Layer 5）可能也要改。以 v0.1 的表格为例：表格需要隐藏行号（gutter 规则和普通行不同）。如果未来一个 codeblock segment 也需要不同的 gutter 规则，那么"所有 `.md` 文件不显示行号"（PRD 5.5）就是正确的简单解。但如果将来有细分需求（"某些 segment 显示行号，某些不显示"），就需要在 Layer 5 或 Pipeline 里加 segment 类型判断。

**配置可组合性**：关闭 `mdtable` 后，table 行落入 paragraph 兜底——这个退化路径清晰、正确。✅

**paragraph 作为兜底 segment**：设计优雅。保证 Pipeline 的代码路径单一，任何行都有 segment 处理，不存在"没有 segment 匹配"的异常分支。

---

## 7. 风险评估

### 架构债务（v0.x 后期可能需要重构的地方）

**⚠️ 关键风险 1：Segment.Render(line int) 的行上下文传递方式**

当前 `Segment.Render(line int)` 接收一个行号，返回该行的 `[]RenderCell`。这意味着 Segment 自己持有"我是谁"的信息（StartLine/EndLine），然后根据传入的行号计算"我在渲染第几行"。

但 v0.5 之后，Segment.Render() 可能输出多行（表格加边框）。如果一次调用返回多行，Mapper 需要知道"这些行分别对应 buffer 哪一行"。

两种方案：
- A) `Segment.Render(range) [][]RenderCell`：一次性渲染一个 segment 的所有行，返回按行拆好的 slice，Mapper 逐行记录
- B) `Segment.Render(line int) []RenderCell`：仍然按行调用，但 Segment.Render() 内部缓存自己上次输出的行数，"这一帧我输出了 3 行"的信息通过 Segment 内部状态传递给 Mapper

方案 A 更清晰，但需要 Pipeline 对一个 segment 做批量调用（"从 StartLine 到 EndLine 一次问完"）。方案 B 更符合当前的"逐行调用"风格，但 Segment 内部需要有状态（上一帧输出行数）才能让 Mapper 记录。

**文档没有明确这个问题，建议在 v0.5 详细设计之前确定方案**。

**⚠️ 关键风险 2：displayBuffer 和 Pipeline 的边界模糊**

当前设计里，`bufwindow.go` 的 `displayBuffer()` 是入口点，Pipeline 在它内部被调用。但 `displayBuffer()` 本身有大量逻辑（软换行、gutter、光标渲染）。如果 Pipeline 也需要一个"外层循环"来调用 `screen.SetContent`，那么 `bufwindow.go` 的改造方式就有两种：

- 方式 1：保留 `displayBuffer` 的外层循环（for 每行），内层 if 分支选择"走 Pipeline 还是走 Micro fallback"
- 方式 2：在 Pipeline 内部做外层循环（for 每行），`displayBuffer` 只判断"是 .md 且开渲染吗"，是就全权委托 Pipeline

方式 1 是增量式改造（改动小），方式 2 是替换式改造（改动大但更干净）。文档没有明确选择哪种，需要在 v0.1 详细设计里确认。

**⚠️ 关键风险 3：Micro 上游合并**

这是一个被低估的长期风险。Micro 原版停滞（858 个 issue），但 MicroNeo 作为 fork，一旦上游有安全更新或功能更新，合并成本可能很高。架构设计里说"改动集中在 `internal/display/` 层"，但 `bufwindow.go` 的改动如果嵌入到主循环的特定位置（方式 2），合并时会非常痛苦。建议在 v0.1 阶段就把 bufwindow.go 的改动写成**最小侵入**的形式——如果可能，用 Hook 模式（注册回调）而不是直接改 `displayBuffer` 的主循环。这样未来上游的 `displayBuffer` 改动只需要更新 Hook 注册点。

**中风险：O1（segment 缓存失效粒度）的实现复杂度**

文档建议"先做全文级，性能不行再优化"，务实。但从 v0.1 开始的全文级缓存（每帧跑 Detector）是性能的地雷。v0.2 表格真渲染时，如果表格检测要扫全文件（哪怕只扫视口外），用户滚动时发现渲染有延迟，bug report 会很难定位（是 detector 的问题还是 mapper 的问题？）。

**建议**：在 v0.1 里就把 segment 检测的耗时做 benchmark 并固化到 CI 里。任何改动导致 detector 耗时增加 > 10%，CI 失败。这样保证后续版本加功能时不会无意中破坏性能。

---

## 8. 与同类系统的对比

### MicroNeo vs nvim render-markdown.nvim

- **nvim 方案**：基于 LSP/Tree-sitter，渲染粒度到 AST 节点（paragraph、list-item、heading），编辑时代码高亮用 nvim 内置的 syntax highlight（和渲染是两套系统）
- **MicroNeo 方案**：没有 AST，纯 regex/regex 启发式检测；渲染和编辑（fallback）是两套代码路径但共用 screen 层
- **差异是优势**：MicroNeo 不需要 nvim 的插件生态依赖，零配置，Go 实现编译成一个 binary。但劣势是"没有 AST"的检测精度不如 tree-sitter——比如 `|` 作为普通文本还是表格分隔符，regex 很难正确判断（当前用分隔行 `|---|` 确认是权宜之计）

### MicroNeo vs Glow/Leaf

- **Glow/Leaf**：纯只读，渲染结果是一次性输出到 tty，没有编辑态的 fallback
- **MicroNeo**：渲染管线内置 edit state 感知，编辑体验是架构内嵌的而不是事后叠加
- **差异是优势**：编辑 + 渲染同态，edit state 下直接委托给 Micro 原生，没有"两套渲染引擎要同步"的复杂度

### MicroNeo vs bat (--style=markdown)

- bat 是 pager 式的，不做编辑，没有 edit state 的概念
- 对比意义不大

---

## 9. 开放问题回应

### O1：segment 缓存失效粒度

**建议**：行级（只重算受影响的 segment）。但实现复杂度高，建议分阶段：

- **v0.1–v0.2**：段级（buffer 变更 → 该行所属 segment 整体 invalidate）
- **v0.3+**：如果性能不够，再做行级增量

段级失效在 v0.2 表格场景下是够的——用户编辑一行表格，整表 invalidate，下一帧重建表格 segment。这个代价在用户感知上是"编辑表格后表格重新渲染了一下"，是可接受的。

### O2：多光标落在同一 segment vs 不同 segment

**建议**：segment 自然处理（`IsEditState(cursors)` 遍历光标列表）。架构不需要特殊设计，但需要在单元测试里覆盖：
- 光标 A 在 segment 内，光标 B 在 segment 外
- 两个光标都在 segment 内
- 光标在 segment 边界上

### O3：`[]RenderCell` vs 紧凑表示

**建议**：v0.1 用 `[]RenderCell`。性能不够再优化。但要注意：如果改用 `([]byte, []tcell.Style)` 的并行 slice，需要保证两个 slice 长度严格一致，建议在测试里加一个 property-based 检查：`len(runes) == len(styles)`。

### O4：鼠标点击反向映射放在哪层

**建议**：Mapper + Segment 协作。Segment 提供 `MapOutputToBuffer(outputIndex, runeOffset int) (buffer.Loc, bool)`，Mapper 在更高层组合"屏幕行 → segment → segment 内部偏移"。

**当前状态**：v0.5 才用到，可以先不定。但接口草案应该现在就写进架构文档里，这样 v0.5 实现时不会发现接口设计不合理。

### O5：Registry 何时重建 segment

**建议**：buffer 变更后重建（增量）。但这需要 Micro 的 buffer 层提供变更通知（`OnChange` hook）。如果 Micro 没有这个 hook，v0.1 只能用"每帧重建视口内 segment"的方案（文档 O5 最后的建议）。

**⚠️ 注意**：如果 v0.1 真的每帧跑 Detector，性能基准必须在 v0.1 实现阶段就建立并监控。否则 v0.2–v0.4 加功能时可能无意中破坏性能。

### O6：Micro 原生 fallback 的代码路径

**建议：部分跳过**（文档已明确倾向）。"外层壳"（gutter、状态栏、视口控制）始终走我们的代码路径，edit state 行的内容生成委托给 Micro 原生函数。这个设计让外层逻辑只写一份。

**一个没识别的问题**：如果 Micro 原生函数 `displayBuffer` 在逐字符渲染时有软换行逻辑（处理超宽行），而我们的 Pipeline 也在处理同一行（但 segment 输出可能是 1:1），两者对"这一行占几个屏幕行"的计算结果是否一致？

Micro 的 softwrap 逻辑（`softwrap.go`）在渲染时会计算"buffer 这一行在窗口宽度下会软换行成几行"。但根据 PRD v0.5 之前"段落不换行、内容超宽时向右延展"的策略，v0.1–v0.4 阶段**应该主动禁用 softwrap**（至少对 `.md` 文件）。否则 Micro 原生 fallback 走 softwrap 路径，我们 segment 输出不走 softwrap，两者对同一 buffer 行的 screen 输出行数可能不同——导致 Mapper 记录的行数不匹配。

**建议**：在 v0.1 的 Host Integration 层，明确处理 softwrap 开关：`.md` 文件渲染时，如果 segment 输出是 1:1，softwrap 应该由 Pipeline 或 Mapper 来处理（passthrough），而不是让 Micro 原生逻辑参与。

---

## 10. 总结

### 架构是否可以作为 v0.1 开发的基础？

**可以，但有限制条件**。

核心架构（Pipeline / Registry / Segment / Inline / Mapper）是 sound 的，主要设计决策（D1–D6）都经得起推敲。探照灯模型的选择消灭了项目最大的风险源，这是整个架构最明智的决策。

**作为 v0.1 开发基础的限制条件**：

1. **displayBuffer 和 Pipeline 的边界必须先明确**（方式 1 还是方式 2，见第 7 章风险 2）。这个问题不解决，v0.1 的编码会陷入"我在这里写还是那里写"的决策疲劳。

2. **viewport 数据结构必须明确定义**（Pipeline.Render 的输入参数）。

3. **gutter 和光标在 Layer 5 还是 Pipeline 里画，必须明确**。

4. **Segment.Render() 批量调用 vs 逐行调用的方向必须在 v0.5 之前确定**（见第 7 章风险 1）。

5. **softwrap 对 `.md` 文件的处理策略必须在 v0.1 之前确定**，否则 v0.2 表格场景会出现行数不一致问题。

### 主要改进建议

| 优先级 | 建议 | 原因 |
|--------|------|------|
| **P0** | 明确 displayBuffer 和 Pipeline 的边界（方式 1 还是方式 2） | v0.1 编码的先决条件 |
| **P0** | 明确 `.md` 文件 gutter/softwrap 由谁负责 | 防止和 Micro 原生逻辑冲突 |
| **P0** | 建立 v0.1 的性能基准（Pipeline.Render p99 < 5ms）并加入 CI | 防止后续版本无意中破坏性能 |
| **P1** | 定义 `RowOutput` 的完整内容（包括是否含 gutter 信息） | 接口清晰化 |
| **P1** | 在 Segment 接口草案里加入 `MapOutputToBuffer`（为 v0.5 准备） | 避免 v0.5 时发现接口设计不合理 |
| **P2** | Mapper 改用"起始行 + delta"存储方式（而不是每个 buffer 行都存映射） | 大文件插入行时的性能 |
| **P2** | Hook 模式替代直接改 `displayBuffer` 主循环 | 降低上游合并成本 |
| **P3** | v0.1 里加 `RenderCell.BufferOffset` 字段（或 segment 层面加位置映射） | v0.5 鼠标精确定位的基础 |

### 开放问题清单（文档未识别）

| # | 问题 | 影响版本 |
|---|------|---------|
| **未识别问题 1** | `.md` 文件的 softwrap 是启用还是禁用？v0.1 阶段 segment 输出 1:1，但 Micro 原生的 softwrap 仍然会对超宽行做软换行。两者对同一行的 screen 输出行数可能不一致，导致 Mapper 行数映射错误。 | v0.1 |
| **未识别问题 2** | `BufWindow` 的 `displayBuffer` 有 `ModifiedThisFrame` 标志，Segment Registry 是否可以复用这个标志来判断"buffer 是否变化"？如果可以，O5 的问题（"何时重建 segment"）就有了解。 | v0.1 |
| **未识别问题 3** | Host Integration 层判断".md 文件 + mdrender 开启"之后，Pipeline 和 Micro 原生 fallback 在同一个 pane 内的共存边界是什么？哪些区域是 Pipeline 画、哪些是 Micro 原生画、哪些区域是 gap？ | v0.1 |
| **未识别问题 4** | `BufWindow` 在做 VSplit/HSplit 时，每个 pane 独立运行 Pipeline 实例，还是共享同一个 Pipeline 实例 + 不同的 viewport？后者需要 Pipeline 是无状态的（当前的 Render() 签名已经是无状态的，这是对的）。 | v0.9 |

---

## 附录：架构评分卡

| 维度 | 评分 | 说明 |
|------|------|------|
| 设计正确性 | ★★★★☆ | 核心架构 sound，主要决策有理有据 |
| 接口清晰度 | ★★★☆☆ | 主要接口草案清晰，但 `Viewport`、`RowOutput` 等关键类型未定义 |
| 扩展性 | ★★★★☆ | "改 3 个地方"在理想情况下成立，有边界情况需要注意 |
| 性能可预期性 | ★★★☆☆ | 每帧重渲染模型清晰，但 detector 性能没有基准 |
| 与 Micro 集成的侵入性 | ★★★☆☆ | 集中在 display 层正确，但 bufwindow 改造的深度未明确 |
| 探照灯模型 | ★★★★★ | 消灭最大风险源，是整个架构最明智的决策 |
| 文档完整性 | ★★★☆☆ | 5 层 + 横切 + 探照灯 + D1-D6 + O1-O6 很完整，但有 4 个关键边界未定义 |

**综合评价**：这是一份**成熟度中等偏上**的架构设计文档。核心决策正确，主要风险（双模式）已被主动规避。文档的完整性在"确定架构"这个阶段是够的，但在"可以开始编码"这个阶段还缺 4 个关键边界定义。修复这 4 个边界后，文档可以升级为"详细设计"的输入。