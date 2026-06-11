**v1.04**
- `viewportRowBufLine` flat array replaces `mdCache` for O(1) screen row → buffer line lookup
- Add `bufferLineToScreenOffset` reverse lookup
- Code block top/bottom borders now map to real buffer lines (opening/closing fence)

**v1.03**
- inline code 的背景色，在所有render中都能正确显示了
- 微调s-ligh的颜色
