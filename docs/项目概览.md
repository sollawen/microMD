# microNeo 项目概览

> 给 AI 阅读的项目说明。了解全貌后，细节再去读对应文件。

---

## 是什么

对 [Micro](https://github.com/micro-editor/micro) 编辑器的 Markdown 渲染增强。只服务 `.md` / `.markdown` 文件，其他文件走 Micro 原版逻辑。

**核心目标**：打开 MD 文件时看到完整的渲染效果（标题、表格、代码块、列表等），而不是原始的 `# ** |` 标记。

**设计原则**：对 Micro 原生代码的侵入和改动越小越好。

---

## 项目结构

```
microNeo/
├── cmd/micro/               # 主程序入口
│   └── main.go
├── internal/
│   ├── action/              # 按键/鼠标事件处理
│   │   ├── bufpane.go       # BufPane：编辑区主控制器（模式切换在此）
│   │   └── defaults_*.go    # 键绑定配置
│   ├── buffer/             # Buffer 核心：行存储、语法高亮、undo/redo
│   │   ├── buffer.go       # Buffer / SharedBuffer
│   │   └── settings.go      # Buffer 本地设置
│   ├── config/             # 全局配置系统
│   ├── display/            # 渲染输出
│   │   ├── bufwindow.go    # BufWindow.Display() 渲染主循环
│   │   └── softwrap.go     # 软换行 + SLoc/VLoc 坐标映射
│   └── md/                 # ⭐ microNeo 核心：Markdown 渲染管线
│       ├── md.go           # 类型定义（Segment、RenderedSegment 等）
│       ├── detect.go       # 检测：遍历 buffer，行分类，返回 []Segment
│       ├── config.go        # MD 配置（开关、对齐策略等）
│       └── render_*.go      # 各渲染器函数（表格、代码块、标题等）
├── runtime/syntax/         # 语法高亮规则
│   └── markdown.yaml       # ⭐ 需要完整版（含 codeblock region）
└── docs/                   # 开发文档（隔离，不链接进主二进制）
    ├── 架构设计V1.md
    └── markdown_table.go   # 旧版表格渲染代码（参考）
```

**重点目录**：
- `internal/md/` — 所有 MD 渲染逻辑
- `internal/display/bufwindow.go` — 渲染入口，改动集中在这里
- `internal/action/bufpane.go` — 模式切换和事件拦截

---

## 核心概念

### 1. 渲染片（Segment）

每一行 buffer 都属于某个渲染片：

| Markdown 元素 | Buffer 行数 | 渲染片行数 |
|---|---|---|
| 标题、引用、列表项 | 1 | 1 |
| 表格 | 多行 | 1（多行 buffer → 一片） |
| 代码块 | 多行 | 1（多行 buffer → 一片） |
| 段落 | 1 | 1 |

**好处**：渲染管线完全统一，循环里没有分支。

### 2. 检测与渲染分离

```
buffer 变化 → detect.go 扫描全 buffer → 分类结果存 buffer.MDSegments
每帧显示   → displayBufferMD() 读分类结果 → 各渲染器计算输出 → 写 screen
```

- **detect** 在事件路径上（buffer 打开/编辑时），只分类，不依赖屏宽
- **render** 在显示路径上，根据屏宽计算具体布局
- 两者完全独立，通过数据结构通信

### 3. 编辑模式 vs 阅读模式

```
默认（阅读模式）：所有渲染片走渲染器输出，↑↓ 拦截为滚动
按键/点击后（编辑模式）：光标所在渲染片回退原生逻辑，10 秒 idle 切回阅读模式
```

---

## 关键设计决策

| 决策 | 说明 |
|------|------|
| `IsMD` 放 SharedBuffer | 文件是否 MD 是 buffer 的固有属性，NewBuffer 时算一次 |
| 模式状态放 BufPane | editMode 是渲染动态状态，就近事件处理 |
| detect/render 分离 | 解决 Scroll/Diff 需要渲染后才知道行高的时序冲突 |
| renderer 是普通函数 | `Segment.Render` 字段直接挂函数引用，不需要接口 |
| 每帧重新渲染 | 不持久化，和 Micro 原版一致 |

---

## 开发状态

```
✅ Step 0: 架构骨架（渲染片模型、检测/渲染分离）
⏳ Step 1+: 逐个渲染器调优（标题、表格、代码块、列表等）
📋 Step 2:  交互设计（模式切换、键拦截、鼠标定位）—— 待渲染稳定后进行
```

---

## 快速上手

```bash
# 编译
cd cmd/micro && go build -o micro .

# 运行
./micro yourfile.md

# 测试
go test ./internal/md/... -v
```

---

## 关键文件说明

| 文件 | 作用 |
|------|------|
| `bufwindow.go:382-843` | `displayBuffer()` — 520+ 行核心渲染函数 |
| `bufwindow.go:displayBufferMD()` | 新增的 MD 渲染入口，读 `b.MDSegments` |
| `detect.go` | 集中式分类器，扫描 buffer 返回 `[]Segment` |
| `render_table.go` | 表格渲染器 |
| `markdown.yaml` | 需完整版（含 codeblock region），Micro 内置版不够用 |

---

## 主要流程

```
Micro 启动 → NewBuffer(path) → NewBufPane(buf)
   ↓
BufPane.HandleEvent() 接收按键/鼠标
   ↓
事件触发 buffer 变化 → MarkModified / UpdateRules → detect.go 重新分类
   ↓
BufWindow.Display() 每帧执行：
   ├── if w.Buf.IsMD { displayBufferMD() } else { displayBuffer() }
```

---

## 注意事项

- **不改 Micro 原生行为**：除 `bufwindow.go` 加 if/else 分流和 `bufpane.go` 加事件拦截外，不改动 Micro 原版
- **不持久化渲染结果**：每帧重新渲染
- **不依赖屏宽做检测**：检测结果 content-static
- **markdown.yaml 需要完整版**：Micro 内置版没有 codeblock region，必须提供完整版