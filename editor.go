package main

import (
	"encoding/base64"
	"fmt"
	"os"
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
	viewportRow int      // Top visible visual line for scrolling
	width       int      // Editor width
	height      int      // Editor height
	placeholder string   // Placeholder text when empty
	focused     bool     // Whether editor is focused
	killBuffer  string   // Killed text for yank (Ctrl+Y)
	showHelp    bool     // Whether to show help overlay
	dirty       bool     // Whether there are unsaved changes
	// Mouse selection state
	selecting       bool // Left mouse button is held (actively dragging)
	hasSelection    bool // A selection exists (persists after mouse release)
	selectionAnchor int  // Character offset where selection started
	yOffset         int  // Editor's Y position in terminal (for mouse coord translation)
}

// New creates a new editor
func NewEditor() Editor {
	return Editor{
		lines:           [][]rune{{}}, // Start with one empty line
		cursorRow:       0,
		cursorCol:       0,
		desiredCol:      0,
		viewportRow:     0,
		width:           80,
		height:          24,
		focused:         false,
		selectionAnchor: -1,
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

// SetYOffset sets the Y offset of the editor in the terminal
func (e *Editor) SetYOffset(y int) {
	e.yOffset = y
}

// copyToPrimarySelection sets the primary selection using OSC 52 escape sequence
func copyToPrimarySelection(text string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	// Write OSC 52 directly to the terminal
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		fmt.Fprintf(tty, "\x1b]52;p;%s\x1b\\", encoded)
		tty.Close()
	}
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

// logicalToVisualRow converts a logical row and column to a global visual row.
// This accounts for line wrapping: each logical line may span multiple visual lines.
func (e *Editor) logicalToVisualRow(logicalRow, col int) int {
	visual := 0
	for i := 0; i < logicalRow && i < len(e.lines); i++ {
		visual += e.countVisualLines(e.lines[i], e.width)
	}
	if e.width > 0 && col > 0 {
		visual += col / e.width
	}
	return visual
}

// visualRowToLogical converts a global visual row to a logical row and the
// visual line offset within that logical row.
func (e *Editor) visualRowToLogical(visualRow int) (logicalRow int, visualOffset int) {
	if visualRow <= 0 {
		return 0, 0
	}
	visual := 0
	for i, line := range e.lines {
		vl := e.countVisualLines(line, e.width)
		if visual+vl > visualRow {
			return i, visualRow - visual
		}
		visual += vl
	}
	// Past the end - return last line
	if len(e.lines) > 0 {
		return len(e.lines) - 1, 0
	}
	return 0, 0
}

// totalVisualLines returns the total number of visual lines in the document.
func (e *Editor) totalVisualLines() int {
	total := 0
	for _, line := range e.lines {
		total += e.countVisualLines(line, e.width)
	}
	return total
}

// ensureCursorVisible adjusts viewport to keep cursor visible.
// viewportRow is tracked in visual lines to correctly handle wrapped lines.
func (e *Editor) ensureCursorVisible() {
	cursorVisual := e.logicalToVisualRow(e.cursorRow, e.cursorCol)
	// Scroll down if cursor is below viewport
	if cursorVisual >= e.viewportRow+e.height {
		e.viewportRow = cursorVisual - e.height + 1
	}
	// Scroll up if cursor is above viewport
	if cursorVisual < e.viewportRow {
		e.viewportRow = cursorVisual
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

// clearSelection clears any active selection
func (e *Editor) clearSelection() {
	e.hasSelection = false
	e.selecting = false
	e.selectionAnchor = -1
}

// deleteSelection deletes the currently selected text and places cursor at start of selection
func (e *Editor) deleteSelection() {
	if !e.hasSelection || e.selectionAnchor < 0 {
		return
	}
	cursorOff := e.GetCursor()
	startOff := e.selectionAnchor
	endOff := cursorOff
	if startOff > endOff {
		startOff, endOff = endOff, startOff
	}

	text := []rune(e.Value())
	if startOff > len(text) {
		startOff = len(text)
	}
	if endOff > len(text) {
		endOff = len(text)
	}

	newText := string(text[:startOff]) + string(text[endOff:])
	e.SetValue(newText)
	e.SetCursor(startOff)
	e.clearSelection()
	e.dirty = true
}

// mouseToPosition converts terminal mouse coordinates to editor (row, col)
func (e *Editor) mouseToPosition(mouseX, mouseY int) (int, int) {
	editorY := mouseY - e.yOffset
	if editorY < 0 {
		editorY = 0
	}
	if editorY >= e.height {
		editorY = e.height - 1
	}

	globalVisual := e.viewportRow + editorY
	logicalRow, visualOffset := e.visualRowToLogical(globalVisual)

	col := visualOffset*e.width + mouseX
	if logicalRow < len(e.lines) {
		if col > len(e.lines[logicalRow]) {
			col = len(e.lines[logicalRow])
		}
	}
	if col < 0 {
		col = 0
	}

	return logicalRow, col
}

// getSelectedText returns the text within the current selection
func (e *Editor) getSelectedText() string {
	if !e.hasSelection || e.selectionAnchor < 0 {
		return ""
	}
	cursorOff := e.GetCursor()
	startOff := e.selectionAnchor
	endOff := cursorOff
	if startOff > endOff {
		startOff, endOff = endOff, startOff
	}

	text := []rune(e.Value())
	if startOff > len(text) {
		startOff = len(text)
	}
	if endOff > len(text) {
		endOff = len(text)
	}
	return string(text[startOff:endOff])
}

// selectionRange returns the ordered selection range as (startRow, startCol, endRow, endCol).
// Returns (-1, -1, -1, -1) if no selection.
func (e *Editor) selectionRange() (int, int, int, int) {
	if !e.hasSelection || e.selectionAnchor < 0 {
		return -1, -1, -1, -1
	}

	cursorOff := e.GetCursor()
	startOff := e.selectionAnchor
	endOff := cursorOff
	if startOff > endOff {
		startOff, endOff = endOff, startOff
	}

	// Convert startOff to row, col
	sRow, sCol := 0, 0
	off := 0
	for i, line := range e.lines {
		if off+len(line) >= startOff {
			sRow = i
			sCol = startOff - off
			break
		}
		off += len(line) + 1
	}

	// Convert endOff to row, col
	eRow, eCol := 0, 0
	off = 0
	for i, line := range e.lines {
		if off+len(line) >= endOff {
			eRow = i
			eCol = endOff - off
			break
		}
		off += len(line) + 1
	}

	return sRow, sCol, eRow, eCol
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
	e.ensureCursorVisible()
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
	if e.width > 0 {
		visualCol := e.cursorCol % e.width
		if visualCol != e.desiredCol && e.cursorRow < len(e.lines) {
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
	if e.width > 0 {
		visualCol := e.cursorCol % e.width
		if visualCol != e.desiredCol && e.cursorRow < len(e.lines) {
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
		e.cursorRow--
		e.cursorCol = len(e.lines[e.cursorRow])
	}
	e.updateDesiredCol()
	e.ensureCursorVisible()
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
		e.cursorRow++
		e.cursorCol = 0
	}
	e.updateDesiredCol()
	e.ensureCursorVisible()
}

// moveToLineStart moves cursor to start of current line
func (e *Editor) moveToLineStart() {
	e.cursorCol = 0
	e.desiredCol = 0
	e.ensureCursorVisible()
}

// moveToLineEnd moves cursor to end of current line
func (e *Editor) moveToLineEnd() {
	if e.cursorRow < len(e.lines) {
		e.cursorCol = len(e.lines[e.cursorRow])
	}
	e.updateDesiredCol()
	e.ensureCursorVisible()
}

// isWordChar returns true if rune is part of a word
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// deleteToLineStart deletes from cursor to start of line (Ctrl+U)
// deleteToLineStart deletes from cursor to start of line (Ctrl+U)
// If cursor is already at start of line, eats the newline (joins with previous line)
func (e *Editor) deleteToLineStart() {
	if e.cursorRow >= len(e.lines) {
		return
	}
	if e.cursorCol > 0 {
		// Text before cursor: delete it
		deleted := string(e.lines[e.cursorRow][:e.cursorCol])
		e.killBuffer = deleted
		e.lines[e.cursorRow] = e.lines[e.cursorRow][e.cursorCol:]
		e.cursorCol = 0
		e.dirty = true
	} else if e.cursorRow > 0 {
		// At start of line: join with previous line (eat the newline)
		e.killBuffer = "\n"
		prevLine := e.lines[e.cursorRow-1]
		currentLine := e.lines[e.cursorRow]
		e.cursorCol = len(prevLine)
		e.lines[e.cursorRow-1] = append(prevLine, currentLine...)
		e.lines = append(e.lines[:e.cursorRow], e.lines[e.cursorRow+1:]...)
		e.cursorRow--
		e.dirty = true
	}
	e.desiredCol = 0
	e.ensureCursorVisible()
}

// deleteToLineEnd deletes from cursor to end of line (Ctrl+K)
// If cursor is already at end of line, eats the newline (joins with next line)
func (e *Editor) deleteToLineEnd() {
	if e.cursorRow >= len(e.lines) {
		return
	}
	line := e.lines[e.cursorRow]
	if e.cursorCol < len(line) {
		// Text after cursor: delete it
		deleted := string(line[e.cursorCol:])
		e.killBuffer = deleted
		e.lines[e.cursorRow] = line[:e.cursorCol]
		e.dirty = true
	} else if e.cursorRow < len(e.lines)-1 {
		// At end of line: join with next line (eat the newline)
		e.killBuffer = "\n"
		nextLine := e.lines[e.cursorRow+1]
		e.lines[e.cursorRow] = append(line, nextLine...)
		e.lines = append(e.lines[:e.cursorRow+1], e.lines[e.cursorRow+2:]...)
		e.dirty = true
	}
	e.updateDesiredCol()
}

// deleteWordBackward deletes the word before the cursor (Ctrl+W, Alt+Backspace)
func (e *Editor) deleteWordBackward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	if e.cursorCol == 0 {
		e.deleteCharBackward()
		return
	}

	startCol := e.cursorCol
	for e.cursorCol > 0 && !isWordChar(line[e.cursorCol-1]) {
		e.cursorCol--
	}
	for e.cursorCol > 0 && isWordChar(line[e.cursorCol-1]) {
		e.cursorCol--
	}

	deleted := string(line[e.cursorCol:startCol])
	e.killBuffer = deleted
	e.lines[e.cursorRow] = append(line[:e.cursorCol], line[startCol:]...)
	e.updateDesiredCol()
	if deleted != "" {
		e.dirty = true
	}
	e.ensureCursorVisible()
}

// jumpWordForward moves cursor to start of next word (Ctrl+Right)
func (e *Editor) jumpWordForward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	for e.cursorCol < len(line) && isWordChar(line[e.cursorCol]) {
		e.cursorCol++
	}
	for e.cursorCol < len(line) && !isWordChar(line[e.cursorCol]) {
		e.cursorCol++
	}

	if e.cursorCol >= len(line) && e.cursorRow < len(e.lines)-1 {
		e.cursorRow++
		e.cursorCol = 0
	}
	e.updateDesiredCol()
	e.ensureCursorVisible()
}

// jumpWordBackward moves cursor to start of previous word (Ctrl+Left)
func (e *Editor) jumpWordBackward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := e.lines[e.cursorRow]
	if e.cursorCol == 0 {
		if e.cursorRow > 0 {
			e.cursorRow--
			e.cursorCol = len(e.lines[e.cursorRow])
		}
		e.updateDesiredCol()
		e.ensureCursorVisible()
		return
	}

	e.cursorCol--
	for e.cursorCol > 0 && !isWordChar(line[e.cursorCol]) {
		e.cursorCol--
	}
	for e.cursorCol > 0 && isWordChar(line[e.cursorCol-1]) {
		e.cursorCol--
	}
	e.updateDesiredCol()
	e.ensureCursorVisible()
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
	e.viewportRow -= e.height
	if e.viewportRow < 0 {
		e.viewportRow = 0
	}
	for i := 0; i < e.height; i++ {
		newRow, newCol := e.moveVisualLineUp(e.cursorRow, e.cursorCol, e.width, e.lines)
		if newRow == e.cursorRow && newCol == e.cursorCol {
			break
		}
		e.cursorRow = newRow
		e.cursorCol = newCol
	}
	e.clampCursor()
	e.ensureCursorVisible()
}

// pageDown scrolls down one page
func (e *Editor) pageDown() {
	e.viewportRow += e.height
	maxVisual := e.totalVisualLines() - e.height
	if maxVisual < 0 {
		maxVisual = 0
	}
	if e.viewportRow > maxVisual {
		e.viewportRow = maxVisual
	}
	for i := 0; i < e.height; i++ {
		newRow, newCol := e.moveVisualLineDown(e.cursorRow, e.cursorCol, e.width, e.lines)
		if newRow == e.cursorRow && newCol == e.cursorCol {
			break
		}
		e.cursorRow = newRow
		e.cursorCol = newCol
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

// scrollUp scrolls the viewport up by n visual lines
func (e *Editor) scrollUp(n int) {
	e.viewportRow -= n
	if e.viewportRow < 0 {
		e.viewportRow = 0
	}
}

// scrollDown scrolls the viewport down by n visual lines
func (e *Editor) scrollDown(n int) {
	e.viewportRow += n
	maxVisual := e.totalVisualLines() - e.height
	if maxVisual < 0 {
		maxVisual = 0
	}
	if e.viewportRow > maxVisual {
		e.viewportRow = maxVisual
	}
}

// Update handles keyboard and mouse input
func (e *Editor) Update(msg tea.Msg) tea.Cmd {
	if !e.focused {
		return nil
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		mouseEvent := tea.MouseEvent(msg)

		switch {
		case mouseEvent.Button == tea.MouseButtonLeft && mouseEvent.Action == tea.MouseActionPress:
			// Start selection: place cursor and set anchor
			row, col := e.mouseToPosition(mouseEvent.X, mouseEvent.Y)
			e.cursorRow = row
			e.cursorCol = col
			e.clampCursor()
			e.updateDesiredCol()
			e.selectionAnchor = e.GetCursor()
			e.selecting = true
			e.hasSelection = false

		case mouseEvent.Action == tea.MouseActionMotion && e.selecting:
			// Extend selection during drag
			row, col := e.mouseToPosition(mouseEvent.X, mouseEvent.Y)
			e.cursorRow = row
			e.cursorCol = col
			e.clampCursor()
			e.updateDesiredCol()
			if e.GetCursor() != e.selectionAnchor {
				e.hasSelection = true
			}

		case mouseEvent.Button == tea.MouseButtonLeft && mouseEvent.Action == tea.MouseActionRelease:
			// End drag: copy selection to kill buffer and primary selection
			if e.selecting && e.hasSelection {
				e.killBuffer = e.getSelectedText()
				copyToPrimarySelection(e.killBuffer)
			}
			e.selecting = false

		case mouseEvent.Button == tea.MouseButtonWheelUp:
			e.scrollUp(3)
			if e.selecting {
				// Extend selection while scrolling
				row, col := e.mouseToPosition(mouseEvent.X, mouseEvent.Y)
				e.cursorRow = row
				e.cursorCol = col
				e.clampCursor()
				if e.GetCursor() != e.selectionAnchor {
					e.hasSelection = true
				}
			}

		case mouseEvent.Button == tea.MouseButtonWheelDown:
			e.scrollDown(3)
			if e.selecting {
				// Extend selection while scrolling
				row, col := e.mouseToPosition(mouseEvent.X, mouseEvent.Y)
				e.cursorRow = row
				e.cursorCol = col
				e.clampCursor()
				if e.GetCursor() != e.selectionAnchor {
					e.hasSelection = true
				}
			}

		case mouseEvent.Button == tea.MouseButtonMiddle && mouseEvent.Action == tea.MouseActionPress:
			// Middle click: place cursor and paste kill buffer
			row, col := e.mouseToPosition(mouseEvent.X, mouseEvent.Y)
			e.cursorRow = row
			e.cursorCol = col
			e.clampCursor()
			e.updateDesiredCol()
			e.clearSelection()
			e.yankText()
		}
		return nil

	case tea.KeyMsg:
		// Handle selection: delete/backspace replace selection, other keys clear it
		if e.hasSelection {
			switch msg.String() {
			case "backspace", "delete":
				e.deleteSelection()
				return nil
			case "enter":
				e.deleteSelection()
				e.insertNewline()
				return nil
			case "ctrl+h", "up", "down", "left", "right", "home", "end",
				"ctrl+left", "ctrl+right", "ctrl+home", "ctrl+end",
				"pgup", "pgdown", "escape":
				// Navigation/toggle keys just clear selection
				e.clearSelection()
			default:
				// Typing replaces selection
				if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
					e.deleteSelection()
					// Fall through to normal insert below
				} else {
					e.clearSelection()
				}
			}
		}

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
	reverseStyle := lipgloss.NewStyle().Reverse(true)
	selStyle := lipgloss.NewStyle().Background(lipgloss.Color("69")).Foreground(lipgloss.Color("255"))

	// Get selection range in row/col coordinates
	selStartRow, selStartCol, selEndRow, selEndCol := e.selectionRange()

	// Convert visual viewport position to starting logical line
	startLogical, startVisualOffset := e.visualRowToLogical(e.viewportRow)
	visualLinesRendered := 0

	// Track character offset incrementally for logical lines before viewport
	lineOffset := 0
	for i := 0; i < startLogical; i++ {
		lineOffset += len(e.lines[i]) + 1
	}

	// Render individual visual lines for consistent output height.
	for row := startLogical; row < len(e.lines) && visualLinesRendered < e.height; row++ {
		line := e.lines[row]
		lineVisualLines := e.countVisualLines(line, e.width)

		firstVisual := 0
		if row == startLogical {
			firstVisual = startVisualOffset
		}

		for v := firstVisual; v < lineVisualLines && visualLinesRendered < e.height; v++ {
			startCol := v * e.width
			endCol := startCol + e.width
			if endCol > len(line) {
				endCol = len(line)
			}

			if visualLinesRendered > 0 {
				sb.WriteRune('\n')
			}

			segment := line[startCol:endCol]

			// Determine selection bounds within this segment
			segSelStart := -1
			segSelEnd := -1
			if selStartRow >= 0 {
				// Check if this row overlaps with selection
				if row > selStartRow && row < selEndRow {
					// Entire line is selected
					segSelStart = 0
					segSelEnd = len(segment)
				} else if row == selStartRow && row == selEndRow {
					// Selection starts and ends on this row
					ss := selStartCol - startCol
					se := selEndCol - startCol
					if ss < 0 {
						ss = 0
					}
					if se > len(segment) {
						se = len(segment)
					}
					if ss < se {
						segSelStart = ss
						segSelEnd = se
					}
				} else if row == selStartRow {
					// Selection starts on this row
					ss := selStartCol - startCol
					if ss < 0 {
						ss = 0
					}
					if ss < len(segment) {
						segSelStart = ss
						segSelEnd = len(segment)
					}
				} else if row == selEndRow {
					// Selection ends on this row
					se := selEndCol - startCol
					if se < 0 {
						se = 0
					}
					if se > len(segment) {
						se = len(segment)
					}
					if se > 0 {
						segSelStart = 0
						segSelEnd = se
					}
				}
			}

			// Cursor position within this segment
			cursorPos := -1
			if e.focused && row == e.cursorRow {
				localCol := e.cursorCol - startCol
				if localCol >= 0 && localCol < len(segment) {
					cursorPos = localCol
				}
			}

			// Render the segment with selection highlighting and cursor
			e.renderSegment(&sb, segment, cursorPos, segSelStart, segSelEnd, reverseStyle, selStyle)

			// Handle cursor at end of logical line (on last visual line)
			if e.focused && row == e.cursorRow && e.cursorCol == len(line) &&
				v == lineVisualLines-1 && e.cursorCol-startCol == len(segment) {
				sb.WriteString(reverseStyle.Render(" "))
			}

			// Handle end-of-line selection marker (newline is "selected")
			if segSelStart >= 0 && row >= selStartRow && row < selEndRow &&
				v == lineVisualLines-1 && !(e.focused && row == e.cursorRow && e.cursorCol == len(line)) {
				sb.WriteString(selStyle.Render(" "))
			}

			visualLinesRendered++
		}

		// Handle cursor at end of line when line length is exact multiple of width
		if e.focused && row == e.cursorRow && e.cursorCol == len(line) &&
			len(line) > 0 && e.width > 0 && len(line)%e.width == 0 &&
			visualLinesRendered < e.height {
			if visualLinesRendered > 0 {
				sb.WriteRune('\n')
			}
			sb.WriteString(reverseStyle.Render(" "))
			visualLinesRendered++
		}

		lineOffset += len(line) + 1
	}

	// Show placeholder if empty and not focused
	if len(e.lines) == 1 && len(e.lines[0]) == 0 && !e.focused && e.placeholder != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(e.placeholder)
	}

	return sb.String()
}

// renderSegment renders a segment with batched styling for cursor and selection.
func (e *Editor) renderSegment(sb *strings.Builder, segment []rune, cursorPos, selStart, selEnd int, reverseStyle, selStyle lipgloss.Style) {
	if len(segment) == 0 {
		return
	}

	// No selection and no cursor: fast path
	if selStart < 0 && cursorPos < 0 {
		sb.WriteString(string(segment))
		return
	}

	// Render in styled runs
	i := 0
	for i < len(segment) {
		isCur := i == cursorPos
		isSel := selStart >= 0 && i >= selStart && i < selEnd

		if isCur {
			// Cursor is always a single character
			sb.WriteString(reverseStyle.Render(string(segment[i : i+1])))
			i++
			continue
		}

		// Find end of current run (same style, not cursor)
		runEnd := i + 1
		for runEnd < len(segment) && runEnd != cursorPos {
			nextSel := selStart >= 0 && runEnd >= selStart && runEnd < selEnd
			if nextSel != isSel {
				break
			}
			runEnd++
		}

		text := string(segment[i:runEnd])
		if isSel {
			sb.WriteString(selStyle.Render(text))
		} else {
			sb.WriteString(text)
		}
		i = runEnd
	}
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
║  MOUSE                                                       ║
║    Click             Place cursor                           ║
║    Drag              Select text                            ║
║    Wheel+Drag        Scroll and extend selection            ║
║    Middle click      Paste last selection                   ║
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

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("235")).
		Padding(1, 2)

	return helpStyle.Render(helpText)
}
