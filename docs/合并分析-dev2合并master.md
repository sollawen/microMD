# 合并分析：dev2 ← master 的冲突与迁移方案

> 背景：`v1.0.5` 发布后，把 master 合并进 dev2，产生 2 处 git 标记的冲突和 2 处未标记的编译 breakage。
> 本文分析冲突根因，并给出"用 master 的 2D 坐标算法 + dev2 的无 MDRender 世界观"的统一处理方案。
>
> 关联文档：
> - master 的 2D 方案详见 `docs/C4-光标滚动-row判定方案.md`
> - dev2 删除 MDRender 的决策详见 `docs/plan-remove-mdrender.md`

## 1. 分支拓扑与演进方向

```
          v1.0.4 (3d3e722b) ← dev2 从这里分叉
              │
    ┌─────────┴─────────┐
   master               dev2
    │                    │
  C4 光标滚动重构         删除 MDRender 配置 + notePane/EABP 新功能
  (57fdb847, f8d9e461)   (4eb6b7c2 等)
    │                    │
    └────────► merge ◄───┘  ← 当前位置
```

两边在 `internal/display/bufwindow*.go` 的**同一块代码**上各自演进，但改的是**两个正交维度**，于是纠缠成 git 无法自动合并的冲突。

## 2. 三条正交的改动轴线

把改动按维度拆开看，冲突就不再混乱：

| 轴线 | 方向 | 哪边做了 | 与另一边关系 |
|------|------|---------|-------------|
| **A. 屏幕坐标精度** | 1D（只记行号）→ 2D（记 Line+Row） | master（C4） | dev2 没动，需要"升级"到 2D |
| **B. MDRender 配置开关** | 保留 → 删除（IsMD 即渲染）| dev2 | master 仍保留，需要"统一删除" |
| **C. notePane / EABP / hideStatusLine** | 新功能（三分屏、agent 通信）| dev2 | master 完全没有，纯增量，**零冲突** |

**轴线 C 完全是 dev2 的增量**（`notepane.go` 554 行、`eabp/`、`bufwindow.go` 的 `hideStatusLine` 字段等），git 全部自动合并成功，不需要处理。真正纠缠的只有 **A 和 B 两条轴**。

> 同意这个判断：屏幕坐标的判断，master是更好的，应该使用master的变量和逻辑，删除dev2里面旧的代码。删除MDRender，dev2是正确的。

## 3. display 层逐点对照

### 3.1 数据结构（字段）

| | dev2 | master | 合并后（git 自动取 master）|
|---|------|--------|------|
| 屏幕行→buffer 映射 | `viewportRowBufLine []int` | `viewportRowmap []SLoc` | `viewportRowmap []SLoc` ✅ |

字段已经是 master 的 2D 版本（`SLoc{Line, Row}`）。**任何还引用旧 `viewportRowBufLine` 的代码都会编译失败。**

### 3.2 四个反查/映射函数

| 函数 | 方向 | dev2 | master | 合并后状态 |
|------|------|------|--------|------|
| 屏幕行→buffer行 | 正向 1D | `screenOffsetToBufferLine` | `screenRowToLine` | 已自动取 master 名 ✅ |
| buffer行→屏幕行 | **反向** | `BufferLineToScreenOffset(line)`（导出）| `lineToScreenRow(line, row)`（未导出）| ⚠️ **冲突 ②** |
| 光标滚动判定 | — | 无 | `relocateVerticalMD` | 已自动合并 ✅ |
| 原生兜底 | — | 无 | `relocateVerticalNativeFallback` | 已自动合并 ✅ |

### 3.3 调用点与 breakage

| 位置 | dev2 调用 | master 调用 | 当前状态 |
|------|----------|------------|---------|
| `bufwindow.go:257` (Relocate) | `w.Buf.IsMD`（无 MDRender）| `w.Buf.IsMD && w.mdConfig.MDRender` | ⚠️ 自动合并成 master 的，**引用了已删除的 MDRender 字段** → 编译失败（**雷1**）|
| `bufwindow.go:312` (LocFromVisual) | `w.Buf.IsMD` + `screenOffsetToBufferLine` | `w.Buf.IsMD && mdConfig.MDRender` + `screenRowToLine` | ⚠️ **冲突 ①** |
| `bufwindow.go:Display` | `!w.Buf.IsMD` | `!w.Buf.IsMD \|\| !mdConfig.MDRender` | 已自动取 dev2 ✅ |
| `notepane.go:433` | `BufferLineToScreenOffset(loc.Y)` | — | ⚠️ 引用旧函数 → 编译失败（**雷2**）|

## 4. 四处 breakage 的修法

合并后即使解完两处冲突标记，仍有 4 个点要改才能编译通过：

| # | 位置 | 问题 | 修法 |
|---|------|------|------|
| 冲突① | `bufwindow.go:307-315` | 条件 + API 纠缠 | **混合解**：条件取 dev2（`w.Buf.IsMD`），API 取 master（`screenRowToLine`）|
| 冲突② | `bufwindow_md.go:852-865` | 函数名 + 签名 + 字段纠缠 | 取 master 的 `lineToScreenRow`；**另加导出 wrapper `LocToScreenRow`** 供 notePane（见第 5 节）|
| 雷1 | `bufwindow.go:257`（非冲突区，自动合并进来）| `w.mdConfig.MDRender` 字段已被 dev2 删除 → 编译失败 | 删掉 `&& w.mdConfig.MDRender`，改成 `if w.Buf.IsMD {` |
| 雷2 | `notepane.go:433` | 调用已不存在的 `BufferLineToScreenOffset` | 改成 `bw.LocToScreenRow(loc)`（配合冲突②的 wrapper）|

**只有上面 4 个点要改。** 轴线 C 的 `hideStatusLine`、notePane 主体、EABP 等都是干净自动合并的，不用动。

## 5. notePane 能否复用 master 的 2D API？

> 结论：**能，而且迁移后位置更精确。** 推荐新增一个导出的高层 wrapper `LocToScreenRow`。

### 5.1 当前 notePane 用法

```go
// notepane.go:431
func (n *NotePane) locToScreenRow(bw, view, loc buffer.Loc) int {
    if bw.Buf.IsMD {
        if offset, ok := bw.BufferLineToScreenOffset(loc.Y); ok {  // ← 只用 loc.Y
            return offset
        }
    }
    // 非 MD 兜底
    sloc := bw.SLocFromLoc(loc)
    row := bw.Diff(view.StartLine, sloc)
    return row + view.Y
}
```

用途：`lowestCursorScreenRow` 找所有光标里**最低的屏幕行**，决定 notePane 浮窗上边框 `y` 坐标，确保浮窗不挡住光标。

### 5.2 1D vs 2D 的语义差异（关键）

**旧函数 `BufferLineToScreenOffset(bufferLine)`（1D）**：
- **倒序遍历**，返回该 buffer 行对应的**最后一个（最底部）屏幕行**
- softwrap 时一个 buffer 行占屏幕第 5、6、7 行，光标实际在第 6 行 → 返回 **7**
- 结果：边框放在光标下方 1 行，**偏了，但安全**（不会盖住光标）

**master 新函数 `lineToScreenRow(line, row)`（2D）**：
- **正序遍历**，精确匹配 `(Line, Row)` 二元组
- 同样场景光标在第 6 行 → 返回 **6**
- 结果：**精确**，边框紧贴光标下方

| 场景（softwrap，光标在 buffer 行的第 2 视觉行）| 旧 1D | 新 2D |
|---|---|---|
| 返回屏幕行 | 第 3 行（行底）| 第 2 行（精确）|
| 边框位置 | 偏低 1 行 | 紧贴 |
| 是否盖住光标 | 不会 | 不会 |

### 5.3 `SLocFromLoc` 是现成的桥梁

notePane 手里是 `buffer.Loc{X, Y}`，而 `lineToScreenRow` 要 `(line, row)`。中间缺的"segment row"恰好能由 `SLocFromLoc` 补上：

```go
// softwrap 关闭：SLocFromLoc 返回 {loc.Y, 0}      → row = 0
// softwrap 开启：SLocFromLoc 内部 getVLocFromLoc 算出真实 Row
sloc := bw.SLocFromLoc(loc)   // {Line: loc.Y, Row: <段内行>}
screenRow, ok := bw.lineToScreenRow(sloc.Line, sloc.Row)
```

而且 notePane 的**非 MD 兜底分支已经在调 `SLocFromLoc` + `Diff`**，说明这条桥接路径本来就是 notePane 的老朋友。**迁移到 2D 不引入任何新依赖，只是把 MD 分支也走上同一条路。**

### 5.4 三种迁移方案对比

| 方案 | 做法 | 评价 |
|------|------|------|
| **方案 1：导出 `LineToScreenRow`** | notePane 自己做 `SLocFromLoc` + `LineToScreenRow` | 可行，但把 SLoc 细节泄露给 action 层，notePane 要写两步 |
| **方案 2：新建高层 wrapper** ⭐ | display 层加 `LocToScreenRow(loc) (int,bool)`，内部封装 `SLocFromLoc` + `lineToScreenRow` | **推荐**。notePane 传 Loc 直接拿屏幕行，干净；2D 细节封在 display 内 |
| **方案 3：保留两个函数** | `BufferLineToScreenOffset`(1D) + `lineToScreenRow`(2D) 共存 | 不推荐。1D 函数体要改用 viewportRowmap 才能编译，等于多维护一个语义更差的函数 |

### 5.5 推荐方案 2 的实现

删掉冲突 ② 的 `BufferLineToScreenOffset`，取 master 的 `lineToScreenRow`，再额外加一个**导出**的高层函数：

```go
// LocToScreenRow 把 buffer 位置映射为视口内屏幕行（含 softwrap 精确匹配）。
// 供 action 层（如 notePane）使用。失败返回 (0,false)。
func (w *BufWindow) LocToScreenRow(loc buffer.Loc) (int, bool) {
    sloc := w.SLocFromLoc(loc)
    return w.lineToScreenRow(sloc.Line, sloc.Row)
}
```

notePane 端只需把 `BufferLineToScreenOffset(loc.Y)` 改成 `LocToScreenRow(loc)`，**还顺手吃掉了 `loc.Y` vs `loc` 的参数差异**（1D 丢列信息、2D 保留，正好都正确）。

## 6. 一句话结论

> 合并的本质 = **把 master 的 2D 坐标算法，嫁接到 dev2 的"无 MDRender 开关"世界观上**。
>
> 冲突只是表象，真问题是 git 没法理解两条轴线的正交性。
>
> notePane 不但能复用 master 的 2D API，而且**迁移后位置更精确** —— 用一个导出的 `LocToScreenRow(loc)` 高层 wrapper，把 2D 细节封在 display 层内，是改动最小、语义最干净的方案。

## 7. 待办（执行顺序）

按依赖关系处理：

1. **冲突 ② `bufwindow_md.go`**：取 master 的 `lineToScreenRow`，新增导出 wrapper `LocToScreenRow`
2. **雷1 `bufwindow.go:257`**：删 `&& w.mdConfig.MDRender`
3. **冲突 ① `bufwindow.go:307-315`**：条件取 dev2，API 取 master
4. **雷2 `notepane.go:433`**：`BufferLineToScreenOffset(loc.Y)` → `LocToScreenRow(loc)`
5. `make build-quick` 验证编译通过
6. 手动测试：MD 文件光标滚动 + notePane 浮窗定位
7. commit + 合并完成
