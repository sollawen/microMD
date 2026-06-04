# Micro 原生高亮机制

> 目的：完整讲解 Micro 自带的 syntax highlighter 是怎么工作的——从 buffer 打开到屏幕渲染的字符颜色计算全流程
>
> 读者：需要理解 micro 高亮机制以做 MicroNeo 渲染的工程师
>
> **不依赖**任何 MicroNeo 代码

---

## 一、概述

### 1.1 一句话定义

Micro 的 syntax highlighter 是一个**把文本字符映射为 group 名称的按需计算服务**。它把每行 buffer 转成一张“字符位置 → group 名字”的表（`LineMatch`），存在 buffer 里供渲染时查表。

### 1.2 本质上的 3 个事实

- **计算 vs 查询分离**：高亮计算是后台 service，渲染时是纯查 map
- **事件驱动**：高亮计算只在 buffer 打开 / 修改两个事件触发，**没有定时器**
- **状态机驱动**：跨行逻辑靠每行的 `state` 传递（“上一行结束时在哪个 region 内”）

### 1.3 整体架构（自顶向下）

```
┌──────────────────────────────────────────────────────────────────┐
│  syntax file (yaml)  ←  内置 runtime/syntax/*.yaml              │
│                          用户 ~/.config/micro/syntax/*.yaml      │
└────────────────────────────────┬─────────────────────────────────┘
                                 │ ParseDef → Def {patterns, regions}
                                 ▼
┌──────────────────────────────────────────────────────────────────┐
│  Highlighter  (pkg/highlight/highlighter.go)                    │
│  - NewHighlighter(def)                                           │
│  - HighlightStates(buf)   → 算每行 state                         │
│  - HighlightMatches(buf)  → 算每行 match (LineMatch map)         │
│  - ReHighlightStates(buf, start)  → 增量传染算 state             │
└────────────────────────────────┬─────────────────────────────────┘
                                 │ 调用
                                 ▼
┌──────────────────────────────────────────────────────────────────┐
│  Buffer  (internal/buffer/buffer.go)                            │
│  - 每行存 state (*region) + match (LineMatch)                    │
│  - SetSyntaxDef(): 打开时 async 启 Highlighter                   │
│  - MarkModified():  编辑时同步调 ReHighlightStates + Matches     │
└────────────────────────────────┬─────────────────────────────────┘
                                 │ 渲染时查
                                 ▼
┌──────────────────────────────────────────────────────────────────┐
│  Display  (internal/display/bufwindow.go)                       │
│  - getStyle() → b.Match(L)[X] → config.GetColor(group.String()) │
│  - 纯查询，不做高亮计算                                           │
└──────────────────────────────────────────────────────────────────┘
```

### 1.4 三个时点的行为

| 时点 | 谁触发 | 干什么 | 同步/异步 | 算几行 |
|------|--------|--------|-----------|--------|
| Buffer 打开 | `SetSyntaxDef` (buffer.go:1006) | `HighlightStates` + `HighlightMatches(0, End.Y)` | 异步 goroutine | 全文 |
| Buffer 编辑 | `MarkModified` (buffer.go:185) | `ReHighlightStates(start)` + `HighlightMatches(start, l)` | 同步 | 受影响行 |
| 屏幕重绘 | `displayBuffer` | `getStyle(bloc)` → `b.Match(L)[X]` | 同步 | 屏上可见行 |
| 用户不动 | - | **不计算** | - | 0 |

### 1.5 4 个关键数据结构

| 名称 | 类型 | 存什么 | 谁写谁读 |
|------|------|--------|----------|
| `pattern` | `{group Group; regex *regexp.Regexp}` | 单行规则 | highlighter 加载 |
| `region` | `{group Group; start,end,skip *regex; rules; parent}` | 多行规则 | highlighter 加载 |
| `State` | `*region` | “行末处于哪个 region” | highlighter 算，buffer 存 |
| `LineMatch` | `map[int]Group` | 一行的“字符位置 → group” | highlighter 算，buffer 存，display 读 |

### 1.6 4 个关键机制

1. **事件驱动**：高亮计算**只在** 打开 / 编辑 两个事件触发，没有定时器，没有 for-loop 轮询
2. **增量传染**：`ReHighlightStates(start)` 从 start 行开始算，直到 state 稳定才停
3. **跨行 state**：每行有个 `state` 指针指向行末所在的 region，传给下一行作为上下文
4. **include 嵌套**：region 的 `rules` 里可以 `include: "其他syntax"`，实现 syntax 嵌套

### 1.7 什么不变 / 什么会变

| 什么 | 静态 | 动态 |
|------|------|------|
| Syntax 文件 (yaml) | 静态（加载到 `Def`） | 一次解析后不变 |
| `Def` (patterns + regions 列表) | 静态 | 一次加载后不变 |
| 每行的 `state` | 动态 | 每次 MarkModified 重算受影响行 |
| 每行的 `match` | 动态 | 每次 MarkModified 重算受影响行 |
| colorscheme 映射 `group.String() → tcell.Style` | 静态 | reload 才变 |
| 屏幕上的实际颜色 | 动态 | 每次 getStyle 查询决定 |

### 1.8 用户视角的可见 vs 不可见

**可见**：
- 屏幕上的颜色（高亮结果）
- 滚动流畅度（增量传染保证）
- 自定义 yaml / colorscheme 改变高亮效果

**不可见**：
- `state` 指针（“行末在哪个 region”）
- `LineMatch` map 里实际打的点
- highlighter 在背后怎么传染

### 1.9 与 MicroNeo 的边界

MicroNeo **复用** micro 的 highlighter，**不重写**：

- **读**：`b.Match(L)[X]` 拿 group → `config.GetColor(group.String())` 拿 style
- **不修改**：pkg/highlight/ 下任何代码
- **不重复计算**：所有计算走 micro 的 event-driven 路径
- **不绕道**：不自己跑 yaml 解析，不自己维护一份 LineMatch

MicroNeo 只在 **渲染时** 插一脚：把 renderer 标的背景色 + micro highlighter 算的前景色合并后写入屏幕。

### 1.10 如何读这份文档

| 读者 | 推荐路径 |
|------|----------|
| 只看架构 | 读本章 §1.1–§1.9（跳过代码） |
| 要看实现 | §1.3 架构图 → §2 关键概念 → §3 打开时 → §4 编辑时 → §6 渲染时 |
| 要调试高亮问题 | §3.4 highlightEmptyRegion + §3.5 highlightRegion + §5.3 实际例子 |
| 要写自定义 syntax | §2.3 region + §5 include 机制 + §7 用户配置 vs 内置 |

### 1.11 几个容易记错的事实

- **micro 机制上完全支持 syntax 嵌套**（region + include），但**内置 markdown.yaml 没配**这个机制
- 用户**自定义** `~/.config/micro/syntax/markdown.yaml` 可以启用嵌套（如 ` ```python ` 块 include python）
- highlighter 是**状态机**，不是简单正则——多行 region 依赖 `state` 跨行传递
- highlighter 是**事件驱动**，不是定时器——编辑时同步算，屏上重绘时不算
- **渲染时**只看 `b.Match(L)[X]` 查 map，**不算**——不查 map 就只能拿 default 颜色

---

## 二、关键概念

### 2.1 Group（uint8 enum）

```go
// pkg/highlight/parser.go:13
type Group uint8

// Groups 是全局映射：group 名字 → Group enum
var Groups map[string]Group
```

每个 syntax 文件加载时，把所有用到的 group 名字登记到 `Groups` 里（递增分配 enum 值）：

```go
// pkg/highlight/parser.go
if _, ok := Groups[groupStr]; !ok {
    numGroups++
    Groups[groupStr] = numGroups
}
```

所以同一个 group 名字在不同 syntax 文件里可能对应不同 enum 值——但 `Group.String()` 通过反查 `Groups` map 拿到原名字。

### 2.2 Pattern（单行规则）

```go
// pkg/highlight/parser.go:63
type pattern struct {
    group Group
    regex *regexp.Regexp
}
```

Pattern 是**单行匹配**的规则，例如：

```yaml
- special: "^#{1,6}.*"      # 整行匹配标题
- type: ".*[ :]\\|[ :].*"    # 表格行
```

Pattern 在某一行内跑正则，匹配的位置被标为对应 group。

### 2.3 Region（多行规则，start/end 配对）

```go
// pkg/highlight/parser.go:81
type region struct {
    group      Group        // region 内字符的默认 group
    limitGroup Group        // 限制哪些 group 可被标（可选）
    parent     *region      // 父 region
    start      *regexp.Regexp
    end        *regexp.Regexp
    skip       *regexp.Regexp
    rules      *rules       // region 内的 rules（可独立 include）
}
```

Region 是**多行匹配**的规则，有 start 和 end 两个正则：

```yaml
- comment:
    start: "/\\*"
    end:   "\\*/"
    rules:
        - todo: "(TODO|XXX|FIXME):?"
```

匹配行为：
- 找到 `start` → 进入 region，state 设为这个 region
- 找到 `end` → 退出 region
- 区域内的字符继承 `region.group`（除非被内部 rules 覆盖）
- `skip` 规则用于忽略 `end`（如字符串里的转义引号）
- region 可以**嵌套**

### 2.4 State（行末状态）

```go
// pkg/highlight/highlighter.go:60
type State = *region

// LineStates 接口（buffer 实现）
type LineStates interface {
    State(lineN int) State
    SetState(lineN int, s State)
    ...
}
```

每行有个 `state` 字段，存的是"处理完这一行后处于哪个 region"：
- `nil`：不在任何 region 内
- 某个 `*region`：在该 region 内（end 还没匹配到）

**State 是 highlighter 跨行传递上下文的核心机制**。

### 2.5 LineMatch（高亮结果）

```go
// pkg/highlight/highlighter.go:83
type LineMatch map[int]Group
```

每行的高亮结果：key 是 rune offset，value 是 group enum。**只在 group 变化的位置打点**：

```go
// pkg/highlight/highlighter.go:172
for i, h := range fullHighlights {
    if i == 0 || h != fullHighlights[i-1] {  // ← 只在变化点打点
        highlights[start+i] = h
    }
}
```

举例 ` ```python ` 行（假设被某 region 整行标为 `default` group，且 `python` 没有特殊匹配）：
- 整行 init 为 default (group 0)
- python 没匹配任何 pattern → 保持 default
- map 里只有 `key=0`（变化点 i==0）

---

## 三、Buffer 打开时

### 3.1 启动入口

```go
// internal/buffer/buffer.go:1004
if b.SyntaxDef != nil {
    b.Highlighter = highlight.NewHighlighter(b.SyntaxDef)
    if b.Settings["syntax"].(bool) {
        go func() {                                  // ← 异步启动 goroutine
            b.Highlighter.HighlightStates(b)
            b.Highlighter.HighlightMatches(b, 0, b.End().Y)
            screen.Redraw()
        }()
    }
}
```

`SetSyntaxDef` 时启动一个 goroutine：
1. 全文算 state
2. 全文算 match
3. 触发屏幕重绘

**异步**——buffer 打开不阻塞。但渲染时如果 highlighter 还没跑完，`b.Match(L)` 返回空 map（getStyle fallthrough 到 default）。

### 3.2 HighlightStates（算每行 state）

```go
// pkg/highlight/highlighter.go:344
func (h *Highlighter) ReHighlightStates(input LineStates, startline int) int {
    h.lastRegion = nil
    if startline > 0 {
        h.lastRegion = input.State(startline - 1)  // ← 初始 state = 上一行
    }
    for i := startline; ; i++ {
        ...
        h.highlightRegion(nil, 0, true, i, line, h.lastRegion, true)
        curState := h.lastRegion
        lastState := input.State(i)
        input.SetState(i, curState)
        if curState == lastState {  // ← 传染停止条件
            return i
        }
    }
}
```

**关键点**：
- `curRegion = input.State(i-1)` —— 用上一行的 state 作为这一行的初始 region
- `highlightRegion(..., statesOnly=true)` 只算 state 不算 match
- `curState == lastState` 时停——state 稳定意味着传染结束

Buffer 打开时 `ReHighlightStates(0)`：从第 0 行开始，逐行传染，**算完整个 buffer**（直到 EOF）。

### 3.3 HighlightMatches（算每行 match）

```go
// pkg/highlight/highlighter.go:318
func (h *Highlighter) HighlightMatches(input LineStates, startline, endline int) {
    for i := startline; i <= endline; i++ {
        ...
        if i == 0 || input.State(i-1) == nil {
            // ← 上一行 state == nil（无 region），用 highlightEmptyRegion
            match = h.highlightEmptyRegion(highlights, 0, true, i, line, false)
        } else {
            // ← 上一行 state != nil（在 region 内），用 highlightRegion
            match = h.highlightRegion(highlights, 0, true, i, line, input.State(i-1), false)
        }
        input.SetMatch(i, match)
    }
}
```

**关键点**：
- 上一行 state == nil（无 region）→ `highlightEmptyRegion`：当前行用根 patterns 匹配
- 上一行 state != nil（在 region 内）→ `highlightRegion`：用 region 内的 rules 匹配
- 结果存到 `input.SetMatch(i, match)`

### 3.4 highlightEmptyRegion（无 region 上下文时）

```go
// pkg/highlight/highlighter.go:208
func (h *Highlighter) highlightEmptyRegion(...) LineMatch {
    // 1. 遍历所有 region 规则，看当前行是否进入 region
    for _, r := range h.Def.rules.regions {
        loc := findIndex(r.start, r.skip, line)
        if loc != nil && loc[0] < firstLoc[0] {
            firstLoc = loc
            firstRegion = r
        }
    }
    if firstRegion != nil {
        // 2. 进入 region：高亮 start 位置，递归处理 region 内和之后
        highlights[start+firstLoc[0]] = firstRegion.limitGroup
        h.highlightEmptyRegion(highlights, start, false, ..., sliceEnd(line, firstLoc[0]), ...)
        h.highlightRegion(highlights, start+firstLoc[1], ..., firstRegion, ...)
        return highlights
    }
    
    // 3. 没进 region：用根 patterns 匹配当前行
    fullHighlights := make([]Group, len(line))
    for _, p := range h.Def.rules.patterns {
        matches := findAllIndex(p.regex, line)
        for _, m := range matches {
            for i := m[0]; i < m[1]; i++ {
                fullHighlights[i] = p.group
            }
        }
    }
    if canMatchEnd {
        h.lastRegion = nil  // ← 结尾 state 回到 nil
    }
    return highlights
}
```

**关键点**：
- 先扫所有 region 的 start regex，看当前行是否**进入**某个 region
- 没进入：用根 `rules.patterns` 跑匹配，结尾 `h.lastRegion = nil`
- 进入了：递归处理（先跑 root patterns 标 start 之前部分，然后切到 region 内 rules）

### 3.5 highlightRegion（在 region 上下文时）

```go
// pkg/highlight/highlighter.go:115
func (h *Highlighter) highlightRegion(... curRegion *region, ...) LineMatch {
    // 1. 行首默认是 curRegion.group
    if start == 0 {
        if _, ok := highlights[0]; !ok {
            highlights[0] = curRegion.group
        }
    }
    
    // 2. 看当前行是否退出 region（end regex 命中）
    endLoc := findIndex(curRegion.end, curRegion.skip, line)
    
    // 3. 看当前行是否进入嵌套 region
    for _, r := range curRegion.rules.regions {
        loc := findIndex(r.start, curRegion.skip, line)
        if loc != nil && loc[0] < firstLoc[0] {
            firstRegion = r
        }
    }
    
    // 4. 用 curRegion.rules.patterns 标整行
    fullHighlights := make([]Group, lineLen)
    for i := 0; i < lineLen; i++ {
        fullHighlights[i] = curRegion.group  // ← 整行 init 为 region.group
    }
    for _, p := range curRegion.rules.patterns {
        if curRegion.group == curRegion.limitGroup || p.group == curRegion.limitGroup {
            matches := findAllIndex(p.regex, line)
            for _, m := range matches {
                for i := m[0]; i < m[1]; i++ {
                    fullHighlights[i] = p.group
                }
            }
        }
    }
    
    // 5. 处理 end 退出 / 嵌套 region 进入
    if endLoc != nil {
        highlights[start+endLoc[0]] = curRegion.limitGroup
        // 退出 region
        if curRegion.parent == nil {
            highlights[start+endLoc[1]] = 0
            h.highlightEmptyRegion(highlights, start+endLoc[1], ..., sliceStart(line, endLoc[1]), ...)
        } else {
            highlights[start+endLoc[1]] = curRegion.parent.group
            h.highlightRegion(highlights, start+endLoc[1], ..., curRegion.parent, ...)
        }
    }
    return highlights
}
```

**关键点**：
- 整行 init 为 `curRegion.group`（region 内的字符默认是这个 group）
- 用 `curRegion.rules.patterns` 标某些字符（如 region 内嵌套的 include 的 patterns）
- end 匹配到 → 退出 region，state 回到父 region 或 nil
- limitGroup 机制：只有 `region.group == limitGroup` 或 `pattern.group == limitGroup` 的 pattern 才生效（用于限制 region 内的颜色种类）

---

## 四、用户编辑时

### 4.1 MarkModified 触发

```go
// internal/buffer/buffer.go:185
func (b *SharedBuffer) MarkModified(start, end int) {
    b.ModifiedThisFrame = true
    start = util.Clamp(start, 0, len(b.lines)-1)
    end   = util.Clamp(end,   0, len(b.lines)-1)
    if b.Settings["syntax"].(bool) && b.SyntaxDef != nil {
        l := -1
        for i := start; i <= end; i++ {
            l = util.Max(b.Highlighter.ReHighlightStates(b, i), l)
        }
        b.Highlighter.HighlightMatches(b, start, l)
    }
    ...
}
```

**每次 buffer 修改（insert/remove）**：
1. **同步**调用 `ReHighlightStates(start)` —— 从 start 行开始传染算 state，直到 state 稳定
2. **同步**调用 `HighlightMatches(start, l)` —— 重算 [start, l] 区间内每行的 match

### 4.2 传染停止条件

```go
// pkg/highlight/highlighter.go:344
for i := startline; ; i++ {
    h.highlightRegion(nil, 0, true, i, line, h.lastRegion, true)
    curState  := h.lastRegion         // 当前新算的 state
    lastState := input.State(i)        // 之前存的 state
    input.SetState(i, curState)
    if curState == lastState {
        return i   // ← 传染停止
    }
}
```

**传染语义**：
- 从 startline 开始
- 用上一行 state 算当前行 state
- 当前行新算的 state 跟修改前一样 → 停止

**例子**：
- 普通段落里改一字符：state 不会传到下一行，1-2 行就稳定 → 停
- 代码块里改一字符：从 ` ```python ` 开始传染，到下一个 ` ``` ` 结束，state 切回 nil 才停
- 跨结构编辑（删了 ` ``` ` 围栏）：传染可能影响更多行

### 4.3 完整时序图

```
[Buffer 打开]
   └─ SetSyntaxDef
       └─ go { HighlightStates(全文) → HighlightMatches(0, End.Y) → screen.Redraw() }
                                                          ↑ 跑完触发重绘

[用户编辑: insert/remove]
   └─ MarkModified(start, end)
       ├─ ReHighlightStates(start)
       │   └─ 从 start 传染到 state 稳定的那行 (l)
       └─ HighlightMatches(start, l)        ← 同步重算区间内的 match

[用户不动]
   └─ 不跑 ← 重要：没有定时器

[屏幕重绘]
   └─ displayBuffer() → getStyle(bloc) → b.Match(bloc.Y)[bloc.X]
                                                  ↑
                                          纯查询，不算
```

---

## 五、Include 机制（嵌套 syntax）

### 5.1 机制原理

```go
// pkg/highlight/parser.go (ResolveIncludes)
func resolveIncludesInDef(files []*File, d *Def) {
    for _, lang := range d.rules.includes {
        for _, searchFile := range files {
            if lang == searchFile.FileType {
                searchDef, _ := ParseDef(searchFile, nil)
                d.rules.patterns = append(d.rules.patterns, searchDef.rules.patterns...)
                d.rules.regions  = append(d.rules.regions,  searchDef.rules.regions...)
            }
        }
    }
}
```

`include: "python"` 把 python.yaml 的所有 patterns 和 regions **追加**到当前 rules 里。

### 5.2 在 region 内 include

最有用的形式是 **region 内 include**：

```yaml
# runtime/syntax/html.yaml 的官方例子
- default:
    start: "<script.*?>"
    end:   "</script.*?>"
    rules:
        - include: "javascript"   # <script> 块内用 javascript 规则
```

行为：
- 进入 `<script>` region → state 设为这个 region
- region 内字符继承 `region.group`（即 `default`）
- region 内跑 include 的 javascript.yaml 的 patterns
- 遇到 `</script>` → 退出 region，state 回到 nil

### 5.3 用户 markdown 配置中的实际例子

`~/.config/micro/syntax/markdown.yaml`：

```yaml
# Code blocks (fenced) - Python
- default:
    start: "^(\`{3,}|~{3,})\\s*python"
    end:   "^(\`{3,}|~{3,})"
    rules:
        - include: "python"

# Code blocks (fenced) - other (default)
- constant.string:
    start: "^(\`{3,}|~{3,})"
    end:   "^(\`{3,}|~{3,})"
    skip: "^\\s*$"
```

行为追踪（` ```python ` 块）：

```
L10: "```python"
    L10 的 state=L9 的 state=nil → highlightEmptyRegion
    扫所有 region.rules.regions，匹配 `^```python` 的 start
    → 进入"Code blocks (fenced) - Python"这个 region
    → L10 的 state 设为这个 region
    
L11: "def foo():"
    L11 的 state=L10 的 state=上面那个 region → highlightRegion
    整行 init 为 region.group=default (0)
    region.rules.patterns = [python.yaml 的所有 patterns]
    python.yaml 跑匹配：`def` 标 identifier，`foo` 标 identifier，`(` `)` 标 symbol.brackets
    end 也没匹配到（不是 ` ``` `）→ 不退出 region
    → L11 的 state 还是这个 region
    
L12-L18: 代码块内，同 L11
    
L19: "```"
    L19 的 state=L18 的 state=上面那个 region → highlightRegion
    整行 init 为 default
    end regex `^\`\`\`` 匹配 → 退出 region
    highlights[0] = curRegion.limitGroup（如果有 limitGroup）
    highlights[3] = 0 (回到根)
    → L19 的 state = nil
    
L20: 普通段落
    L20 的 state=L19 的 state=nil → highlightEmptyRegion
    ...
```

**所以** ` ```python ` 块内字符的 group 来自 **python.yaml**，而不是 markdown.yaml。这就是用户看到的"python 颜色"。

` ```go ` 块（go 不在前 3 个 region 白名单）：
- 匹配"Code blocks (fenced) - other"region
- 这个 region 的 group = `constant.string`（绿色）
- region.rules.patterns 是空的（没 include 任何东西）
- 整块字符都继承 `constant.string`
- 用户的 colorscheme 把 `constant.string` 配成绿色
- → 整块绿色

---

## 六、渲染时

### 6.1 查 match

```go
// internal/display/bufwindow.go:368
func (w *BufWindow) getStyle(style tcell.Style, bloc buffer.Loc) (tcell.Style, bool) {
    if group, ok := w.Buf.Match(bloc.Y)[bloc.X]; ok {
        s := config.GetColor(group.String())
        return s, true
    }
    return style, false
}
```

**纯查 map**：
- `b.Match(lineY)` 返回 `LineMatch`（即 `map[int]Group`）
- `map[colX]` 拿 group
- `group.String()` 拿 group 名字
- `config.GetColor(name)` 拿 colorscheme 里配的 `tcell.Style`

### 6.2 getColor

```go
// internal/config/colorscheme.go:19
func GetColor(color string) tcell.Style {
    st := DefStyle
    if color == "" {
        return st
    }
    // 支持 subgroup: constant.string → constant.string > constant
    groups := strings.Split(color, ".")
    for i, g := range groups {
        if i != 0 {
            curGroup += "."
        }
        curGroup += g
        if style, ok := Colorscheme[curGroup]; ok {
            st = style
        }
    }
    return st
}
```

支持 subgroup fallback：`constant.string.bool.true` → 找不到就试 `constant.string.bool` → 找不到就试 `constant.string` → 找不到就试 `constant` → 找不到用 DefStyle。

### 6.3 完整渲染流程

```
displayBuffer()
  ↓
  bloc.Y = 当前 buffer 行
  bloc.X = 当前 buffer 列
  ↓
  curStyle, _ = getStyle(curStyle, bloc)
  ↓
  b.Match(bloc.Y)[bloc.X] → Group
  ↓
  config.GetColor(group.String()) → tcell.Style
  ↓
  screen.SetContent(x, y, r, combc, style)
```

---

## 七、用户配置 vs 内置配置

### 7.1 加载顺序

Micro 加载 syntax 文件的顺序：
1. 用户配置：`~/.config/micro/syntax/<filetype>.yaml`（如果存在）
2. 内置配置：`runtime/syntax/<filetype>.yaml`

**用户配置优先**。如果用户配置存在，**完全替换**内置。

### 7.2 内置 markdown.yaml 的真实能力

```yaml
# runtime/syntax/markdown.yaml 全部相关规则
- special: "^\`\`\`$"    # 普通 pattern，不是 region
- special:               # 行内代码 ` ` 的 region
    start: "\`"
    end:   "\`"
    rules: []
```

**内置 markdown.yaml 没给 ` ``` ` 配 region**。所以：
- ` ``` ` 围栏行被标 `special`（普通 pattern 匹配）
- 代码块内字符**没有任何特殊 group**（用 default）
- 跨行的代码块 state **永远是 nil**（因为没 region 可进）

### 7.3 用户配置可以增强

通过用户配置加 region 规则：

```yaml
# ~/.config/micro/syntax/markdown.yaml
- default:
    start: "^(\`{3,}|~{3,})\\s*python"
    end:   "^(\`{3,}|~{3,})"
    rules:
        - include: "python"
```

**机制完全支持**。只是内置配置**没用**。

---

## 八、关键代码位置一览

| 文件 | 行号 | 内容 |
|------|------|------|
| `pkg/highlight/highlighter.go` | 75 | `NewHighlighter` |
| `pkg/highlight/highlighter.go` | 83 | `type LineMatch` |
| `pkg/highlight/highlighter.go` | 115 | `highlightRegion` |
| `pkg/highlight/highlighter.go` | 208 | `highlightEmptyRegion` |
| `pkg/highlight/highlighter.go` | 272 | `HighlightString` |
| `pkg/highlight/highlighter.go` | 318 | `HighlightMatches` |
| `pkg/highlight/highlighter.go` | 344 | `ReHighlightStates` |
| `pkg/highlight/parser.go` | 13 | `type Group` |
| `pkg/highlight/parser.go` | 63 | `type pattern` |
| `pkg/highlight/parser.go` | 81 | `type region` |
| `pkg/highlight/parser.go` | 339 | `parseRules` |
| `pkg/highlight/parser.go` | 412 | `parseRegion` |
| `internal/buffer/buffer.go` | 1006 | 打开 buffer 时启动高亮 goroutine |
| `internal/buffer/buffer.go` | 185 | `MarkModified` 触发增量高亮 |
| `internal/buffer/line_array.go` | 355 | `Match` 函数（查 map） |
| `internal/display/bufwindow.go` | 370 | `getStyle`（渲染时查 map） |
| `internal/config/colorscheme.go` | 19 | `GetColor` |
| `runtime/help/colors.md` | 308-321 | Includes 章节（官方文档） |

---

## 九、容易混淆的概念对照

| 概念 | 解释 |
|------|------|
| `LineMatch` map | 每行的 `rune_offset → Group`，存的是**结果** |
| `Highlighter.HighlightMatches` | 算 match 的**过程**，把结果写入 map |
| `ReHighlightStates` | 算 state 的**过程**（先算 state 再算 match） |
| `b.Match(L)` | 读 map 的**查询**（渲染时用） |
| `state`（`*region`） | "行末处于哪个 region" 的指针，跨行传递上下文 |
| `pattern` | 单行规则，匹配位置标 group |
| `region` | 多行规则，start/end 配对，跨行有 state |
| `limitGroup` | region 限制只允许某些 group 出现（可选） |
| `include: "X"` | 把 X.yaml 的 patterns 和 regions 追加到当前 rules |
| `root region` | 不在任何 region 内时用的"虚拟根" |

---

## 十、与 MicroNeo 的关系

MicroNeo 复用 micro 的 highlighter 结果（不重写）：
- **mergeStyle** 在 `displayBufferMD()` 写入 screen 前，调用 `b.Match(L)[X]` 查 group
- 背景色用 renderer 的，前景色用 micro 的
- 用户自定义 markdown.yaml（如本文 §5.3）会被自动识别——` ```python ` 块内字符的 group 来自 python.yaml，mergeStyle 会用 python 颜色

MicroNeo **不修改** micro highlighter 的代码，只读其结果。
