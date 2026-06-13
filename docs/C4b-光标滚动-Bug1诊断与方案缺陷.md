# C4b：光标滚动 — Bug1 诊断（修正版）

> 关联：C4 方案见 `docs/C4-光标滚动-row判定方案.md`；根因背景见 `docs/C0-光标滚动-现状调研.md`。
>
> 本文是 **C4b 第一版的勘误重写**。第一版把多处"连续相同行号"误判为 softwrap，
> 经对照 `sample.md` 原文核实，其中多数实为**装饰行**。本文用 0-indexed 逐行重新对照。
> 本文不修改 C4 本身，仅作诊断备查。

---

## 0. 勘误说明

第一版的核心错误：把 dump 中 `row 32: line 52 / row 33: line 52` 这类
"连续相同行号"一律当作 **softwrap 续行**。

经用户指出并对照 `sample.md` 原文核实：

| 第一版判断 | 实际（对照 sample.md） |
|------------|------------------------|
| `line 52` 双行 = softwrap | `buf[52]` = `# Tables`（标题），第 2 行是**标题下划线装饰行** |
| `line 54` 双行 = softwrap | `buf[54]` = `\| Column A...`（表格首行），第 2 行是**表格 top frame 装饰行** |

这一勘误不止是细节——它暴露了 `viewportRowBufLine` 的真实语义，进而牵出 C4 代码里的
**三个真实 bug**（见 §2）。本文据此重做。

---

## 1. 前提澄清：viewportRowBufLine 的真实语义

### 1.1 三种 row 的取值

`viewportRowBufLine[i]` 记录屏幕第 `i` 行对应的 buffer 行号，但**取值规则**
（来自 `renderSegmentMD:116-134` 与 `renderSegmentNative:672-674`）比 C4 设想的复杂：

| row 类型 | 取值 | 来源 |
|----------|------|------|
| 内容行 | 该 buffer 行号（非负） | 两条渲染路径都填 `bloc.Y` / `effectiveLine` |
| 装饰行（MD） | **绑定的内容行号（非负）** | `renderSegmentMD:119-121` 装饰行复用最近/首个内容行号 |
| softwrap 续行（原生） | **主行号（非负）** | `renderSegmentNative:672-674` 续行复用 `bloc.Y` |
| 空白区 | -2 | `displayBufferMD:712` 初始化填充 |

**关键**：装饰行和 softwrap 续行的取值都**非负**，都等于某个内容行号。
C4 文档里"装饰行 = -1"的设想（C4 §3.1）是错的。

### 1.2 后果：连续相同行号无法在数组层面区分

给定 `viewportRowBufLine`，看到 `row 35: line 54 / row 36: line 54`，
**无法判断** row 36 是：

- 表格 top frame 装饰行（MD 渲染），还是
- buf[54] 的 softwrap 续行（原生渲染），还是
- 别的

要区分必须回到渲染层（知道该 row 来自哪条路径）。这直接导致 §2 的下游 bug。

---

## 2. C4 代码暴露的三个 Bug

C4 的核心计算依赖两个函数，它们的实现都有问题。

### Bug-α：`bufferLineToScreenOffset` 倒序返回——装饰行让 cursorRow 偏大

`bufwindow_md.go:846-854`：
```go
func (w *BufWindow) bufferLineToScreenOffset(bufferLine int) (int, bool) {
	for i := len(w.viewportRowBufLine) - 1; i >= 0; i-- {  // 倒序
		if w.viewportRowBufLine[i] == bufferLine {
			return i, true                                   // 返回最后一个匹配
		}
	}
	return 0, false
}
```

注释自承"返回最后一个屏幕行"。但当光标行 `c.Line` 后面**跟着它的装饰行**
（如标题下划线、表格 frame）时，装饰行也标记为 `c.Line`（§1.1），
倒序命中**装饰行**而非光标真正所在的内容行。

→ `cursorRow` 系统性偏大（多出装饰行数）→ `delta` 偏大 → `StartLine` 偏小
（视口起点偏上）→ 重新渲染后光标被推到 botMarginRow 之下。

**这正是用户感知的"光标跑到 statusLine 下面"的直接成因之一。**

### Bug-β：`bufferLineToScreenOffset` 倒序返回——softwrap 续行让 cursorRow 偏大

同一函数，对 softwrap 长行同样出错。长行（如 `buf[68]` 长段落）原生渲染时
占多个屏幕行（§1.1），全部标记为同一行号。倒序返回**续行末尾**的 row，
而光标实际可能在**第一屏行**。

→ `cursorRow` 偏大（多出 wrap 续行数）→ 同 α 的连锁反应。

**这是用户感知的"经过长段落偏差变大到 4"的成因。**

### Bug-γ：`viewportRowBufLine[delta] < 0` 检查永远命中不了装饰行

`relocateVerticalMD`（C4 §4.2）：
```go
if delta >= n || w.viewportRowBufLine[delta] < 0 {
	return w.relocateVerticalNativeFallback(...)  // 想在 delta 落装饰行时兜底
}
w.StartLine = SLoc{viewportRowBufLine[delta], 0}
```

设计意图：delta 落在装饰行（原以为 = -1）时走 fallback。
但 §1.1 证明装饰行值非负，`< 0` 永远为 false → **这个兜底分支对装饰行形同虚设**，
delta 落装饰行时会直接把装饰行绑定的内容行号当作 StartLine。

---

## 3. 逐帧重新分析（对照 sample.md，buffer 0-indexed）

> dump 中 `line N` = `buf[N]` = `sample.md` 第 `N+1` 行。
> 完整对照表见本节末附录。

### 3.1 帧 A：光标在 buf[54]（表格首行 `| Column A...`）

```
cursor: bufferLine=54 screenRow=36  botMarginRow=33  delta=3
viewportRowBufLine[delta=3] = 24  (将作为新 StartLine)
viewport rows:
  row  0: line 22   buf[22]=第23行 (空行)
  row  1: line 23   buf[23]=第24行 "# Task List"            ← 标题内容
  row  2: line 23   标题下划线装饰行（绑定 23）              ★装饰行
  row  3: line 24   buf[24]=第25行 (空行)                    ← delta 落点
  ...
  row 13: line 52   buf[52]=第53行 "# Tables"                ← 标题内容
  row 14: line 52   标题下划线装饰行（绑定 52）              ★装饰行
  row 15: line 53   buf[53]=第54行 (空行)
  row 16: line 54   buf[54]=第55行 "| Column A..."           ← 表格首行内容
  row 17: line 54   表格 top frame 装饰行（绑定 54）         ★装饰行
  row 18: line 54   表格 ? (第三行，疑分隔/装饰)             ★疑似装饰
  ...
  row 35: line 54   buf[54] 的内容行（光标真正位置）
  row 36: line 54   buf[54] 的装饰/续行                      ← cursorRow 错误落点
```

**问题链**：
1. 光标 `buf[54]` 真正在 row 35（内容行）。
2. 但 `bufferLineToScreenOffset(54)` 倒序命中 row 36（**Bug-α**：装饰行也标 54）。
3. `cursorRow=36` → `delta=36-33=3`（本应更小）。
4. `viewportRowBufLine[3]=buf[24]`（空行），`< 0` 不成立（**Bug-γ** 未拦截）。
5. `StartLine=buf[24]`，比正确值偏小（视口偏上）。
6. 重渲染后 buf[24..54] + 装饰占的屏行数 > 33，光标被推到 botMarginRow 之下。

**偏差性质**：**Bug-α/γ 主导**。本帧光标段（表格）虽原生渲染，
但 `bufferLineToScreenOffset` 仍把光标后的装饰行算进来。

### 3.2 帧 B：光标 buf[59] → buf[60]（跨段切换，灾难性）

#### B-1：光标在 buf[59]（第60行，空行，紧接表格段后）

```
cursor: bufferLine=59 screenRow=34  delta=1
viewportRowBufLine[delta=1] = 28  (buf[28]=第29行 "# Code Blocks" 标题)
viewport rows (尾部):
  row 30: line 55   buf[55]=第56行 "|---...|"  (表格分隔，原生渲染 1 行)
  row 31: line 56   buf[56]=第57行 "| Data 1..."
  row 32: line 57   buf[57]=第58行 "| Data 4..."
  row 33: line 58   buf[58]=第59行 "| **Bold**..."               ← botMarginRow
  row 34: line 59   buf[59]=第60行 (空行)                        ← CURSOR
  row 35: line 60   buf[60]=第61行 (空行)
  row 36: line 61   buf[61]=第62行 "# Blockquotes"
```

光标 buf[59] 不在表格段（buf[54..58]）→ 表格段 **MD 渲染**？但 dump 显示
表格每行只占 1 行（row 30-33），说明此刻表格段**仍是原生渲染**——
因为 `hasCursorInside` 按段判断，buf[59] 所在段若与表格同段则原生。
此处表格行仅 1 行/行，是原生渲染的特征（MD 渲染会有 frame 装饰）。

`delta=1`，`StartLine=buf[28]`（# Code Blocks 标题）。此处 buf[28] 是标题，
新视口从标题开始。本帧动作本身几何尚可。

#### B-2：光标移到 buf[60] 后（同一视口重渲染）

```
cursor: bufferLine=60 screenRow=0  → FALLBACK(not-in-viewport)
current bottom content: screenRow=36 bufferLine=58
viewport rows (尾部):
  row 28: line 54   buf[54]=表格首行
  row 29: line 54   表格 top frame 装饰（绑定 54）          ★涌现
  row 30: line 54   表格 ? 装饰（绑定 54）                 ★涌现
  row 31: line 56   buf[56]
  row 32: line 56   装饰/续行（绑定 56）                   ★涌现
  row 33: line 57   buf[57]
  row 34: line 57   装饰/续行（绑定 57）                   ★涌现
  row 35: line 58   buf[58]
  row 36: line 58   装饰/续行（绑定 58）                   ★涌现
                                                     ← buf[59]/[60]/[61] 全部被挤出视口
```

**问题链（根本性，非计算 bug）**：
1. B-1 → B-2：光标从 buf[59] 移到 buf[60]。
2. 若 buf[59] 与表格段同段、buf[60] 不同段，则**表格段从原生渲染切回 MD 渲染**。
3. MD 渲染下表格每行冒出 frame 装饰行（buf[54] 从 1 行变 3 行，buf[56/57/58] 各变 2 行）。
4. 视口屏行预算固定（bufHeight=37），装饰行挤占后，**buf[59]/[60]/[61] 被挤出视口**。
5. 下一帧 Relocate：`bufferLineToScreenOffset(60)` 找不到 → `FALLBACK(not-in-viewport)`
   → `relocateVerticalNativeFallback`（1:1）→ StartLine 偏小 → 错上加错。
6. 用户连续按 ↓ 多次，光标才重新进入视口。

**偏差性质**：**跨段切换的根本问题**（prefix-sum 级），Bug-α/β/γ 无法解决。
这是用户感知的"光标跑出去不见了，继续按才出来"的成因。
偏差量 ≈ 涌现的装饰行总数（本例约 5-6 行）。

### 3.3 帧 C：光标在 buf[68]（第69行，超长段落）

```
cursor: bufferLine=68 screenRow=36  delta=3
viewportRowBufLine[delta=3] = 42  (buf[42]=第43行 "```go")
viewport rows (尾部):
  row 33: line 66   buf[66]=第67行 "# Mixed Long Paragraph"    ← botMarginRow
  row 34: line 66   标题下划线装饰（绑定 66）                  ★装饰行
  row 35: line 67   buf[67]=第68行 (空行)
  row 36: line 68   buf[68]=第69行 "On the 4th local time..."  ← CURSOR（长段落）
```

buf[68] 是超长段落，原生渲染下会 softwrap 成多屏行。但视口底（row 36）只显示了
它的**第一屏行**（后续屏行在 row 37+，超出视口）。

**问题链（Bug-β 主导）**：
1. 光标 buf[68] 真正在 row 36（第一屏行，也是此时视口内唯一屏行）。
2. 此处 cursorRow=36 暂时"碰巧正确"——但下一帧（StartLine 改 buf[42] 重渲染后）
   buf[68] 从 row 33 开始展开成 4-5 个 softwrap 屏行（row 33..37）。
3. `bufferLineToScreenOffset(68)` 倒序命中 **row 37（续行末尾）**（**Bug-β**），
   而光标实际在 row 33（第一屏行）。
4. `cursorRow=37` → `delta=4` → 又触发向下滚 → 但 buf[68] 已经在视口里，
   不该再滚 → 视口被错误地继续上移 → 光标相对地"偏低"。
5. 反复：每次重渲染 buf[68] 都展开多行，倒序返回续行末尾，误判为需下滚。
   这与用户反馈"经过长段落偏差变大到 4"吻合（偏差 ≈ softwrap 续行数）。

**偏差性质**：**Bug-β 主导**（softwrap 倒序返回）。本帧无跨段切换。

---

## 4. 偏差归因汇总

| 帧 | 现象 | 主因 | 性质 |
|----|------|------|------|
| A（buf[54] 表格首行） | 光标偏低 2-3 行 | Bug-α（装饰行倒序）+ Bug-γ | **计算 bug，可修复** |
| B（buf[59]→[60] 跨段） | 光标消失，多按才回来 | 跨段切换涌现装饰行 | **根本问题，prefix-sum 级** |
| C（buf[68] 长段落） | 光标偏低 4 行 | Bug-β（softwrap 倒序） | **计算 bug，可修复** |

**关键判断**：
- **Bug-α / Bug-β / Bug-γ 是计算实现错误**，修复后帧 A、帧 C 类场景应大幅改善。
- **帧 B 是方案级缺陷**（光标段原生渲染特权导致屏行布局随光标漂移），
  修复 α/β/γ 无法解决，需收敛循环或 prefix-sum。

---

## 5. Bug 修复方案（针对 α/β/γ）

### 5.1 修 Bug-α / Bug-β：`bufferLineToScreenOffset` 应返回光标真实 row

根因：函数只接收 `bufferLine`，无法知道光标在该行的哪个屏行（softwrap），
也无法区分内容行 vs 装饰行。

**修法方向**：改用"第一个匹配"（正序）而非"最后一个匹配"（倒序）。
- 对装饰行：正序命中内容行（装饰行在内容行之后），返回内容行 row。✓ 修 Bug-α。
- 对 softwrap：正序命中第一屏行，恰是光标默认/常见位置。✓ 缓解 Bug-β。

**残余**：softwrap 时光标可能在中间屏行（光标 X 在该行较后位置），
正序仍偏小。彻底解决需要传入光标 X 坐标计算精确 wrap 行——侵入性大。
但"正序"已能把偏差从"续行末尾"降到"第一屏行"，多数场景够用。

### 5.2 修 Bug-γ：装饰行识别

`viewportRowBufLine` 层面无法识别装饰行（§1.2）。要真正拦截"delta 落装饰行"，
需要渲染层额外标记每 row 是否装饰（如并行数组 `viewportRowIsDecoration[]`）。
侵入性中等。

**廉价替代**：若 5.1 改正序后，delta 通常落在内容行上（因为 cursorRow 不再含
后续装饰行），Bug-γ 触发概率大降，可暂不修。

---

## 6. 修正方向（重新评估，含 bug 修复后）

修复 α/β/γ 后，帧 A、帧 C 类（非跨段）场景预期正常。剩下的核心难题是帧 B（跨段）。

### 方向 B（收敛循环）—— 现在更可行
修复 α/β/γ 后，常规场景初值已精确，收敛循环只在跨段切换时触发（罕见），
抖动风险大降。值得重新评估。

### 方向 A（prefix-sum，放弃光标段原生渲染）—— 仍是最重但最彻底
帧 B 的根治仍需稳定屏行映射。若可接受编辑体验变化，方向 A 一劳永逸。

### 方向 C（放宽精度）—— 保底
最小改动，先保证不崩溃、光标不彻底丢失。

**建议路径**：先修 α/β/γ（5.1 正序 + 评估 5.2）→ 重测 → 若帧 B 仍明显，
再加收敛循环（方向 B）。

---

## 7. 附录：sample.md 0-indexed 对照表

dump 中 `line N` = `buf[N]` = `sample.md` 第 `N+1` 行。关键区段：

```
buf[13] = # Lists                    （标题，带下划线装饰）
buf[23] = # Task List                （标题，带下划线装饰）
buf[28] = # Code Blocks              （标题，带下划线装饰）
buf[52] = # Tables                   （标题，带下划线装饰）
buf[54] = | Column A | Column B | Column C |    （表格首行，MD 渲染有 top frame 装饰）
buf[55] = |----------|----------|----------|    （表格分隔行）
buf[56] = | Data 1 ... |             （表格数据行）
buf[57] = | Data 4 ... |
buf[58] = | **Bold** ... |
buf[61] = # Blockquotes              （标题，带下划线装饰）
buf[66] = # Mixed Long Paragraph     （标题，带下划线装饰）
buf[68] = On the 4th local time ...  （超长段落，softwrap 占 4-5 屏行）
```

**装饰行规律**（MD 渲染）：
- 标题：内容行 + 1 个下划线装饰行（绑定标题行号）。
- 表格：首行有 top frame 装饰，行间可能有分隔装饰（绑定各自内容行号）。

**softwrap 规律**（原生渲染）：
- 长行（如 buf[68]）按 bufWidth 折行，多个屏行复用同一行号。
