**v1.0.5**
- MD cursor vertical scroll relocation 从 buffer 行空间改为屏幕行空间：新增 2D viewportRowmap，按 (Line, Row) 精确匹配光标屏幕位置
- Relocate 垂直滚动分发：MD 文件走 `relocateVerticalMD`，非 MD 文件保持 micro 原生逻辑（零侵入）
- 修复文件尾 panic：`renderSegmentNative` 在最后一行 softwrap 时 vY 越界导致 `viewportRowBufLine` 切片越界，截断到 bufHeight
- `render_table.makeTableSeparator` 增加 bufLine 参数，code block header 分隔线映射真实 buffer 行
- 微调 s-dark colorscheme 的 `statusline.special/dim/normal` 配色
- 移除 `initMDConfig` 中写入 `/tmp/microNeo-debug.log` 的调试日志

**v1.0.4**
- `viewportRowBufLine` flat array replaces `mdCache` for O(1) screen row → buffer line lookup
- Add `bufferLineToScreenOffset` reverse lookup
- Code block top/bottom borders now map to real buffer lines (opening/closing fence)

**v1.0.3**
- inline code 的背景色，在所有render中都能正确显示了
- 微调s-ligh的颜色
