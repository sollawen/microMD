## 表格的原始内容在这里

```
| 模块 | 职责 | 我们要改？ |
|------|------|-----------|
| `internal/display/bufwindow.go` (903行) | 缓冲区内容渲染 | **✅ 主要改这里** |
| `internal/display/softwrap.go` (342行) | 软换行坐标映射 | **✅ 需要适配** |
| `internal/buffer/line_array.go` | 存储文件行数据 | ❌ 不动 |
| `internal/buffer/cursor.go` | 光标位置管理 | ❌ 不动 |
| `internal/screen/screen.go` | 底层屏幕输出 | ❌ 不动 |
```

## micromd 渲染后的表格在这里


| 模块 | 职责 | 我们要改？ |
|------|------|-----------|
| `internal/display/bufwindow.go` (903行) | 缓冲区内容渲染 | **✅ 主要改这里** |
| `internal/display/softwrap.go` (342行) | 软换行坐标映射 | **✅ 需要适配** |
| `internal/buffer/line_array.go` | 存储文件行数据 | ❌ 不动 |
| `internal/buffer/cursor.go` | 光标位置管理 | ❌ 不动 |
| `internal/screen/screen.go` | 底层屏幕输出 | ❌ 不动 |



