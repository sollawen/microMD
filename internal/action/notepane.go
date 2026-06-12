package action

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"time"

	"github.com/micro-editor/micro/v2/internal/buffer"
	"github.com/micro-editor/micro/v2/internal/config"
	"github.com/micro-editor/micro/v2/internal/display"
	"github.com/micro-editor/micro/v2/internal/eabp"
	"github.com/micro-editor/micro/v2/internal/screen"
	"github.com/micro-editor/micro/v2/internal/util"
	"github.com/micro-editor/tcell/v2"
)

// NotePane is a floating overlay pane for quick notes.
// It embeds *BufPane to get full editing capabilities while
// restricting available actions via a whitelist.
type NotePane struct {
	*BufPane
	isOpen        bool
	x, y          int
	width         int
	height        int
	filePath      string       // main editor buffer.AbsPath
	fileCursor    buffer.Loc   // captured cursor (X=col, Y=line)
	fileSelection    *[2]buffer.Loc // nil = no selection
	fileSelectionText string          // main editor's selected text, captured at open()
}

// TheNotePane is the global NotePane instance
var TheNotePane *NotePane

// NotePaneBindings is the whitelist KeyTree for NotePane
var NotePaneBindings *KeyTree

// allowedNotePaneActions is the whitelist of allowed action names
var allowedNotePaneActions = map[string]bool{
	// Cursor movement
	"CursorUp": true, "CursorDown": true, "CursorLeft": true, "CursorRight": true,
	"CursorPageUp": true, "CursorPageDown": true,
	"CursorStart": true, "CursorEnd": true,
	"CursorToViewTop": true, "CursorToViewCenter": true, "CursorToViewBottom": true,

	// Selection
	"SelectUp": true, "SelectDown": true, "SelectLeft": true, "SelectRight": true,
	"SelectToStart": true, "SelectToEnd": true,
	"SelectWordRight": true, "SelectWordLeft": true,
	"SelectSubWordRight": true, "SelectSubWordLeft": true,
	"SelectLine": true,
	"SelectToStartOfLine": true, "SelectToStartOfText": true,
	"SelectToStartOfTextToggle": true, "SelectToEndOfLine": true,
	"SelectPageUp": true, "SelectPageDown": true,
	"StartOfText": true, "StartOfTextToggle": true,
	"StartOfLine": true, "EndOfLine": true,

	// Paragraph navigation
	"ParagraphPrevious": true, "ParagraphNext": true,
	"SelectToParagraphPrevious": true, "SelectToParagraphNext": true,

	// Text editing
	"InsertNewline": true, "Backspace": true, "Delete": true, "InsertTab": true,
	"DeleteWordRight": true, "DeleteWordLeft": true,
	"DeleteSubWordRight": true, "DeleteSubWordLeft": true,
	"IndentLine": true, "OutdentLine": true,
	"IndentSelection": true, "OutdentSelection": true,

	// Scrolling
	"PageUp": true, "PageDown": true,
	"HalfPageUp": true, "HalfPageDown": true,
	"ScrollUpAction": true, "ScrollDownAction": true,
	"Center": true, "Start": true, "End": true,
	"ScrollUp": true, "ScrollDown": true,

	// Clipboard
	"Copy": true, "CopyLine": true, "Cut": true, "CutLine": true,
	"Paste": true, "PastePrimary": true,
	"Duplicate": true, "DuplicateLine": true,
	"DeleteLine": true, "MoveLinesUp": true, "MoveLinesDown": true,

	// Multi-cursor
	"SpawnMultiCursor": true, "SpawnMultiCursorUp": true,
	"SpawnMultiCursorDown": true, "SpawnMultiCursorSelect": true,
	"RemoveMultiCursor": true, "RemoveAllMultiCursors": true,
	"SkipMultiCursor": true, "SkipMultiCursorBack": true,

	// Other
	"JumpToMatchingBrace": true, "SelectAll": true,
	"Deselect": true, "Escape": true, "ToggleOverwriteMode": true,
	"ClearInfo": true, "ClearStatus": true, "None": true,

	// EABP send
	"NotePaneSend": true,

	// Mouse
	"MousePress": true, "MouseDrag": true, "MouseRelease": true,
	"MouseMultiCursor": true,

	// Word navigation
	"WordRight": true, "WordLeft": true,
	"SubWordRight": true, "SubWordLeft": true,
}

func init() {
	NotePaneBindings = NewKeyTree()
	notePaneMapDefaults(DefaultBindings("buffer"))
	// Bind Alt-Enter to NotePaneSend (fallback Alt-s if Alt-Enter not available in terminal)
	notePaneMapBinding("Alt-Enter", "NotePaneSend")
}

// notePaneMapDefaults registers allowed key bindings from defaults into NotePaneBindings.
// It uses the same mechanism as BufMapEvent but filters by whitelist.
func notePaneMapDefaults(defaults map[string]string) {
	for keyStr, actionStr := range defaults {
		notePaneMapBinding(keyStr, actionStr)
	}
}

// notePaneMapBinding maps a key string to action(s), filtering by whitelist.
// An action string can contain multiple actions separated by & | , (e.g. "Autocomplete|IndentSelection|InsertTab").
// Only allowed actions are kept; if none are allowed, the binding is skipped entirely.
func notePaneMapBinding(keyStr, actionStr string) {
	// Parse the key event
	ev, err := findEvent(keyStr)
	if err != nil {
		return
	}

	// Split action string by & | , and filter by whitelist
	filteredActions := filterActions(actionStr)
	if len(filteredActions) == 0 {
		return
	}

	// Rebuild the action string with only allowed actions
	filteredStr := strings.Join(filteredActions, "|")

	// Use the same binding mechanism as BufMapEvent
	notePaneRegisterBinding(ev, filteredStr)
}

// filterActions parses a composite action string (e.g. "Autocomplete|IndentSelection|InsertTab")
// and returns only the actions that are in the whitelist.
func filterActions(actionStr string) []string {
	var result []string
	for {
		idx := util.IndexAnyUnquoted(actionStr, "&|,")
		a := actionStr
		sep := byte('|')
		if idx >= 0 {
			a = actionStr[:idx]
			sep = actionStr[idx]
			actionStr = actionStr[idx+1:]
		} else {
			actionStr = ""
		}

		if isActionAllowed(a) {
			result = append(result, a)
		}

		if actionStr == "" {
			break
		}
		_ = sep // we rejoin with | regardless
	}
	return result
}

// isActionAllowed checks if an action name is in the whitelist.
// It also handles "command:" and "lua:" prefixed actions (which are not allowed).
func isActionAllowed(a string) bool {
	if strings.HasPrefix(a, "command:") || strings.HasPrefix(a, "command-edit:") || strings.HasPrefix(a, "lua:") {
		return false
	}
	return allowedNotePaneActions[a]
}

// notePaneRegisterBinding registers a filtered action binding to NotePaneBindings.
// This mirrors the logic in BufMapEvent but targets NotePaneBindings.
func notePaneRegisterBinding(k Event, action string) {
	var actionfns []BufAction
	for i := 0; ; i++ {
		if action == "" {
			break
		}

		idx := util.IndexAnyUnquoted(action, "&|,")
		a := action
		if idx >= 0 {
			a = action[:idx]
			action = action[idx+1:]
		} else {
			action = ""
		}

		var afn BufAction
		if f, ok := BufKeyActions[a]; ok {
			afn = f
		} else if f, ok := BufMouseActions[a]; ok {
			afn = f
		} else {
			continue
		}
		actionfns = append(actionfns, afn)
	}

	if len(actionfns) == 0 {
		return
	}

	bufAction := func(h *BufPane, te *tcell.EventMouse) bool {
		for _, a := range actionfns {
			var success bool
			h.Buf.SetCurCursor(0)
			h.Cursor = h.Buf.GetActiveCursor()
			success = h.execAction(a, "", te)
			_ = success
		}
		return true
	}

	switch e := k.(type) {
	case KeyEvent, KeySequenceEvent, RawEvent:
		NotePaneBindings.RegisterKeyBinding(e, BufKeyActionGeneral(func(h *BufPane) bool {
			return bufAction(h, nil)
		}))
	case MouseEvent:
		NotePaneBindings.RegisterMouseBinding(e, BufMouseActionGeneral(bufAction))
	}
}

// NewNotePane creates a new NotePane instance
func NewNotePane() *NotePane {
	n := &NotePane{
		height: 5,
	}

	// Create an in-memory scratch buffer. Content is discarded on close;
	// see buffer.BTScratch ("Cannot save scratch buffer" in save.go:237).
	buf := buffer.NewBufferFromString("", "", buffer.BTScratch)

	// Disable ruler for NotePane
	buf.SetOptionNative("ruler", false)

	// Create BufWindow with initial position (will be adjusted in open())
	win := display.NewBufWindow(0, 0, 80, n.height, buf)
	win.SetHideStatusLine(true)

	// Create BufPane using newBufPane (lowercase, does not trigger finishInitialize)
	n.BufPane = newBufPane(buf, win, nil)
	n.BufPane.bindings = NotePaneBindings

	return n
}

// Toggle opens or closes the NotePane
func (n *NotePane) Toggle() {
	if n.isOpen {
		n.close()
	} else {
		n.open()
	}
}

// IsOpen returns whether the NotePane is currently open
func (n *NotePane) IsOpen() bool {
	return n.isOpen
}

// open opens the NotePane and positions it below the cursor
func (n *NotePane) open() {
	// Get the current active BufPane
	pane := MainTab().CurPane()
	if pane == nil {
		return
	}

	// Capture file path from the main editor buffer
	n.filePath = pane.Buf.AbsPath

	// Get the pane's view and BufWindow
	view := pane.BWindow.GetView()
	bw := pane.BWindow.(*display.BufWindow)

	// 1. Find the lowest cursor/selection screen row
	lowestRow := n.lowestCursorScreenRow(bw, view)

	// 2. Calculate NotePane position
	n.x = view.X
	n.width = view.Width
	notePaneTopBorder := lowestRow + 1
	notePaneBottomBorder := notePaneTopBorder + n.height + 1

	// 3. If not enough space below, scroll the main editor up
	viewBottom := view.Y + view.Height
	if notePaneBottomBorder > viewBottom {
		deficit := notePaneBottomBorder - viewBottom + 2

		// Safety constraint: don't scroll cursor above scrollmargin
		scrollmargin := int(pane.Buf.Settings["scrollmargin"].(float64))
		maxDeficit := lowestRow - scrollmargin
		if deficit > maxDeficit {
			deficit = maxDeficit
		}

		if deficit > 0 {
			oldStartLine := view.StartLine
			view.StartLine = bw.Scroll(view.StartLine, deficit)
			lowestRow -= bw.Diff(oldStartLine, view.StartLine)
			notePaneTopBorder = lowestRow + 1
		}
	}

	// 4. Set NotePane position
	n.y = notePaneTopBorder

	// 5. Reposition BufWindow to be inside the border
	nbw := n.BufPane.BWindow.(*display.BufWindow)
	nbw.X = n.x + 1
	nbw.Y = n.y + 1
	n.BufPane.Resize(n.width-2, n.height)

	n.isOpen = true
}

// NotePaneSend sends the note content via EABP to a receiver.
// It reads the current notePane buffer as message, discovers receivers,
// and sends the context payload to the unix socket of the single live receiver.
func NotePaneSend(h *BufPane) bool {
	n := TheNotePane
	if n == nil {
		return false
	}

	// 1. Discover receivers
	receivers, err := eabp.Discover()
	if err != nil {
		InfoBar.Message("✗ discover error: " + err.Error())
		return false
	}
	if len(receivers) != 1 {
		if len(receivers) == 0 {
			InfoBar.Message("✗ no receiver found")
		} else {
			InfoBar.Message("✗ multiple receivers found, need exactly one")
		}
		return false
	}
	receiver := receivers[0]

	// 2. Build payload
	// Convert buffer.Loc {X,Y} = {col,row} (0-based) to EABP Position {Line,Col} = {row,col} (1-based)
	cursorPos := eabp.Position{Line: n.fileCursor.Y + 1, Col: n.fileCursor.X + 1}

	// Read notePane buffer text as message
	message := string(n.BufPane.Buf.Bytes())

	payload := eabp.ContextPayload{
		Path:    n.filePath,
		Cursor:  cursorPos,
		Message: message,
	}

	// Selection: pre-captured and normalized at open() time
	if n.fileSelection != nil {
		payload.Selection = &eabp.Selection{
			Start: eabp.Position{Line: n.fileSelection[0].Y + 1, Col: n.fileSelection[0].X + 1},
			End:   eabp.Position{Line: n.fileSelection[1].Y + 1, Col: n.fileSelection[1].X + 1},
			Text:  n.fileSelectionText,
		}
	}

	// Serialize payload
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		InfoBar.Message("✗ marshal error: " + err.Error())
		return false
	}

	// 3. Build envelope
	env := eabp.Envelope{
		V:       1,
		Type:    "context",
		Sender:  eabp.Sender{PID: os.Getpid(), Name: "microNeo", Instance: "default"},
		TS:      float64(time.Now().UnixNano()) / 1e9,
		Payload: payloadJSON,
	}

	// 4. Dial receiver's unix socket, write JSON line, close
	c, err := net.Dial("unix", receiver.Socket)
	if err != nil {
		InfoBar.Message("✗ send failed: " + err.Error())
		return false
	}
	lineBytes, err := env.MarshalLine()
	if err != nil {
		c.Close()
		InfoBar.Message("✗ marshal error: " + err.Error())
		return false
	}
	if _, err := c.Write(lineBytes); err != nil {
		c.Close()
		InfoBar.Message("✗ send failed: " + err.Error())
		return false
	}
	c.Close()

	// 5. Success: close notePane and show confirmation
	n.close()
	InfoBar.Message("✓ sent to " + receiver.Name)
	return false
}

// locToScreenRow converts a buffer location to its screen row.
// It correctly handles softwrap by using SLocFromLoc and Diff.
func (n *NotePane) locToScreenRow(bw *display.BufWindow, view *display.View, loc buffer.Loc) int {
	if bw.Buf.IsMD {
		if offset, ok := bw.BufferLineToScreenOffset(loc.Y); ok {
			return offset
		}
	}
	sloc := bw.SLocFromLoc(loc)
	row := bw.Diff(view.StartLine, sloc)
	return row + view.Y
}

// lowestCursorScreenRow finds the lowest screen row among all cursors and selections.
// For cursors with selection, it uses the bottom of the selection (max Y).
func (n *NotePane) lowestCursorScreenRow(bw *display.BufWindow, view *display.View) int {
	lowestRow := -1

	for _, cursor := range bw.Buf.GetCursors() {
		var loc buffer.Loc
		if cursor.HasSelection() {
			// Use the selection endpoint with larger Y
			sel := cursor.CurSelection
			if sel[0].Y > sel[1].Y {
				loc = sel[0]
			} else {
				loc = sel[1]
			}
		} else {
			loc = cursor.Loc
		}

		screenRow := n.locToScreenRow(bw, view, loc)
		if screenRow > lowestRow {
			lowestRow = screenRow
			// Capture cursor position and selection
			n.fileCursor = loc
			if cursor.HasSelection() {
				sel := cursor.CurSelection
				start, end := sel[0], sel[1]
				if start.GreaterThan(end) {
					start, end = end, start
				}
				normalized := [2]buffer.Loc{start, end}
				n.fileSelection = &normalized
				selText := string(bw.Buf.Substr(start, end))
				if len(selText) > 2048 {
					selText = ""
				}
				n.fileSelectionText = selText
			} else {
				n.fileSelection = nil
				n.fileSelectionText = ""
			}
		}
	}

	return lowestRow
}

// close closes the NotePane and saves the file
func (n *NotePane) close() {
	n.isOpen = false

	// Restore main editor's normal scroll position
	if pane := MainTab().CurPane(); pane != nil {
		pane.BWindow.Relocate()
	}
}

// HandleEvent handles keyboard events for the NotePane
func (n *NotePane) HandleEvent(event tcell.Event) {
	if _, ok := event.(*tcell.EventResize); ok {
		if n.isOpen {
			n.open()
		}
		return
	}
	n.BufPane.HandleEvent(event)
}

// Display renders the NotePane on the screen
func (n *NotePane) Display() {
	if !n.isOpen {
		return
	}

	// Clear the entire NotePane area (border + content) to hide underlying editor content
	for row := 0; row < n.height+2; row++ {
		for col := 0; col < n.width; col++ {
			screen.Screen.SetContent(n.x+col, n.y+row, ' ', nil, config.DefStyle)
		}
	}

	// Box-drawing characters
	topLeft := '┌'
	topRight := '┐'
	bottomLeft := '└'
	bottomRight := '┘'
	horizontal := '─'
	vertical := '│'

	// Draw top border
	screen.Screen.SetContent(n.x, n.y, topLeft, nil, config.DefStyle)
	for i := 1; i < n.width-1; i++ {
		screen.Screen.SetContent(n.x+i, n.y, horizontal, nil, config.DefStyle)
	}
	screen.Screen.SetContent(n.x+n.width-1, n.y, topRight, nil, config.DefStyle)

	// Draw bottom border
	screen.Screen.SetContent(n.x, n.y+n.height+1, bottomLeft, nil, config.DefStyle)
	for i := 1; i < n.width-1; i++ {
		screen.Screen.SetContent(n.x+i, n.y+n.height+1, horizontal, nil, config.DefStyle)
	}
	screen.Screen.SetContent(n.x+n.width-1, n.y+n.height+1, bottomRight, nil, config.DefStyle)

	// Draw side borders
	for row := 0; row < n.height; row++ {
		screenY := n.y + 1 + row
		screen.Screen.SetContent(n.x, screenY, vertical, nil, config.DefStyle)
		screen.Screen.SetContent(n.x+n.width-1, screenY, vertical, nil, config.DefStyle)
	}

	// Display the BufWindow content (includes cursor)
	n.BufPane.BWindow.Display()
}
