# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a terminal-based note-taking application called "notes" built with Go using the Bubble Tea TUI framework. It provides a hierarchical note organization system with markdown support, tagging, favorites, and trash functionality.

## Build and Run Commands

- **Build**: `go build -o notes` (compiles all .go files in directory)
- **Run**: `./notes`
- **Version check**: `./notes -v` or `./notes --version`
- **Install dependencies**: `go mod download`
- **Update dependencies**: `go mod tidy`

**Important**: Always use `go build -o notes` (without specifying files) to ensure both `main.go` and `editor.go` are compiled together.

## Version Management

The application version is stored in the `VERSION` file and embedded into the binary at compile time using `//go:embed VERSION`. When making significant changes to the codebase, increment the version number in the VERSION file following semantic versioning (MAJOR.MINOR.PATCH).

Current version format: Single line with version number (e.g., "0.5.2")

## Architecture

### Main Components

1. **Model-View-Update (MVU) Pattern**: Uses Bubble Tea's MVU architecture
   - `model` struct: Application state including view mode, current node, custom editor, cursor positions
   - `Update()`: Handles all user input and state transitions
   - `View()`: Renders the UI based on current state

2. **Custom Text Editor** (`editor.go`):
   - Built-in text editor with full cursor position tracking
   - Implements `Editor` struct with `[][]rune` buffer for line-based editing
   - Provides `GetCursor()` and `SetCursor()` methods for persistent cursor positions
   - Supports advanced keyboard shortcuts (Ctrl+U/K/W/Y, Ctrl+Left/Right, etc.)
   - Includes viewport management for scrolling long documents
   - Kill buffer for cut/yank operations (Emacs-style)
   - Built-in help overlay (Ctrl+H) showing all keybindings

3. **View Modes** (main.go):
   - `navigationView`: Browse notes and folders
   - `editingView`: Edit note content using custom editor
   - `creatingFolderView`: Create new folders
   - `trashView`: View and manage deleted items
   - `tagBrowserView`: Browse notes by tags
   - `configView`: Configure application settings
   - `helpView`: Display help information

4. **Note Structure**:
   - Tree-based hierarchy with parent/child relationships
   - Each note can be a directory or markdown file
   - Supports favorites, tags, and metadata

5. **Cursor Position Persistence**:
   - Cursor positions saved to `~/.config/notes/cursor_positions.json`
   - Maps file paths to character offsets
   - Automatically restored when reopening notes
   - Positions saved on every note save/close

### Configuration System

- Config location: `~/.config/notes/config.json`
- Default notes path: `~/Documents/notes`
- Configurable external editor (default: nano)
- Customizable color scheme using ANSI color codes (0-255):
  - Title bar colors (background/foreground)
  - Status bar colors (background/foreground)
  - Border color
  - Selected item foreground color
  - Favorite item color
  - Tag bar colors (background/foreground for normal and selected tags)
- Configuration is loaded at startup and can be modified via the config view (press 'c')

### Data Storage

- Notes stored as `.md` files in hierarchical folders
- Trash stored in `.trash` subdirectory within notes path
- Favorite metadata stored as `favorite: true\n` prefix in file content
- Tags extracted from content using regex pattern: `(^|\s)#(\w+)`
- Cursor positions stored in `~/.config/notes/cursor_positions.json` as path->offset map

### Key Functions

**Main Application (main.go)**:
- `loadNotes()`: Recursively walks directory and builds note tree
- `sanitizeTitle()`: Converts user input to filesystem-safe names (removes special chars, replaces spaces with hyphens)
- `collectAllTags()` / `getAllTags()`: Extracts all tags from note tree
- `updateNavigationView()`, `updateEditingView()`, etc.: Handle input for each view mode
- `loadCursorPositions()`: Loads saved cursor positions from JSON file
- `saveCursorPositions()`: Persists cursor positions to JSON file
- `getCursorPositionsPath()`: Returns path to cursor positions file

**Custom Editor (editor.go)**:
- `NewEditor()`: Creates a new editor instance
- `SetValue()` / `Value()`: Set and get editor text content
- `SetCursor()` / `GetCursor()`: Set and get cursor position as character offset
- `Update()`: Handles keyboard input and editor operations
- `View()`: Renders editor content with cursor
- Movement: `moveUp/Down/Left/Right()`, `moveToLineStart/End()`, `moveToTop/Bottom()`
- Editing: `insertRune()`, `insertNewline()`, `deleteCharBackward/Forward()`
- Advanced: `deleteToLineStart/End()`, `deleteWordBackward()`, `jumpWordForward/Backward()`, `yankText()`

## Development Notes

### General
- The application uses `filepath.WalkDir` to load the note hierarchy on startup
- External editor integration via `tea.ExecProcess` for opening notes in user-configured editor (Ctrl+E)
- Sort modes: by name (alphabetical) or by date (modification time)
- All file operations create parent directories as needed with 0755 permissions
- Notes with spaces in titles are converted to hyphens on disk but displayed with spaces in UI

### Tag Picker
- Triggered by typing '#' in editing view
- Displays as horizontal bar above status bar (non-intrusive design)
- Live filters tags as you type
- Arrow keys to navigate, Enter to select, Esc to cancel
- Colors are fully configurable via config view
- Shows as: `Tags: #filter │ #tag1 #tag2 #tag3`

### Custom Editor Features
- **Traditional cursor behavior**: Cursor cannot move beyond line end (stable and predictable)
- **Keyboard shortcuts**:
  - `Ctrl+U`: Delete from cursor to line start
  - `Ctrl+K`: Delete from cursor to line end
  - `Ctrl+W` / `Alt+Backspace`: Delete word backward
  - `Ctrl+Y`: Yank (paste) killed text
  - `Ctrl+Left/Right`: Jump by word
  - `Ctrl+Home/End`: Jump to document start/end
  - `Ctrl+H`: Toggle help overlay showing all keybindings
- **Line-based buffer**: Uses `[][]rune` for efficient text manipulation
- **Viewport scrolling**: Automatically keeps cursor visible when editing long documents
- **Cursor persistence**: Character offset saved per file, restored on reopen

### Important Implementation Notes
- The custom editor (`editor.go`) must always be compiled alongside `main.go`
- Use `go build -o notes` (not `go build -o notes main.go`) to compile all files
- Cursor position is saved as character offset, not row/col, for consistency across edits
- The `clampCursor()` function ensures cursor never exceeds line boundaries
