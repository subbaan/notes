# Notes

A terminal-based note-taking application built with Go using the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework.

## Features

- Hierarchical note organization with folders
- Markdown file storage
- Tag support (`#tagname` in notes)
- Favorites
- Trash with restore functionality
- Built-in text editor with Emacs-style keybindings
- External editor support
- Customizable color scheme (256 colors)
- Cursor position persistence across sessions

## Installation

```bash
go build -o notes
```

## Usage

```bash
./notes
```

### Navigation View

| Key | Action |
|-----|--------|
| `↑`/`↓` or `k`/`j` | Navigate up/down |
| `←` or `Esc` | Go back to parent folder |
| `→` or `Enter` | Open note/folder |
| `n` | Create new note |
| `F` | Create new folder |
| `f` | Toggle favorite |
| `t` | Toggle sort (name/date) |
| `r` | Rename note/folder |
| `d` | Move to trash |
| `g` | Open tag browser |
| `c` | Open configuration |
| `Ctrl+t` | View trash |
| `Ctrl+e` | Open in external editor |
| `?` | Show help |
| `q` | Quit |

### Editing View

| Key | Action |
|-----|--------|
| `Esc` | Save and close |
| `Ctrl+s` | Save |
| `Ctrl+e` | Open in external editor |
| `#` | Trigger tag picker |
| `Ctrl+h` | Show editor help |

### Editor Keybindings

| Key | Action |
|-----|--------|
| `Ctrl+a` / `Home` | Start of line |
| `Ctrl+e` / `End` | End of line |
| `Ctrl+u` | Delete to line start |
| `Ctrl+k` | Delete to line end |
| `Ctrl+w` | Delete word backward |
| `Ctrl+y` | Yank (paste) killed text |
| `Ctrl+←`/`→` | Jump by word |
| `Ctrl+Home`/`End` | Jump to document start/end |

## Configuration

Configuration is stored at `~/.config/notes/config.json`:

- **Notes path**: Where notes are stored (default: `~/Documents/notes`)
- **External editor**: Editor command for `Ctrl+e` (default: `nano`)
- **Colors**: Full 256-color customization for UI elements

Press `c` in navigation view to configure.

## Data Storage

- Notes are stored as `.md` files in the configured notes path
- Favorites are marked with `favorite: true` prefix in files
- Trash is stored in `.trash` subdirectory
- Cursor positions saved to `~/.config/notes/cursor_positions.json`

## License

MIT
