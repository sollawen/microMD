# notePane EABP 发送端（D4）

> 本文档是把 notePane 改造为 EABP 发送端的**设计决策文档**。
> D3 验证了协议端到端可行；D4 把命令行原型（`proto/eabp-sender/`）内建进编辑器，notePane 成为用户侧唯一入口。
>
> **实现代码的修改按项目规则先请求许可。**

---

## 一、背景与动机

M1 联调已通：`proto/bin/send` 向 pi receiver 发报文，pi 端 notify 正确显示。但命令行工具不是用户会用的东西——用户在编辑器里工作，触发应该跟呼吸一样自然。

notePane 已经是一个可编辑的浮动面板（`Alt-i` 唤起），有完整的 BufPane 编辑能力。把它变成 EABP 发送端是最小改造路径：不新建 UI 组件，不改主编辑器交互流，只给 notePane 加一个"寄出"动作。

## 二、核心决策

### 2.1 上下文捕获时机：打开时读取

打开 notePane 时，一次性从主编辑器读取 path / cursor / selection，存入 `NotePane` 结构体的字段。

这是自然的选择——notePane 打开后主编辑器的键盘/鼠标事件已被阻断（micro.go 事件路由：notePane 开着时键盘/鼠标事件全给它，主编辑器收不到），所以打开时的状态就是发送时的状态。在 `open()` 里读并存字段，只是让代码意图显式化。

### 2.2 发送触发：Alt+Enter

notePane 当前 `Enter` = `InsertNewline`（多行编辑）。`Alt+Enter` 触发发送。

micro 已支持 Alt 修饰键（`Alt-i` 唤起 notePane 就是先例），所以 `Alt+Enter` 的绑定机制是通的。唯一不确定的是终端是否给 Alt+Enter 发出唯一 escape sequence——实测确认。

如果 Alt+Enter 不可用，候选：`Alt+s`（send）。

**为什么不用 Enter = 发送 / Shift+Enter = 换行**：notePane 是编辑器面板，用户习惯 `Enter` 换行，改掉会破坏已有肌肉记忆。

### 2.3 发送后行为：关闭 + InfoBar 提示

发送成功 → 关闭 notePane（保存笔记）→ InfoBar 显示一行确认。失败 → 不关闭，InfoBar 显示错误，用户可以修改后重试。

### 2.4 发送目标：单 receiver（MVP）

发送时 discover 所有存活 receiver。如果恰好只有一个——直接发。如果有多个或没有——InfoBar 报错，用户稍后可扩展为选择。

这是 MVP 路径。多 receiver 选择是产品功能，不属于协议层，后面加。

## 三、接口契约

### 3.1 NotePane 新增字段

```
filePath           string     // 主编辑器 buffer.AbsPath
fileCursor         Loc        // 主编辑器活跃光标（0-based Y,X）
fileSelection      *SelRange  // nil = 无选区；有 = start/end（0-based）
```

`SelRange` 不需要新类型——直接用 buffer 的 `[2]Loc`。

### 3.2 上下文读取点

`open()` 调用 `lowestCursorScreenRow()` 遍历所有 cursor 找最低位置。EABP 只关心最低光标——正好复用这个循环，在更新 `lowestRow` 时顺便记录：

```go
// lowestCursorScreenRow 里，现有逻辑：
for _, cursor := range bw.Buf.GetCursors() {
    // ... 现有的 loc 计算不变 ...
    screenRow := n.locToScreenRow(bw, view, loc)
    if screenRow > lowestRow {
        lowestRow = screenRow
        // ← 顺手记录
        n.fileCursor = loc
        if cursor.HasSelection() {
            sel := cursor.CurSelection  // [2]Loc
            n.fileSelection = &sel
        } else {
            n.fileSelection = nil
        }
    }
}
```

`filePath` 不在循环里赋值——notePane 打开时主编辑器只有一个 buffer，放在 `open()` 里 `pane` 拿到之后赋一次即可：`n.filePath = pane.Buf.AbsPath`。循环里只记 cursor 和 selection。

### 3.3 发送动作

新增 action `NotePaneSend`，注册到 `allowedNotePaneActions` 白名单。行为：

1. 读取 notePane buffer 全文作为 `message`
2. `eabp.Discover()` 找存活 receiver（零个或多个 → InfoBar 报错，结束；恰好一个 → 继续）
3. 拼装 payload 并序列化为一行 JSON（转换细节见下）
4. dial receiver 的 unix socket，写入 JSON 行，关闭连接
5. 成功 → `close()` notePane + `InfoBar.Message("✓ sent to <name>")`
6. 失败 → `InfoBar.Message("✗ send failed: <err>")`，不关闭

**坐标转换（存储用 Loc，发送时转 EABP Position）**：

`buffer.Loc` 是 `{X, Y}` = `{col, row}`；EABP `Position` 是 `{Line, Col}` = `{row, col}`。字段顺序相反，发送时必须显式转换：
```go
cursor := eabp.Position{Line: n.fileCursor.Y, Col: n.fileCursor.X}
```

**选区归一化 + 选区文本**：

`CurSelection` 可能是反向选择（用户从右往左选），发送前归一化为 start ≤ end：
```go
start, end := n.fileSelection[0], n.fileSelection[1]
if start.GreaterThan(end) {
    start, end = end, start
}
// 选区文本：从 buffer 取，超 2KB 省略（D2 §5.3）
selText := pane.Buf.Substr(start, end)  // []byte
if len(selText) > 2048 { selText = nil }
```

**visible_lines**：MVP 不实现（payload 该字段 omitempty，省略即可）。

**sender 字段**：`pid` = `os.Getpid()`，`name` = `"microNeo"`，`instance` = MVP 固定 `"default"`（microNeo 当前无实例/窗口 ID 概念，后续再设计）。

**payload 合法性校验**：不需要显式校验。上下文来自编辑器自身状态（cursor.Loc、CurSelection），天然满足 D2 §5.3 的非负/不超界约束。

### 3.4 快捷键绑定

`AltEnter` 绑定到 `NotePaneSend`。micro 已有 Alt 修饰键的解析路径（`Alt-i` 先例），注册方式相同。

## 四、代码放置

### 4.1 EABP 核心类型

`proto/eabp-sender/go/` 里的 `message.go` 和 `registry.go` 搬到 `internal/eabp/`。

这是 D3 就计划好的路径——原型验证完后，生产代码进 microNeo 内部包。搬迁不是纯 `git mv`：`internal/eabp/` 是新包路径，需要改 package 声明、改 import 路径（从 `eabp-proto` 改为 `github.com/micro-editor/micro/v2/internal/eabp`）。

`cmd/discover` 和 `cmd/send` 保留在 `proto/` 作为调试工具，不进主线。

### 4.2 NotePane 改动

只改 `internal/action/notepane.go`。改动清单：

- `NotePane` 结构体加 3 个字段（§3.1）
- `open()` 方法加上下文读取（§3.2）
- 新增 `NotePaneSend` action 函数（§3.3）
- `allowedNotePaneActions` 加 `"NotePaneSend": true`
- 绑定 `AltEnter` → `NotePaneSend`

### 4.3 不改的东西

| 不改 | 原因 |
|------|------|
| 主编辑器事件流 | notePane 从主编辑器读数据，但不监听、不注入 |
| D2 协议 | payload 格式不变，notePane 只是另一个 sender |
| notePane 的 BufPane 行为 | 编辑、持久化、白名单机制原样保留 |
| pi receiver | 对 receiver 来说，报文来自命令行还是编辑器没有区别 |

## 五、风险

| 风险 | 等级 | 缓解 |
|------|------|------|
| `Alt+Enter` 终端不传递唯一 escape sequence | 低 | fallback 到 `Alt+s` |
| Discover 阻塞（socket probe 200ms × N 个 receiver） | 低 | notePane 发送是用户主动触发，200ms 可接受；后续可异步 |
| 多 receiver 场景 | 无（MVP 不支持） | MVP 报错提示；选择 UI 是产品功能，后续迭代 |
| `internal/eabp/` 搬入影响 `make build` | 低 | 纯新增包，不改已有包；`make build-quick` 先验 |

## 六、验证

```bash
# 1. 搬 code + 改 import
make build-quick    # 编译通过

# 2. 启动 pi（带 eabp-pi 扩展）
# 确认注册表落地（已有 receiver-pi-<pid>.json）

# 3. 启动 microNeo，打开一个文件，选一段文字
# Alt-i 打开 notePane
# 输入 "测试 D4"
# Alt+Enter → 发送

# 4. 预期
#    - notePane 关闭
#    - InfoBar 显示 "✓ sent to pi-<pid>"
#    - pi 端 notify 显示上下文 + "测试 D4"
```

## 七、执行顺序

1. `git mv proto/eabp-sender/go/message.go proto/eabp-sender/go/registry.go → internal/eabp/`，改包路径
2. `make build-quick` 验编译
3. 改 `notepane.go`：加字段、加 `NotePaneSend`、加绑定
4. `make build-quick` 验编译
5. 联调测试（§六）
6. `make build`（含 generate）最终验证
