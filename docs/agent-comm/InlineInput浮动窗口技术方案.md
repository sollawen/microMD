# InlineInput 浮动窗口技术方案

## 需求概述

用户编辑文件时按快捷键，在**当前光标位置**显示一个浮动窗口：
- 窗口内可输入内容，支持 micro 的各种编辑功能
- 再按一次快捷键，窗口关闭，内容自动存盘到主缓冲区

---

## 一、Micro 现有架构分析

### 1.1 核心组件关系

```
TabList
  └── Tab
        ├── Node (views.Node - 布局管理)
        ├── UIWindow (边框绘制)
        └── Panes[] (Pane 接口)
              ├── BufPane (主编辑面板)
              │     ├── BufWindow (显示层 - display.BWindow)
              │     └── Buffer (数据层)
              ├── TermPane (终端面板)
              └── InfoPane (命令栏)
```

### 1.2 关键接口定义

**display.Window 接口** (`internal/display/window.go`):
```go
type Window interface {
    Display()
    Clear()
    Relocate() bool
    GetView() *View
    SetView(v *View)
    LocFromVisual(vloc buffer.Loc) buffer.Loc
    Resize(w, h int)
    SetActive(b bool)
    IsActive() bool
}

type BWindow interface {
    Window
    SetBuffer(b *buffer.Buffer)
    BufView() View
}
```

**action.Pane 接口** (`internal/action/pane.go`):
```go
type Pane interface {
    Handler
    display.Window
    ID() uint64
    SetID(i uint64)
    Name() string
    Close()
    SetTab(t *Tab)
    Tab() *Tab
}
```

### 1.3 事件处理流程

主循环 (`cmd/micro/micro.go`):
```go
func DoEvent() {
    // 显示
    action.Tabs.Display()
    action.InfoBar.Display()
    
    // 事件分发
    if action.InfoBar.HasPrompt {
        action.InfoBar.HandleEvent(event)  // 命令栏优先
    } else {
        action.Tabs.HandleEvent(event)     // 正常转发给 Tab
    }
}
```

---

## 二、Lua 插件系统能力分析

### 2.1 Lua 可用 API

| 功能 | 可用性 | 说明 |
|------|--------|------|
| `micro.CurPane()` | ✅ | 获取当前面板 |
| `micro.Tabs()` | ✅ | 获取 TabList |
| `micro.InfoBar()` | ✅ | 获取 InfoBar |
| `config.MakeCommand()` | ✅ | 创建命令 |
| `config.TryBindKey()` | ✅ | 绑定快捷键 |
| `micro/buffer.NewBuffer()` | ✅ | 创建缓冲区 |
| `config.SetGlobalOption()` | ✅ | 设置选项 |
| **NewBufPane()** | ❌ | 未暴露给 Lua |
| **NewBufWindow()** | ❌ | 未暴露给 Lua |
| **窗口定位 API** | ❌ | 未暴露给 Lua |

### 2.2 结论

**Lua 无法实现 InlineInput 功能**，原因：
1. 无法创建新的 BufWindow/BufPane
2. 无法控制窗口位置和大小
3. 无法实现独立的窗口叠加层

---

## 三、现有覆盖层机制分析

### 3.1 InfoBar 实现

InfoBar 是 micro 唯一的覆盖层机制（单行固定在底部）：

```go
// internal/action/infopane.go
type InfoPane struct {
    *BufPane      // 嵌入编辑能力
    *info.InfoBuf // 输入缓冲
}

func NewInfoBar() *InfoPane {
    ib := info.NewBuffer()
    w := display.NewInfoWindow(ib)
    return NewInfoPane(ib, w, nil)  // 无 Tab关联
}
```

**事件优先路由**：
```go
if action.InfoBar.HasPrompt {
    action.InfoBar.HandleEvent(event)  // 拦截事件
} else {
    action.Tabs.HandleEvent(event)     // 正常流程
}
```

### 3.2 InfoWindow定位逻辑

```go
func NewInfoWindow(b *info.InfoBuf) *InfoWindow {
    w, h := screen.Screen.Size()
    iw.Width, iw.Y = w, h  // 固定底部
    iw.Y--
    return iw
}
```

---

## 四、InlineInput 实现方案

### 4.1 设计原则

1. **最小侵入**：不修改 micro 原生代码
2. **独立组件**：新建文件 `internal/action/inlineinput.go`
3. **参考 InfoBar**：复用其事件路由和 Prompt机制

### 4.2 组件结构

```
InlineInput
  ├── BufWindow (位置可变的窗口)
  ├── Buffer (输入缓冲)
  ├── active (激活状态)
  └── onClose callback (存盘回调)
```

### 4.3 核心接口设计

```go
// internal/action/inlineinput.go

type InlineInput struct {
    win *display.BufWindow      // 可定位的窗口
    buf   *buffer.Buffer         // 输入缓冲
    active bool
    onClose func(content string) // 关闭时回调
}

func NewInlineInput(x, y, width, height int) *InlineInput

// 窗口控制
func (i *InlineInput) Show()       // 显示窗口
func (i *InlineInput) Hide()       // 隐藏窗口
func (i *InlineInput) Toggle()     // 切换显示/隐藏
func (i *InlineInput) IsActive() bool

// 事件处理
func (i *InlineInput) HandleEvent(event tcell.Event) bool

// 内容操作
func (i *InlineInput) GetContent() string
func (i *InlineInput) SetContent(content string)
```

### 4.4 光标位置计算

```go
func CalcInputWindowPosition(bp *BufPane) (x, y, width, height int) {
    //1. 获取当前光标的屏幕坐标
    cur := bp.GetActiveCursor()
    curSloc := bp.SLocFromLoc(cur.Loc)
    
    // 2. 获取视口信息
    view := bp.BufView()
    screenW, screenH := screen.Screen.Size()
    
    // 3. 计算相对位置
    relY := curSloc.Line - view.StartLine.Line
    
    // 4. 计算窗口位置（以光标为左上角）
    x = view.X + cur.GetVisualX()
    y = view.Y + relY
    
    // 5. 确定窗口大小（可配置）
    width =60  // 默认宽度
    height = 10 // 默认高度
    
    // 6. 边界检测
    if x + width > screenW {
        x = screenW - width
    }
    if y + height > screenH {
        y = screenH - height
    }
    
    return x, y, width, height
}
```

### 4.5 事件路由

```go
func (i *InlineInput) HandleEvent(event tcell.Event) bool {
    if !i.active {
        return false
    }
    
    switch e := event.(type) {
    case *tcell.EventKey:
        switch e.Key() {
        case tcell.KeyEscape:
            i.Close()
            return true
        case tcell.KeyEnter:
            i.Close()
            return true
        default:
            // 传递给 BufPane 处理
            return i.bufPane.HandleEvent(event)
        }
    }
    
    return i.bufPane.HandleEvent(event)
}
```

### 4.6 主循环集成

```go
// cmd/micro/micro.go (修改)

func DoEvent() {
    // 显示
    if InlineInputInstance != nil && InlineInputInstance.IsActive() {
        InlineInputInstance.Display()  // 在主内容之上显示
    }
    action.Tabs.Display()
    action.InfoBar.Display()
    screen.Screen.Show()
    
    // 事件分发
    if InlineInputInstance != nil && InlineInputInstance.IsActive() {
        if InlineInputInstance.HandleEvent(event) {
            return  // 已被处理
        }
    }
    
    if action.InfoBar.HasPrompt {
        action.InfoBar.HandleEvent(event)
    } else {
        action.Tabs.HandleEvent(event)
    }
}
```

---

## 五、快捷键绑定

### 5.1 默认绑定

```go
// internal/action/defaults.go
func init() {
    // 默认快捷键 Ctrl-i触发 InlineInput
    AddBindWithAlt("Ctrl-i", "InlineInput")
}
```

### 5.2 Lua 暴露

```go
// cmd/micro/initlua.go
func luaImportMicro() *lua.LTable {
    pkg := ulua.L.NewTable()
    
    // 新增
    ulua.L.SetField(pkg, "ToggleInlineInput", luar.New(ulua.L, action.ToggleInlineInput))
    
    return pkg
}
```

Lua 使用：
```lua
config.TryBindKey("Ctrl-i", "lua:myplugin.inlineInput", true)

function inlineInput(bp)
    micro.ToggleInlineInput()
    return true
end
```

---

## 六、实现步骤

### 阶段一：基础框架
1. 新建 `internal/action/inlineinput.go`
2. 实现 InlineInput 结构体
3. 实现 Show/Hide/Toggle 基本功能
4. 实现基础的窗口显示

### 阶段二：事件处理
1. 集成到主循环
2. 实现键盘事件处理
3. 实现 Escape/Enter 关闭逻辑

### 阶段三：高级功能
1. 光标位置计算
2. 窗口边界检测
3. 边框和样式

### 阶段四：存盘功能
1. 实现 onClose 回调
2. 内容插入主缓冲区
3. 支持多种插入模式（替换/追加）

### 阶段五：Lua 集成
1. 暴露 Toggle API
2.绑定默认快捷键
3. 文档编写

---

## 七、风险与注意事项

### 7.1 性能
- 每次事件循环都需要检查 InlineInput 状态
- 解决方案：使用 `IsActive()` 快速短路

### 7.2 边界情况
- 光标在屏幕边缘
- 屏幕尺寸较小
- 多显示器场景
- 解决方案：完善的边界检测逻辑

### 7.3 与 micro 原生代码的关系
- **不修改** `BufWindow`、`BufPane`、`TabList` 等核心组件
- **只在主循环中添加** InlineInput 的显示和事件处理
- 遵循 AGENTS.md 的原则：最小侵入

---

## 八、参考代码

| 文件 | 用途 |
|------|------|
| `internal/display/infowindow.go` | InfoWindow 实现参考 |
| `internal/action/infopane.go` | InfoPane事件路由参考 |
| `internal/display/bufwindow.go` | BufWindow 显示逻辑参考 |
| `cmd/micro/micro.go` | 主循环事件分发参考 |

---

## 九、结论

| 方案 | 可行性 | 工作量 | 侵入性 |
|------|--------|--------|--------|
| 纯 Lua | ❌ | - | - |
| 修改 Go 原生代码 | ✅ | 中等 | 中等 |
| 新增组件 + 主循环集成 | ✅ | 较小 | 最小 |

**推荐方案**：新增 `inlineinput.go` 组件，在主循环中添加条件判断，遵循"不修改原生代码"原则。