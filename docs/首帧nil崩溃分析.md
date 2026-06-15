# 首帧 nil 崩溃分析（viewportRowBufLine）

> 关联：`docs/C3-光标滚动-最小修复.md` §6.2 时序正确性的补充。
> 这是 C3 实现落地时暴露的、原方案未覆盖的一个时段。

---

## 1. 现象

`./microneo docs/sample.md` 启动瞬间崩溃：

```
runtime.boundsError runtime error: index out of range [3] with length 0
bufwindow_md.go:864  mdScrollMargins: viewportRowBufLine[3]
bufwindow.go:251     Relocate
bufwindow.go:109     Resize
bufpane.go:305       BufPane.Resize
tab.go:95            TabList.HandleEvent (初始 resize 事件)
micro.go:476         启动
```

## 2. 直接原因

`viewportRowBufLine` 是 nil（长度 0）时，访问了 `viewportRowBufLine[scrollmargin]`（默认 `scrollmargin = 3`）。

C3 §3.2 原守卫 `if scrollmargin >= height` 防不住这个 case：
- `height`（来自 `w.bufHeight`）是正常整数
- nil 的是 `viewportRowBufLine` 这个 slice
- 旧守卫判的是 `scrollmargin` vs `height`，不是 `scrollmargin` vs `len(viewportRowBufLine)`

## 3. nil 的场景：窗口创建后、第一次 Display 之前

### 3.1 数据构建时机

`viewportRowBufLine` 字段定义在 `bufwindow.go:41`，零值为 nil，`NewBufWindow`（`bufwindow.go:46`）**没有预分配**。

全代码库唯一给它 `make([]int, ...)` 的地方：

```go
// bufwindow_md.go:706-709（displayBufferMD 开头）
if cap(w.viewportRowBufLine) < bufHeight {
    w.viewportRowBufLine = make([]int, bufHeight)
}
w.viewportRowBufLine = w.viewportRowBufLine[:bufHeight]
```

而 `displayBufferMD` 只在 `Display()` 里被调用（`bufwindow.go:944`）。

**→ `viewportRowBufLine` 第一次被分配，是在第一次 `Display()` 执行期间。第一次 `Display()` 之前，它是 nil。**

### 3.2 启动时序

```go
// micro.go:465-479
select {
case event := <-screen.Events:
    action.Tabs.HandleEvent(event)   // 第一个事件 = 初始 resize
case <-time.After(10 * time.Millisecond):
}
for {
    DoEvent()                        // Display 在这里面才发生
}
```

调用链：

```
终端连上 → 发送 EventResize
  → TabList.HandleEvent (tab.go:95)
    → Resize → BufPane.Resize (bufpane.go:305)
      → BufWindow.Resize (bufwindow.go:109)
        → Relocate           ← 💥 此时 Display() 还没跑过
          → mdScrollMargins: viewportRowBufLine[3]  越界
```

启动时第一个事件就是 resize（终端报告尺寸）。处理它 → `Resize → Relocate`。但 `Display()` 在主循环 `DoEvent()` 里，**还没进入**。

## 4. nil 时段的范围

`viewportRowBufLine` 为 nil 的时段：**`BufWindow` 创建之后、第一次 `Display()` 之前**。

在这个时段里能触发 `Relocate` 的路径：
- `bufwindow.go:109`（`Resize` 内）—— **首帧 resize 触发，本次崩溃点**
- 其余 90+ 处 `Relocate` 调用都在 `actions.go`（光标移动后），都发生在主循环里、首次 `Display()` 之后，那时 slice 已被填充，安全。

**结论：唯一会踩 nil 的场景是"打开 MD 文件（或创建新窗口）后的第一个 resize 事件"。** 一旦首次 `Display()` 跑完，slice 被填充，后续 resize 只是按新尺寸重填，不会回到 nil。

## 5. 为什么 C3/C0 的时序分析没覆盖

C0 §4.1 / C3 §6.2 讨论的时序坑是：

> "`viewportRowBufLine` 数据**陈旧**"（上一帧的旧数据被这一帧用）

这个论证的前提是"它**至少被构建过一次**"。

而本次崩溃是：

> "`viewportRowBufLine` 数据**不存在**"（nil，一次都没构建过）

是更早阶段的问题。C3 §6.2 的"按下键时视口没变，映射没变"论证，只在 `displayBufferMD` 至少跑过一次之后成立，**完全没覆盖"首次 Display 之前"这个时段**。

## 6. 修法

C3 §3.2 的守卫从判 `height` 改为判**真实的 `len(viewportRowBufLine)`**，每个索引访问前单独做边界检查：

```go
func (w *BufWindow) mdScrollMargins(scrollmargin, height int) (topMargin, botMargin SLoc) {
    topMargin = w.Scroll(w.StartLine, scrollmargin)
    botMargin = w.Scroll(w.StartLine, height-1-scrollmargin)
    n := len(w.viewportRowBufLine)
    if n == 0 {                              // 首帧/Resize：尚未构建 → 用 fallback
        return
    }
    if scrollmargin < n {
        if t := w.viewportRowBufLine[scrollmargin]; t >= 0 {
            topMargin = SLoc{t, 0}
        }
    }
    bot := height - 1 - scrollmargin
    if bot >= 0 && bot < n {
        if t := w.viewportRowBufLine[bot]; t >= 0 {
            botMargin = SLoc{t, 0}
        }
    }
    return
}
```

核心变化：判真实长度 `len()` 而非假设的 `height`；每个索引访问前单独判 `< n`。只改这一个函数，`Relocate` 的分发缝不动。

**首帧 nil 时回退原生 `Scroll` 的行为是正确的**：首帧 resize 的滚动判定本来就该用 1:1 原生逻辑——那时还没有任何 MD 渲染发生，谈不上装饰行偏移。
