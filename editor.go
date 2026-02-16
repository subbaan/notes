package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Editor is a simple text editor with cursor position tracking
type Editor struct {
	lines       [][]rune // Text buffer as lines of runes
	cursorRow   int      // Current cursor row (0-indexed)
	cursorCol   int      // Current cursor column (0-indexed)
	desiredCol  int      // Desired column for vertical movement (preserves column across lines)
	viewportRow int      // Top visible row for scrolling
	width       int      // Editor width
	height      int      // Editor height
	placeholder string   // Placeholder text when empty
	focused     bool     // Whether editor is focused
	killBuffer  string   // Killed text for yank (Ctrl+Y)
	showHelp    bool     // Whether to show help overlay
	dirty       bool     // Whether there are unsaved changes
}

// New creates a new editor
func NewEditor() Editor {
	return Editor{
		lines:       [][]rune{{}}, // Start with one empty line
		cursorRow:   0,
		cursorCol:   0,
		desiredCol:  0,
		viewportRow: 0,
		width:       80,
		height:      24,
		focused:     false,
	}
}

// SetWidth sets the editor width
func (e *Editor) SetWidth(w int) {
	e.width = w
}

// SetHeight sets the editor height
func (e *Editor) SetHeight(h int) {
	e.height = h
}

// SetPlaceholder sets the placeholder text
func (e *Editor) SetPlaceholder(p string) {
	e.placeholder = p
}

// Focus focuses the editor
func (e *Editor) Focus() {
	e.focused = true
}

// Blur removes focus from the editor
func (e *Editor) Blur() {
	e.focused = false
}

// Value returns the current text content
func (e *Editor) Value() string {
	var sb strings.Builder
	for i, line := range e.lines {
		sb.WriteString(string(line))
		if i < len(e.lines)-1 {
			sb.WriteRune('\n')
		}
	}
	return sb.String()
}

// SetValue sets the text content
func (e *Editor) SetValue(text string) {
	e.lines = [][]rune{}
	if text == "" {
		e.lines = [][]rune{{}}
		e.cursorRow = 0
		e.cursorCol = 0
		e.desiredCol = 0
		e.viewportRow = 0
		e.dirty = false
		return
	}

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		e.lines = append(e.lines, []rune(line))
	}

	// Reset cursor to beginning
	e.cursorRow = 0
	e.cursorCol = 0
	e.desiredCol = 0
	e.viewportRow = 0
	e.dirty = false
}

// Dirty reports whether the editor has unsaved changes.
func (e *Editor) Dirty() bool {
	return e.dirty
}

// ClearDirty marks the editor as clean.
func (e *Editor) ClearDirty() {
	e.dirty = false
}

// MarkDirty marks the editor as having unsaved changes.
func (e *Editor) MarkDirty() {
	e.dirty = true
}

// SetCursor sets the cursor position by character index
func (e *Editor) SetCursor(pos int) {
	if pos < 0 {
		pos = 0
	}

	charCount := 0
	for row, line := range e.lines {
		lineLen := len(line)
		if charCount+lineLen >= pos {
			// Cursor is on this line
			e.cursorRow = row
			e.cursorCol = pos - charCount
			e.updateDesiredCol()
			e.ensureCursorVisible()
			return
		}
		charCount += lineLen + 1 // +1 for newline
	}

	// Position is beyond end, put cursor at end
	if len(e.lines) > 0 {
		e.cursorRow = len(e.lines) - 1
		e.cursorCol = len(e.lines[e.cursorRow])
		e.updateDesiredCol()
	}
	e.ensureCursorVisible()
}

// GetCursor returns the cursor position as character index
func (e *Editor) GetCursor() int {
	pos := 0
	for i := 0; i < e.cursorRow && i < len(e.lines); i++ {
		pos += len(e.lines[i]) + 1 // +1 for newline
	}
	pos += e.cursorCol
	return pos
}

// ensureCursorVisible adjusts viewport to keep cursor visible
func (e *Editor) ensureCursorVisible() {
	// Scroll down if cursor is below viewport
	if e.cursorRow >= e.viewportRow+e.height {
		e.viewportRow = e.cursorRow - e.height + 1
	}
	// Scroll up if cursor is above viewport
	if e.cursorRow < e.viewportRow {
		e.viewportRow = e.cursorRow
	}
}

// clampCursor ensures cursor is within valid bounds
func (e *Editor) clampCursor() {
	// Ensure row is valid
	if e.cursorRow >= len(e.lines) {
		e.cursorRow = len(e.lines) - 1
	}
	if e.cursorRow < 0 {
		e.cursorRow = 0
	}

	// Clamp column to line length
	if e.cursorRow < len(e.lines) {
		lineLen := len(e.lines[e.cursorRow])
		if e.cursorCol > lineLen {
			e.cursorCol = lineLen
		}
	}
	if e.cursorCol < 0 {
		e.cursorCol = 0
	}
}

// updateDesiredCol updates the desired column based on current cursor position
// This tracks the visual column (within the line wrap width) for consistent up/down movement
func (e *Editor) updateDesiredCol() {
	if e.width > 0 {
		e.desiredCol = e.cursorCol % e.width
	} else {
		e.desiredCol = e.cursorCol
	}
}

// insertRune inserts a rune at the cursor position
func (e *Editor) insertRune(r rune) {
	if e.cursorRow >= len(e.lines) {
		e.lines = append(e.lines, []rune{})
		e.cursorRow = len(e.lines) - 1
	}

	line := e.lines[e.cursorRow]
	// Insert rune at cursor position
	line = append(line[:e.cursorCol], append([]rune{r}, line[e.cursorCol:]...)...)
	e.lines[e.cursorRow] = line
	e.cursorCol++
	e.updateDesiredCol()
	e.dirty = true
}

// insertNewline inserts a newline at cursor position
func (e *Editor) insertNewline() {
	if e.cursorRow >= len(e.lines) {
		e.lines = append(e.lines, []rune{})
		e.cursorRow = len(e.lines) - 1
	}

	currentLine := e.lines[e.cursorRow]
	// Split line at cursor
	beforeCursor := make([]rune, len(currentLine[:e.cursorCol]))
	copy(beforeCursor, currentLine[:e.cursorCol])
	afterCursor := make([]rune, len(currentLine[e.cursorCol:]))
	copy(afterCursor, currentLine[e.cursorCol:])

	// Update current line and insert new line
	e.lines[e.cursorRow] = beforeCursor
	e.lines = append(e.lines[:e.cursorRow+1], append([][]rune{afterCursor}, e.lines[e.cursorRow+1:]...)...)

	// Move cursor to start of next line
	e.cursorRow++
	e.cursorCol = 0
	e.desiredCol = 0
	e.ensureCursorVisible()
	e.dirty = true
}

// deleteCharBackward deletes character before cursor (backspace)
func (e *Editor) deleteCharBackward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	changed := false
	if e.cursorCol > 0 {
		// Delete character on current line
		line := e.lines[e.cursorRow]
		line = append(line[:e.cursorCol-1], line[e.cursorCol:]...)
		e.lines[e.cursorRow] = line
		e.cursorCol--
		changed = true
	} else if e.cursorRow > 0 {
		// At start of line, merge with previous line
		prevLine := e.lines[e.cursorRow-1]
		currentLine := e.lines[e.cursorRow]
		e.cursorCol = len(prevLine)
		e.lines[e.cursorRow-1] = append(prevLine, currentLine...)
		e.lines = append(e.lines[:e.cursorRow], e.lines[e.cursorRow+1:]...)
		e.cursorRow--
		e.ensureCursorVisible()
		changed = true
	}
	e.updateDesiredCol()
	if changed {
		e.dirty = true
	}
}

// deleteCharForward deletes character at cursor (delete key)
func (e *Editor) deleteCharForward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	changed := false

	if e.cursorCol < len(line) {
		// Delete character at cursor
		line = append(line[:e.cursorCol], line[e.cursorCol+1:]...)
		e.lines[e.cursorRow] = line
		changed = true
	} else if e.cursorRow < len(e.lines)-1 {
		// At end of line, merge with next line
		nextLine := e.lines[e.cursorRow+1]
		e.lines[e.cursorRow] = append(line, nextLine...)
		e.lines = append(e.lines[:e.cursorRow+1], e.lines[e.cursorRow+2:]...)
		changed = true
	}
	e.updateDesiredCol()
	if changed {
		e.dirty = true
	}
}

// countVisualLines calculates how many visual lines a logical line occupies
// based on the editor width. Empty lines are counted as 1 visual line.
func (e *Editor) countVisualLines(line []rune, width int) int {
	if width <= 0 {
		return 1
	}
	lineLen := len(line)
	if lineLen == 0 {
		return 1
	}
	// Ceiling division: (lineLen + width - 1) / width
	return (lineLen + width - 1) / width
}

// moveVisualLineUp moves the cursor up one visual line, accounting for text wrapping.
// It uses desiredCol to maintain consistent column position across wrapped lines.
func (e *Editor) moveVisualLineUp(cursorRow, cursorCol, width int, lines [][]rune) (int, int) {
	if width <= 0 {
		width = 80 // fallback
	}

	// Calculate current visual line within the logical line
	currentVisualLine := cursorCol / width

	// If not on the first visual line of current logical line, move up within same line
	if currentVisualLine > 0 {
		newCol := (currentVisualLine-1)*width + e.desiredCol
		// Clamp to line length
		if cursorRow < len(lines) && newCol > len(lines[cursorRow]) {
			newCol = len(lines[cursorRow])
		}
		return cursorRow, newCol
	}

	// Already on first visual line of logical line
	// Check if we can move to previous logical line
	if cursorRow == 0 {
		// At document start
		return 0, 0
	}

	// Move to previous logical line
	prevLogicalRow := cursorRow - 1
	prevLine := lines[prevLogicalRow]
	prevLineLen := len(prevLine)
	prevVisualLines := e.countVisualLines(prevLine, width)
	lastVisualLine := prevVisualLines - 1

	// Position at desiredCol on the last visual line of previous logical line
	newCol := lastVisualLine*width + e.desiredCol
	// Clamp to valid position (line might be shorter than full width)
	if newCol > prevLineLen {
		newCol = prevLineLen
	}

	return prevLogicalRow, newCol
}

// moveVisualLineDown moves the cursor down one visual line, accounting for text wrapping.
// It uses desiredCol to maintain consistent column position across wrapped lines.
func (e *Editor) moveVisualLineDown(cursorRow, cursorCol, width int, lines [][]rune) (int, int) {
	if width <= 0 {
		width = 80 // fallback
	}

	if cursorRow >= len(lines) {
		return cursorRow, cursorCol
	}

	currentLine := lines[cursorRow]
	lineLen := len(currentLine)

	// Calculate current visual line within the logical line
	currentVisualLine := cursorCol / width
	currentVisualLines := e.countVisualLines(currentLine, width)

	// If not on the last visual line of current logical line, move down within same line
	if currentVisualLine < currentVisualLines-1 {
		newCol := (currentVisualLine+1)*width + e.desiredCol
		// Clamp to line end
		if newCol > lineLen {
			newCol = lineLen
		}
		return cursorRow, newCol
	}

	// Already on last visual line of logical line
	// Check if we can move to next logical line
	if cursorRow == len(lines)-1 {
		// At document end
		return cursorRow, lineLen
	}

	// Move to next logical line
	nextLogicalRow := cursorRow + 1
	nextLine := lines[nextLogicalRow]
	nextLineLen := len(nextLine)

	// Position at desiredCol on the first visual line of next logical line
	newCol := e.desiredCol
	// Clamp to line length
	if newCol > nextLineLen {
		newCol = nextLineLen
	}

	return nextLogicalRow, newCol
}

// moveUp moves cursor up one visual line (accounting for text wrapping)
func (e *Editor) moveUp() {
	newRow, newCol := e.moveVisualLineUp(e.cursorRow, e.cursorCol, e.width, e.lines)
	e.cursorRow = newRow
	e.cursorCol = newCol

	// If cursor was clamped to a shorter position, update desiredCol to match
	// This handles moving to/from empty or short lines correctly
	if e.width > 0 {
		visualCol := e.cursorCol % e.width
		if visualCol != e.desiredCol && e.cursorRow < len(e.lines) {
			// Check if we're at the end of the line (cursor was clamped)
			if e.cursorCol == len(e.lines[e.cursorRow]) {
				e.updateDesiredCol()
			}
		}
	}

	e.ensureCursorVisible()
}

// moveDown moves cursor down one visual line (accounting for text wrapping)
func (e *Editor) moveDown() {
	newRow, newCol := e.moveVisualLineDown(e.cursorRow, e.cursorCol, e.width, e.lines)
	e.cursorRow = newRow
	e.cursorCol = newCol

	// If cursor was clamped to a shorter position, update desiredCol to match
	// This handles moving to/from empty or short lines correctly
	if e.width > 0 {
		visualCol := e.cursorCol % e.width
		if visualCol != e.desiredCol && e.cursorRow < len(e.lines) {
			// Check if we're at the end of the line (cursor was clamped)
			if e.cursorCol == len(e.lines[e.cursorRow]) {
				e.updateDesiredCol()
			}
		}
	}

	e.ensureCursorVisible()
}

// moveLeft moves cursor left one character
func (e *Editor) moveLeft() {
	if e.cursorCol > 0 {
		e.cursorCol--
	} else if e.cursorRow > 0 {
		// Move to end of previous line
		e.cursorRow--
		e.cursorCol = len(e.lines[e.cursorRow])
		e.ensureCursorVisible()
	}
	e.updateDesiredCol()
}

// moveRight moves cursor right one character
func (e *Editor) moveRight() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	if e.cursorCol < len(line) {
		e.cursorCol++
	} else if e.cursorRow < len(e.lines)-1 {
		// At end of line, move to start of next line
		e.cursorRow++
		e.cursorCol = 0
		e.ensureCursorVisible()
	}
	e.updateDesiredCol()
}

// moveToLineStart moves cursor to start of current line
func (e *Editor) moveToLineStart() {
	e.cursorCol = 0
	e.desiredCol = 0
}

// moveToLineEnd moves cursor to end of current line
func (e *Editor) moveToLineEnd() {
	if e.cursorRow < len(e.lines) {
		e.cursorCol = len(e.lines[e.cursorRow])
	}
	e.updateDesiredCol()
}

// isWordChar returns true if rune is part of a word
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// deleteToLineStart deletes from cursor to start of line (Ctrl+U)
func (e *Editor) deleteToLineStart() {
	if e.cursorRow < len(e.lines) {
		deleted := string(e.lines[e.cursorRow][:e.cursorCol])
		e.killBuffer = deleted
		e.lines[e.cursorRow] = e.lines[e.cursorRow][e.cursorCol:]
		e.cursorCol = 0
		e.desiredCol = 0
		if deleted != "" {
			e.dirty = true
		}
	}
}

// deleteToLineEnd deletes from cursor to end of line (Ctrl+K)
func (e *Editor) deleteToLineEnd() {
	if e.cursorRow < len(e.lines) {
		deleted := string(e.lines[e.cursorRow][e.cursorCol:])
		e.killBuffer = deleted
		e.lines[e.cursorRow] = e.lines[e.cursorRow][:e.cursorCol]
		e.updateDesiredCol()
		if deleted != "" {
			e.dirty = true
		}
	}
}

// deleteWordBackward deletes the word before the cursor (Ctrl+W, Alt+Backspace)
func (e *Editor) deleteWordBackward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	if e.cursorCol == 0 {
		// At start of line, delete backwards like backspace
		e.deleteCharBackward()
		return
	}

	startCol := e.cursorCol
	// Skip whitespace backwards
	for e.cursorCol > 0 && !isWordChar(line[e.cursorCol-1]) {
		e.cursorCol--
	}
	// Delete word characters backwards
	for e.cursorCol > 0 && isWordChar(line[e.cursorCol-1]) {
		e.cursorCol--
	}

	// Save deleted text and remove it
	deleted := string(line[e.cursorCol:startCol])
	e.killBuffer = deleted
	e.lines[e.cursorRow] = append(line[:e.cursorCol], line[startCol:]...)
	e.updateDesiredCol()
	if deleted != "" {
		e.dirty = true
	}
}

// jumpWordForward moves cursor to start of next word (Ctrl+Right)
func (e *Editor) jumpWordForward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	// Skip current word
	for e.cursorCol < len(line) && isWordChar(line[e.cursorCol]) {
		e.cursorCol++
	}
	// Skip whitespace
	for e.cursorCol < len(line) && !isWordChar(line[e.cursorCol]) {
		e.cursorCol++
	}

	// If at end of line, move to next line
	if e.cursorCol >= len(line) && e.cursorRow < len(e.lines)-1 {
		e.cursorRow++
		e.cursorCol = 0
		e.ensureCursorVisible()
	}
	e.updateDesiredCol()
}

// jumpWordBackward moves cursor to start of previous word (Ctrl+Left)
func (e *Editor) jumpWordBackward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	if e.cursorCol == 0 {
		// At start of line, move to end of previous line
		if e.cursorRow > 0 {
			e.cursorRow--
			e.cursorCol = len(e.lines[e.cursorRow])
			e.ensureCursorVisible()
		}
		e.updateDesiredCol()
		return
	}

	// Move back one character
	e.cursorCol--
	// Skip whitespace backwards
	for e.cursorCol > 0 && !isWordChar(line[e.cursorCol]) {
		e.cursorCol--
	}
	// Skip word characters backwards
	for e.cursorCol > 0 && isWordChar(line[e.cursorCol-1]) {
		e.cursorCol--
	}
	e.updateDesiredCol()
}

// yankText inserts the killed text at cursor (Ctrl+Y)
func (e *Editor) yankText() {
	if e.killBuffer == "" {
		return
	}

	for _, r := range e.killBuffer {
		if r == '\n' {
			e.insertNewline()
		} else {
			e.insertRune(r)
		}
	}
}

// pageUp scrolls up one page
func (e *Editor) pageUp() {
	e.cursorRow -= e.height
	if e.cursorRow < 0 {
		e.cursorRow = 0
	}
	e.clampCursor()
	e.ensureCursorVisible()
}

// pageDown scrolls down one page
func (e *Editor) pageDown() {
	e.cursorRow += e.height
	if e.cursorRow >= len(e.lines) {
		e.cursorRow = len(e.lines) - 1
	}
	e.clampCursor()
	e.ensureCursorVisible()
}

// moveToTop moves cursor to the very beginning of the document
func (e *Editor) moveToTop() {
	e.cursorRow = 0
	e.cursorCol = 0
	e.desiredCol = 0
	e.ensureCursorVisible()
}

// moveToBottom moves cursor to the very end of the document
func (e *Editor) moveToBottom() {
	if len(e.lines) > 0 {
		e.cursorRow = len(e.lines) - 1
		e.cursorCol = len(e.lines[e.cursorRow])
	}
	e.updateDesiredCol()
	e.ensureCursorVisible()
}

// Update handles keyboard input
func (e *Editor) Update(msg tea.Msg) tea.Cmd {
	if !e.focused {
		return nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Check for help toggle first
		if msg.String() == "ctrl+h" {
			e.showHelp = !e.showHelp
			return nil
		}

		// If help is showing, close on any key
		if e.showHelp {
			e.showHelp = false
			return nil
		}

		switch msg.String() {
		case "enter":
			e.insertNewline()
		case "backspace":
			e.deleteCharBackward()
		case "delete":
			e.deleteCharForward()
		case "up":
			e.moveUp()
		case "down":
			e.moveDown()
		case "left":
			e.moveLeft()
		case "right":
			e.moveRight()
		case "home", "ctrl+a":
			e.moveToLineStart()
		case "end", "ctrl+e":
			e.moveToLineEnd()
		case "ctrl+u":
			e.deleteToLineStart()
		case "ctrl+k":
			e.deleteToLineEnd()
		case "ctrl+w", "alt+backspace":
			e.deleteWordBackward()
		case "ctrl+y":
			e.yankText()
		case "ctrl+left":
			e.jumpWordBackward()
		case "ctrl+right":
			e.jumpWordForward()
		case "pgup":
			e.pageUp()
		case "pgdown":
			e.pageDown()
		case "ctrl+home":
			e.moveToTop()
		case "ctrl+end":
			e.moveToBottom()
		default:
			// Insert regular characters
			if len(msg.Runes) > 0 {
				for _, r := range msg.Runes {
					if r == '\n' || r == '\r' {
						e.insertNewline()
					} else {
						e.insertRune(r)
					}
				}
			}
		}
	}

	return nil
}

// View renders the editor
func (e *Editor) View() string {
	// Show help overlay if requested
	if e.showHelp {
		return e.renderHelp()
	}

	if len(e.lines) == 0 {
		if e.placeholder != "" {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(e.placeholder)
		}
		return ""
	}

	var sb strings.Builder

	// Render visible lines
	endRow := e.viewportRow + e.height
	if endRow > len(e.lines) {
		endRow = len(e.lines)
	}

	for row := e.viewportRow; row < endRow; row++ {
		line := e.lines[row]

		// Check if this is the cursor line and we're focused
		if e.focused && row == e.cursorRow {
			// Render line with cursor
			if e.cursorCol < len(line) {
				// Cursor is on a character
				sb.WriteString(string(line[:e.cursorCol]))
				cursorChar := string(line[e.cursorCol])
				sb.WriteString(lipgloss.NewStyle().Reverse(true).Render(cursorChar))
				sb.WriteString(string(line[e.cursorCol+1:]))
			} else {
				// Cursor is at end of line - show space cursor
				sb.WriteString(string(line))
				sb.WriteString(lipgloss.NewStyle().Reverse(true).Render(" "))
			}
		} else {
			// Regular line without cursor
			sb.WriteString(string(line))
		}

		// Add newline except for last line
		if row < endRow-1 || endRow < len(e.lines) {
			sb.WriteRune('\n')
		}
	}

	// Show placeholder if empty and not focused
	if len(e.lines) == 1 && len(e.lines[0]) == 0 && !e.focused && e.placeholder != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(e.placeholder)
	}

	return sb.String()
}

// renderHelp renders the help overlay showing all keybindings
func (e *Editor) renderHelp() string {
	helpText := `
╔══════════════════════════════════════════════════════════════╗
║                    EDITOR KEYBINDINGS                        ║
╠══════════════════════════════════════════════════════════════╣
║                                                              ║
║  NAVIGATION                                                  ║
║    ↑↓←→              Move by character/line                 ║
║    Home / Ctrl+A     Start of current line                  ║
║    End  / Ctrl+E     End of current line                    ║
║    Ctrl+Home         Start of entire document               ║
║    Ctrl+End          End of entire document                 ║
║    Page Up/Down      Scroll by page                         ║
║    Ctrl+Left         Jump word backward                     ║
║    Ctrl+Right        Jump word forward                      ║
║                                                              ║
║  EDITING                                                     ║
║    Enter             New line                               ║
║    Backspace         Delete character backward              ║
║    Delete            Delete character forward               ║
║    Ctrl+U            Delete to line start                   ║
║    Ctrl+K            Delete to line end                     ║
║    Ctrl+W            Delete word backward                   ║
║    Alt+Backspace     Delete word backward                   ║
║    Ctrl+Y            Yank (paste) killed text               ║
║                                                              ║
║  OTHER                                                       ║
║    Ctrl+H            Toggle this help                       ║
║    #                 Tag picker                             ║
║    Esc               Save and close note                    ║
║    Ctrl+E            Open in external editor                ║
║                                                              ║
║  Press any key to close this help                           ║
╚══════════════════════════════════════════════════════════════╝
`

	// Center the help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("235")).
		Padding(1, 2)

	return helpStyle.Render(helpText)
}
