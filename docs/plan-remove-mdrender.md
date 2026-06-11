# 方案：删除 mdConfig.MDRender

## 背景

microNeo 的核心价值是 MD 文件的渲染能力。如果用户不需要 MD 渲染，直接使用 micro 即可，无需使用 microNeo。因此 `MDRender` 这个"是否启用 MD 渲染"的开关没有实际意义。

## 修改范围

涉及以下文件：

| 文件 | 修改内容 |
|------|----------|
| `internal/config/settings_md.go` | 删除 `mdrender` 默认设置 |
| `internal/action/bufpane_md.go` | 删除 `MDRender` 字段赋值 |
| `internal/md/config.go` | 删除 `MDRender` 字段定义和默认值 |
| `internal/display/bufwindow.go` | 简化判断条件 |

## 具体修改

### 1. `internal/config/settings_md.go`

```go
// 删除: defaultCommonSettings["mdrender"] = true
```

### 2. `internal/action/bufpane_md.go`

```go
// 删除这两行:
// MDRender:       config.GetGlobalOption("mdrender").(bool),
// MDRenderIdle:   config.GetGlobalOption("mdrenderidle").(float64),
```

### 3. `internal/config/settings_md.go`

```go
// 删除:
DefaultGlobalOnlySettings["mdrenderidle"] = float64(10)
```

### 4. `internal/md/config.go`

```go
// MDConfig 结构体中删除:
MDRender      bool    // 功能总开关
MDRenderIdle  float64 // 编辑模式超时秒数（死代码，一并删除）

// DefaultMDConfig() 中删除:
MDRender:      true,
MDRenderIdle:  10,
```

### 5. `internal/display/bufwindow.go`

**两处判断简化：**

- **第 302 行**: 
  - 原: `if w.Buf.IsMD && w.mdConfig.MDRender {`
  - 改: `if w.Buf.IsMD {`

- **第 945 行**:
  - 原: `if !w.Buf.IsMD || !w.mdConfig.MDRender {`
  - 改: `if !w.Buf.IsMD {`

## 变更后逻辑

```
IsMD=true  → 永远走 displayBufferMD()
IsMD=false → 永远走 displayBuffer()
```

不再有第三种分支。

## 用户影响

- 用户配置中的 `mdrender` 设置不再生效（可保留设置不报错，但无效果）
- MD 文件始终渲染，microNeo 体验保持一致

## 已确认（Lisa Review）

- `MDRenderIdle` 一并删除（死代码，未被使用）
- `mdrenderidle` 从 `DefaultGlobalOnlySettings` 删除

## 用户影响

- 用户配置中的 `mdrender` 和 `mdrenderidle` 设置不再生效（不报错，无效果）
- 建议通过 changelog 告知用户