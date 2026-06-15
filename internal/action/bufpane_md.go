package action

import (
	"github.com/micro-editor/tcell/v2"
	"github.com/micro-editor/micro/v2/internal/buffer"
	"github.com/micro-editor/micro/v2/internal/config"
	"github.com/micro-editor/micro/v2/internal/display"
	"github.com/micro-editor/micro/v2/internal/md"
	"github.com/micro-editor/micro/v2/internal/util"
)

// initMDConfig 在创建 BufWindow 后立刻同步 MD 渲染配置。
// 仅当 buffer 是 MD 文件时才生效，非 MD 文件为 no-op。
// 该函数封装所有 MD 相关逻辑，让 bufpane.go 的 NewBufPaneFromBuf 保持简洁。
func initMDConfig(buf *buffer.Buffer, w *display.BufWindow) {
	if !buf.IsMD { // 单一真源（NewBuffer 算了 IsMarkdownFile）
		return
	}

	w.SetMDConfig(md.MDConfig{
		Colorscheme: md.MDColorscheme{
			DefStyle: config.DefStyle,
			Styles:   config.Colorscheme,
		},
		TabSize:        util.IntOpt(buf.Settings["tabsize"]),
		MDTableAlign:   buf.Settings["mdtablealign"].(bool),
		MDTableBorder:  buf.Settings["mdtableborder"].(bool),
		MDBoldItalic:   buf.Settings["mdbolditalic"].(bool),
		MDCodeBlock:    buf.Settings["mdcodeblock"].(bool),
		MDHeading:      buf.Settings["mdheading"].(bool),
		MDList:         buf.Settings["mdlist"].(bool),
		MDLink:         buf.Settings["mdlink"].(bool),
	})
	// Step 1.0：MD 文件默认进入编辑模式（光标所在 segment 回退原生显示）
	w.SetEditMode(true)
}

// observeEditModeToggle 在 micro 完整处理完事件后，被动观察事件类型来切换 editMode。
// 它不消费事件，不影响 micro 的任何原生行为。
//
// 规则：
//   - ESC 键 → editMode=false（阅读模式）
//   - 任意非 ESC 有意义按键 + 鼠标 click → editMode=true（编辑模式）
//   - 单独 modifier 键（Shift/Ctrl/Alt/Meta）不触发
//   - 非MD文件 → 忽略
func (h *BufPane) observeEditModeToggle(event tcell.Event) {
	if !h.Buf.IsMD {
		return
	}

	w, ok := h.BWindow.(*display.BufWindow)
	if !ok {
		return
	}

	switch e := event.(type) {
	case *tcell.EventKey:
		if e.Key() == tcell.KeyEscape {
			w.SetEditMode(false)
		} else {
			// 任意其他按键（字母、方向键、Enter、Ctrl+key 等）→ 进入编辑模式
			// modifier-only 按键（Shift/Ctrl/Alt/Meta 单独按）在大多数终端不会产生事件，
			// 因此无需特殊处理。即使产生事件，它们也是有效的 key press，应该触发编辑模式。
			w.SetEditMode(true)
		}
	case *tcell.EventMouse:
		// 排除滚轮：只有真实点击/拖拽才进入编辑模式
		if e.Buttons() != tcell.ButtonNone &&
			e.Buttons()&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) == 0 {
			w.SetEditMode(true)
		}
	}
}
