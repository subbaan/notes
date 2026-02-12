package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

//go:embed VERSION
var versionFile string

func getVersion() string {
	return strings.TrimSpace(versionFile)
}

type ColorConfig struct {
	TitleBg         int `json:"title_bg"`
	TitleFg         int `json:"title_fg"`
	StatusBg        int `json:"status_bg"`
	StatusFg        int `json:"status_fg"`
	BorderColor     int `json:"border_color"`
	SelectedFg      int `json:"selected_fg"`
	FavoriteColor   int `json:"favorite_color"`
	TagBarBg        int `json:"tag_bar_bg"`
	TagBarFg        int `json:"tag_bar_fg"`
	TagSelectedBg   int `json:"tag_selected_bg"`
	TagSelectedFg   int `json:"tag_selected_fg"`
}

type Config struct {
	NotesPath      string      `json:"notes_path"`
	ExternalEditor string      `json:"external_editor"`
	Colors         ColorConfig `json:"colors"`
}

var (
	config       Config
	notesPath    string
	nonAlphanum  = regexp.MustCompile(`[^a-zA-Z0-9_ ]+`)
	tagRegex     = regexp.MustCompile(`(^|\s)#(\w+)`)
	statusStyle  lipgloss.Style
	contentStyle lipgloss.Style
	titleStyle   lipgloss.Style
	borderStyle  lipgloss.Style
	selectedStyle lipgloss.Style
	favoriteStyle lipgloss.Style
)

func getConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "notes", "config.json")
}

func getCursorPositionsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "notes", "cursor_positions.json")
}

func loadCursorPositions() map[string]int {
	positions := make(map[string]int)
	data, err := os.ReadFile(getCursorPositionsPath())
	if err != nil {
		return positions // Return empty map if file doesn't exist
	}
	_ = json.Unmarshal(data, &positions) // Ignore error, return empty/partial map on failure
	return positions
}

func saveCursorPositions(positions map[string]int) error {
	configDir := filepath.Dir(getCursorPositionsPath())
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(positions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getCursorPositionsPath(), data, 0644)
}

func getDefaultConfig() Config {
	homeDir, _ := os.UserHomeDir()
	return Config{
		NotesPath:      filepath.Join(homeDir, "Documents", "notes"),
		ExternalEditor: "nano",
		Colors: ColorConfig{
			TitleBg:       4,   // Blue
			TitleFg:       15,  // Bright White
			StatusBg:      8,   // Dark Gray
			StatusFg:      7,   // Light Gray
			BorderColor:   12,  // Bright Blue
			SelectedFg:    11,  // Bright Yellow
			FavoriteColor: 9,   // Bright Red
			TagBarBg:      235, // Dark Gray
			TagBarFg:      250, // Light Gray
			TagSelectedBg: 11,  // Bright Yellow
			TagSelectedFg: 0,   // Black
		},
	}
}

func loadConfig() Config {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config doesn't exist, create default
		cfg := getDefaultConfig()
		saveConfig(cfg)
		return cfg
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("Error parsing config, using defaults: %v", err)
		return getDefaultConfig()
	}
	return cfg
}

func saveConfig(cfg Config) error {
	configPath := getConfigPath()
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func applyColorConfig() {
	titleStyle = lipgloss.NewStyle().
		Background(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TitleBg))).
		Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TitleFg))).
		Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
		Background(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusBg))).
		Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusFg)))

	borderStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.BorderColor)))

	selectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.SelectedFg))).
		Bold(true)

	favoriteStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.FavoriteColor)))

	contentStyle = lipgloss.NewStyle()
}

type viewMode int
type sortMode int

const (
	navigationView viewMode = iota
	editingView
	creatingFolderView
	trashView
	tagBrowserView
	configView
	helpView
)

const (
	sortByName sortMode = iota
	sortByDate
)

type note struct {
	title    string
	content  string
	path     string
	isDir    bool
	favorite bool
	tags     []string
	children []*note
	parent   *note
	modTime  os.FileInfo
}

type model struct {
	mode          viewMode
	previousMode  viewMode
	currentNode   *note
	trashNode     *note
	cursor        int
	sort          sortMode
	editor        Editor
	quitting      bool
	isNameTaken   bool
	width         int
	height        int
	allTags       []string
	selectedTag   string
	filteredNotes []*note
	configCursor  int
	tempConfig    ColorConfig
	editingPath   bool
	pathInput     string
	editingEditor bool
	editorInput   string
	// Tag picker state
	showTagPicker     bool
	tagPickerFilter   string
	tagPickerCursor   int
	tagPickerFiltered []string
	// Cursor position tracking
	cursorPositions map[string]int // note path -> cursor position
	currentNotePath string         // path of currently edited note
	// Rename popup state
	showRenamePopup bool
	renameInput     string
	renamingNode    *note // the note/folder being renamed
	// Folder creation popup state
	showFolderPopup bool
	folderInput     string
}

func (m *model) filterTags() {
	if m.tagPickerFilter == "" {
		m.tagPickerFiltered = m.allTags
	} else {
		m.tagPickerFiltered = []string{}
		filterLower := strings.ToLower(m.tagPickerFilter)
		for _, tag := range m.allTags {
			if strings.Contains(strings.ToLower(tag), filterLower) {
				m.tagPickerFiltered = append(m.tagPickerFiltered, tag)
			}
		}
	}
	// Reset cursor if out of bounds
	if m.tagPickerCursor >= len(m.tagPickerFiltered) {
		m.tagPickerCursor = 0
	}
}

func (m *model) checkName(name string) {
	sanitized := sanitizeTitle(name)
	if sanitized == "" {
		m.isNameTaken = false
		return
	}

	var path string
	if m.mode == creatingFolderView {
		path = filepath.Join(m.currentNode.path, sanitized)
	} else {
		path = filepath.Join(m.currentNode.path, sanitized+".txt")
	}
	_, err := os.Stat(path)
	m.isNameTaken = !os.IsNotExist(err)
}

func (m *model) checkNameForRename(name string) {
	sanitized := sanitizeTitle(name)
	if sanitized == "" {
		m.isNameTaken = false
		return
	}

	// Get the new path based on whether it's a directory or file
	var newPath string
	parentPath := filepath.Dir(m.renamingNode.path)
	if m.renamingNode.isDir {
		newPath = filepath.Join(parentPath, sanitized)
	} else {
		newPath = filepath.Join(parentPath, sanitized+".txt")
	}

	// Check if the new path already exists AND it's not the same as the current path
	if newPath != m.renamingNode.path {
		_, err := os.Stat(newPath)
		m.isNameTaken = !os.IsNotExist(err)
	} else {
		m.isNameTaken = false // Same name, not taken
	}
}

func (m *model) checkNameForFolder(name string) {
	sanitized := sanitizeTitle(name)
	if sanitized == "" {
		m.isNameTaken = false
		return
	}

	path := filepath.Join(m.currentNode.path, sanitized)
	_, err := os.Stat(path)
	m.isNameTaken = !os.IsNotExist(err)
}

func sanitizeTitle(title string) string {
	title = nonAlphanum.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, " ", "-")
	if title == "" {
		return "Untitled"
	}
	return title
}

func newNote(parent *note, path, title, content string, isDir, favorite bool, modTime os.FileInfo, tags []string) *note {
	return &note{
		parent:   parent,
		path:     path,
		title:    title,
		content:  content,
		isDir:    isDir,
		favorite: favorite,
		modTime:  modTime,
		tags:     tags,
	}
}

func collectAllTags(n *note, tags map[string]bool) {
	if !n.isDir {
		for _, tag := range n.tags {
			tags[tag] = true
		}
	}
	for _, child := range n.children {
		collectAllTags(child, tags)
	}
}

func getAllTags(root *note) []string {
	tagMap := make(map[string]bool)
	collectAllTags(root, tagMap)
	tags := make([]string, 0, len(tagMap))
	for tag := range tagMap {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func findNotesByTag(n *note, tag string, results *[]*note) {
	if !n.isDir {
		for _, t := range n.tags {
			if t == tag {
				*results = append(*results, n)
				break
			}
		}
	}
	for _, child := range n.children {
		findNotesByTag(child, tag, results)
	}
}

func loadNotes(rootPath string) *note {
	root := &note{title: "All Notes", path: rootPath, isDir: true}
	nodes := map[string]*note{rootPath: root}

	filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == rootPath {
			return nil
		}
		// Skip .trash directory
		if d.Name() == ".trash" && d.IsDir() {
			return filepath.SkipDir
		}
		parentPath := filepath.Dir(path)
		parent, exists := nodes[parentPath]
		if !exists {
			return nil
		}
		info, _ := d.Info()
		title := d.Name()
		if !d.IsDir() {
			title = strings.TrimSuffix(title, filepath.Ext(title))
		}
		title = strings.ReplaceAll(title, "-", " ")
		var content string
		var favorite bool
		var tags []string
		if !d.IsDir() {
			fileContent, err := os.ReadFile(path)
			if err == nil {
				content = string(fileContent)
				if strings.HasPrefix(content, "favorite: true\n") {
					favorite = true
					content = strings.TrimPrefix(content, "favorite: true\n")
				}
				matches := tagRegex.FindAllStringSubmatch(content, -1)
				for _, match := range matches {
					tags = append(tags, match[2])
				}
			}
		}
		n := newNote(parent, path, title, content, d.IsDir(), favorite, info, tags)
		parent.children = append(parent.children, n)
		if d.IsDir() {
			nodes[path] = n
		}
		return nil
	})
	return root
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.editor.SetWidth(m.width)
		// Calculate editor height: total height - title (1 line) - status bar (dynamic)
		statusHeight := m.getStatusBarHeight()
		m.editor.SetHeight(m.height - 1 - statusHeight)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || (m.mode == navigationView && msg.String() == "q") {
			m.quitting = true
			return m, tea.Quit
		}
		switch m.mode {
		case navigationView:
			return m.updateNavigationView(msg)
		case editingView:
			return m.updateEditingView(msg)
		case creatingFolderView:
			return m.updateCreatingFolderView(msg)
		case trashView:
			return m.updateTrashView(msg)
		case tagBrowserView:
			return m.updateTagBrowserView(msg)
		case configView:
			return m.updateConfigView(msg)
		case helpView:
			return m.updateHelpView(msg)
		}
	}

	// Only update editor when in editing or creating modes
	if m.mode == editingView || m.mode == creatingFolderView {
		cmd = m.editor.Update(msg)

		// After every editor update, check the name if we're in a creation mode
		if m.mode == creatingFolderView {
			m.checkName(m.editor.Value())
		} else if m.mode == editingView && m.cursor == -1 { // Only for new notes
			lines := strings.SplitN(m.editor.Value(), "\n", 2)
			m.checkName(lines[0])
		}

		return m, cmd
	}

	return m, nil
}

func (m *model) sortNotes() {
	switch m.sort {
	case sortByName:
		sort.Slice(m.currentNode.children, func(i, j int) bool {
			return m.currentNode.children[i].title < m.currentNode.children[j].title
		})
	case sortByDate:
		sort.Slice(m.currentNode.children, func(i, j int) bool {
			return m.currentNode.children[i].modTime.ModTime().After(m.currentNode.children[j].modTime.ModTime())
		})
	}
}

func (m *model) updateNavigationView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle rename popup if it's showing
	if m.showRenamePopup {
		switch msg.String() {
		case "enter":
			if m.isNameTaken {
				return m, nil // Don't save if name is taken
			}
			newName := m.renameInput
			sanitizedName := sanitizeTitle(newName)
			if sanitizedName != "" && m.renamingNode != nil {
				oldPath := m.renamingNode.path
				parentPath := filepath.Dir(oldPath)

				// Construct new path
				var newPath string
				if m.renamingNode.isDir {
					newPath = filepath.Join(parentPath, sanitizedName)
				} else {
					newPath = filepath.Join(parentPath, sanitizedName+".txt")
				}

				// Only rename if the path has actually changed
				if oldPath != newPath {
					if err := os.Rename(oldPath, newPath); err != nil {
						log.Printf("Error renaming: %v", err)
					} else {
						// Update the note structure
						m.renamingNode.title = newName
						m.renamingNode.path = newPath

						// Update cursor position tracking if it's a file
						if !m.renamingNode.isDir {
							if pos, exists := m.cursorPositions[oldPath]; exists {
								delete(m.cursorPositions, oldPath)
								m.cursorPositions[newPath] = pos
								saveCursorPositions(m.cursorPositions)
							}
						}
					}
				} else {
					// Just update the title if only display name changed
					m.renamingNode.title = newName
				}
			}
			// Close popup
			m.showRenamePopup = false
			m.renameInput = ""
			m.renamingNode = nil
			m.isNameTaken = false
			return m, nil
		case "esc":
			// Cancel rename
			m.showRenamePopup = false
			m.renameInput = ""
			m.renamingNode = nil
			m.isNameTaken = false
			return m, nil
		case "backspace":
			if len(m.renameInput) > 0 {
				m.renameInput = m.renameInput[:len(m.renameInput)-1]
				m.checkNameForRename(m.renameInput)
			}
			return m, nil
		default:
			// Add character to rename input
			if len(msg.String()) == 1 {
				m.renameInput += msg.String()
				m.checkNameForRename(m.renameInput)
			}
			return m, nil
		}
	}

	// Handle folder creation popup if it's showing
	if m.showFolderPopup {
		switch msg.String() {
		case "enter":
			if m.isNameTaken {
				return m, nil // Don't create if name is taken
			}
			folderName := m.folderInput
			sanitizedName := sanitizeTitle(folderName)
			if sanitizedName != "" {
				newPath := filepath.Join(m.currentNode.path, sanitizedName)
				if err := os.MkdirAll(newPath, 0755); err != nil {
					log.Printf("Error creating directory: %v", err)
				} else {
					n := newNote(m.currentNode, newPath, folderName, "", true, false, nil, nil)
					m.currentNode.children = append(m.currentNode.children, n)
				}
			}
			// Close popup
			m.showFolderPopup = false
			m.folderInput = ""
			m.isNameTaken = false
			return m, nil
		case "esc":
			// Cancel folder creation
			m.showFolderPopup = false
			m.folderInput = ""
			m.isNameTaken = false
			return m, nil
		case "backspace":
			if len(m.folderInput) > 0 {
				m.folderInput = m.folderInput[:len(m.folderInput)-1]
				m.checkNameForFolder(m.folderInput)
			}
			return m, nil
		default:
			// Add character to folder input
			if len(msg.String()) == 1 {
				m.folderInput += msg.String()
				m.checkNameForFolder(m.folderInput)
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "up", "k":
		if len(m.currentNode.children) > 0 {
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.currentNode.children) - 1
			}
		}
	case "down", "j":
		if len(m.currentNode.children) > 0 {
			if m.cursor < len(m.currentNode.children)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		}
	case "right", "enter":
		if len(m.currentNode.children) > 0 {
			selectedNote := m.currentNode.children[m.cursor]
			if selectedNote.isDir {
				m.currentNode = selectedNote
				m.cursor = 0
				m.sortNotes()
			} else {
				m.mode = editingView
				m.currentNotePath = selectedNote.path
				m.editor.SetValue(selectedNote.content)

				// Restore cursor position if we have one saved
				if savedPos, exists := m.cursorPositions[selectedNote.path]; exists {
					// Clamp to content length to avoid out of bounds
					maxPos := len(selectedNote.content)
					if savedPos > maxPos {
						savedPos = maxPos
					}
					m.editor.SetCursor(savedPos)
				}

				m.editor.Focus()
				return m, nil
			}
		}
	case "left", "esc":
		if m.currentNode.parent != nil {
			// Remember which folder we're coming from
			previousNode := m.currentNode
			m.currentNode = m.currentNode.parent
			m.sortNotes()
			// Find the index of the folder we just left and position cursor there
			for i, child := range m.currentNode.children {
				if child == previousNode {
					m.cursor = i
					break
				}
			}
		}
	case "n":
		m.mode = editingView
		m.currentNotePath = "" // New note doesn't have a path yet
		m.editor.SetValue("")
		m.editor.SetPlaceholder("New Note: first line is the title. ESC to save.")
		m.editor.Focus()
		m.isNameTaken = false
		m.cursor = -1
		return m, nil
	case "F":
		m.showFolderPopup = true
		m.folderInput = ""
		m.isNameTaken = false
		return m, nil
	case "ctrl+t":
		m.previousMode = m.mode
		m.mode = trashView
		m.currentNode = m.trashNode
		m.cursor = 0
		return m, nil
	case "g":
		m.previousMode = m.mode
		m.mode = tagBrowserView
		rootNote := m.currentNode
		for rootNote.parent != nil {
			rootNote = rootNote.parent
		}
		m.allTags = getAllTags(rootNote)
		m.cursor = 0
		return m, nil
	case "c":
		m.previousMode = m.mode
		m.mode = configView
		m.configCursor = 0
		m.tempConfig = config.Colors
		return m, nil
	case "?":
		m.previousMode = m.mode
		m.mode = helpView
		return m, nil
	case "t":
		m.sort = (m.sort + 1) % 2
		m.sortNotes()
		return m, nil
	case "f":
		if len(m.currentNode.children) > 0 {
			selectedNote := m.currentNode.children[m.cursor]
			if !selectedNote.isDir {
				selectedNote.favorite = !selectedNote.favorite
				var content string
				if selectedNote.favorite {
					content = "favorite: true\n" + selectedNote.content
				} else {
					content = selectedNote.content
				}
				if err := os.WriteFile(selectedNote.path, []byte(content), 0644); err != nil {
					log.Printf("Could not update note: %v", err)
				}
			}
		}
		return m, nil
	case "r":
		if len(m.currentNode.children) > 0 {
			selectedNote := m.currentNode.children[m.cursor]
			m.renamingNode = selectedNote
			m.showRenamePopup = true
			m.renameInput = selectedNote.title
			m.isNameTaken = false
			return m, nil
		}
		return m, nil
	case "d":
		if len(m.currentNode.children) > 0 {
			selectedNote := m.currentNode.children[m.cursor]
			trashPath := filepath.Join(notesPath, ".trash")
			newPath := filepath.Join(trashPath, selectedNote.title)
			if err := os.Rename(selectedNote.path, newPath); err != nil {
				log.Printf("Could not move to trash: %v", err)
			}
			m.currentNode.children = append(m.currentNode.children[:m.cursor], m.currentNode.children[m.cursor+1:]...)
			if m.cursor > 0 {
				m.cursor--
			}
		}
		return m, nil
	case "ctrl+e":
		if len(m.currentNode.children) > 0 {
			selectedNote := m.currentNode.children[m.cursor]
			if !selectedNote.isDir {
				return m, openInExternalEditor(selectedNote.path)
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *model) updateTrashView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if len(m.currentNode.children) > 0 {
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.currentNode.children) - 1
			}
		}
	case "down", "j":
		if len(m.currentNode.children) > 0 {
			if m.cursor < len(m.currentNode.children)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		}
	case "esc":
		m.mode = m.previousMode
		m.currentNode = loadNotes(notesPath)
		m.cursor = 0
		return m, nil
	case "r":
		if len(m.currentNode.children) > 0 {
			selectedNote := m.currentNode.children[m.cursor]
			newPath := filepath.Join(notesPath, selectedNote.title)
			if err := os.Rename(selectedNote.path, newPath); err != nil {
				log.Printf("Could not restore note: %v", err)
			}
			m.trashNode = loadNotes(filepath.Join(notesPath, ".trash"))
			m.currentNode = m.trashNode
			if m.cursor > 0 {
				m.cursor--
			}
		}
		return m, nil
	case "d":
		if len(m.currentNode.children) > 0 {
			selectedNote := m.currentNode.children[m.cursor]
			if err := os.RemoveAll(selectedNote.path); err != nil {
				log.Printf("Could not delete note: %v", err)
			}
			m.currentNode.children = append(m.currentNode.children[:m.cursor], m.currentNode.children[m.cursor+1:]...)
			if m.cursor > 0 {
				m.cursor--
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *model) updateTagBrowserView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if len(m.filteredNotes) > 0 {
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.filteredNotes) - 1
			}
		} else if len(m.allTags) > 0 {
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.allTags) - 1
			}
		}
	case "down", "j":
		if len(m.filteredNotes) > 0 {
			if m.cursor < len(m.filteredNotes)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		} else if len(m.allTags) > 0 {
			if m.cursor < len(m.allTags)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		}
	case "esc":
		if len(m.filteredNotes) > 0 {
			// Go back to tag list
			m.filteredNotes = nil
			m.selectedTag = ""
			m.cursor = 0
		} else {
			// Go back to previous mode
			m.mode = m.previousMode
			m.cursor = 0
		}
		return m, nil
	case "enter":
		if len(m.filteredNotes) > 0 {
			// Open the selected note
			selectedNote := m.filteredNotes[m.cursor]
			m.mode = editingView
			m.currentNotePath = selectedNote.path
			m.editor.SetValue(selectedNote.content)

			// Restore cursor position if we have one saved
			if savedPos, exists := m.cursorPositions[selectedNote.path]; exists {
				maxPos := len(selectedNote.content)
				if savedPos > maxPos {
					savedPos = maxPos
				}
				m.editor.SetCursor(savedPos)
			}

			m.editor.Focus()
			// Store the note for editing
			m.currentNode = selectedNote.parent
			for i, n := range m.currentNode.children {
				if n == selectedNote {
					m.cursor = i
					break
				}
			}
			return m, nil
		} else if len(m.allTags) > 0 {
			// Filter notes by selected tag
			m.selectedTag = m.allTags[m.cursor]
			m.filteredNotes = make([]*note, 0)
			rootNote := m.currentNode
			for rootNote.parent != nil {
				rootNote = rootNote.parent
			}
			findNotesByTag(rootNote, m.selectedTag, &m.filteredNotes)
			m.cursor = 0
		}
		return m, nil
	}
	return m, nil
}

func (m *model) updateHelpView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?":
		m.mode = m.previousMode
		return m, nil
	}
	return m, nil
}

func (m *model) updateConfigView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	const numConfigElements = 13 // 1 path + 1 editor + 11 colors

	// If editing path, handle differently
	if m.editingPath {
		switch msg.String() {
		case "enter", "esc":
			if msg.String() == "enter" && m.pathInput != "" {
				config.NotesPath = m.pathInput
				saveConfig(config)
			}
			m.editingPath = false
			m.pathInput = ""
			return m, nil
		case "backspace":
			if len(m.pathInput) > 0 {
				m.pathInput = m.pathInput[:len(m.pathInput)-1]
			}
			return m, nil
		default:
			// Add character to path input
			if len(msg.String()) == 1 {
				m.pathInput += msg.String()
			}
			return m, nil
		}
	}

	// If editing editor, handle differently
	if m.editingEditor {
		switch msg.String() {
		case "enter", "esc":
			if msg.String() == "enter" && m.editorInput != "" {
				config.ExternalEditor = m.editorInput
				saveConfig(config)
			}
			m.editingEditor = false
			m.editorInput = ""
			return m, nil
		case "backspace":
			if len(m.editorInput) > 0 {
				m.editorInput = m.editorInput[:len(m.editorInput)-1]
			}
			return m, nil
		default:
			// Add character to editor input
			if len(msg.String()) == 1 {
				m.editorInput += msg.String()
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "up", "k":
		if m.configCursor > 0 {
			m.configCursor--
		} else {
			m.configCursor = numConfigElements - 1
		}
	case "down", "j":
		if m.configCursor < numConfigElements-1 {
			m.configCursor++
		} else {
			m.configCursor = 0
		}
	case "enter":
		// If on save path item (cursor == 0), start editing
		if m.configCursor == 0 {
			m.editingPath = true
			m.pathInput = config.NotesPath
			return m, nil
		}
		// If on external editor item (cursor == 1), start editing
		if m.configCursor == 1 {
			m.editingEditor = true
			m.editorInput = config.ExternalEditor
			return m, nil
		}
	case "left", "h":
		// Decrease color index (skip if on path or editor)
		if m.configCursor > 1 {
			switch m.configCursor {
			case 2:
				m.tempConfig.TitleBg = (m.tempConfig.TitleBg - 1 + 256) % 256
			case 3:
				m.tempConfig.TitleFg = (m.tempConfig.TitleFg - 1 + 256) % 256
			case 4:
				m.tempConfig.StatusBg = (m.tempConfig.StatusBg - 1 + 256) % 256
			case 5:
				m.tempConfig.StatusFg = (m.tempConfig.StatusFg - 1 + 256) % 256
			case 6:
				m.tempConfig.BorderColor = (m.tempConfig.BorderColor - 1 + 256) % 256
			case 7:
				m.tempConfig.SelectedFg = (m.tempConfig.SelectedFg - 1 + 256) % 256
			case 8:
				m.tempConfig.FavoriteColor = (m.tempConfig.FavoriteColor - 1 + 256) % 256
			case 9:
				m.tempConfig.TagBarBg = (m.tempConfig.TagBarBg - 1 + 256) % 256
			case 10:
				m.tempConfig.TagBarFg = (m.tempConfig.TagBarFg - 1 + 256) % 256
			case 11:
				m.tempConfig.TagSelectedBg = (m.tempConfig.TagSelectedBg - 1 + 256) % 256
			case 12:
				m.tempConfig.TagSelectedFg = (m.tempConfig.TagSelectedFg - 1 + 256) % 256
			}
			// Apply temp config for live preview
			config.Colors = m.tempConfig
			applyColorConfig()
		}
	case "right", "l":
		// Increase color index (skip if on path or editor)
		if m.configCursor > 1 {
			switch m.configCursor {
			case 2:
				m.tempConfig.TitleBg = (m.tempConfig.TitleBg + 1) % 256
			case 3:
				m.tempConfig.TitleFg = (m.tempConfig.TitleFg + 1) % 256
			case 4:
				m.tempConfig.StatusBg = (m.tempConfig.StatusBg + 1) % 256
			case 5:
				m.tempConfig.StatusFg = (m.tempConfig.StatusFg + 1) % 256
			case 6:
				m.tempConfig.BorderColor = (m.tempConfig.BorderColor + 1) % 256
			case 7:
				m.tempConfig.SelectedFg = (m.tempConfig.SelectedFg + 1) % 256
			case 8:
				m.tempConfig.FavoriteColor = (m.tempConfig.FavoriteColor + 1) % 256
			case 9:
				m.tempConfig.TagBarBg = (m.tempConfig.TagBarBg + 1) % 256
			case 10:
				m.tempConfig.TagBarFg = (m.tempConfig.TagBarFg + 1) % 256
			case 11:
				m.tempConfig.TagSelectedBg = (m.tempConfig.TagSelectedBg + 1) % 256
			case 12:
				m.tempConfig.TagSelectedFg = (m.tempConfig.TagSelectedFg + 1) % 256
			}
			// Apply temp config for live preview
			config.Colors = m.tempConfig
			applyColorConfig()
		}
	case "esc":
		// Save config and exit
		config.Colors = m.tempConfig
		saveConfig(config)
		applyColorConfig()
		m.mode = m.previousMode
		return m, nil
	}
	return m, nil
}

func (m *model) updateEditingView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Handle tag picker if it's showing
	if m.showTagPicker {
		switch msg.String() {
		case "up", "ctrl+p":
			if m.tagPickerCursor > 0 {
				m.tagPickerCursor--
			} else if len(m.tagPickerFiltered) > 0 {
				m.tagPickerCursor = len(m.tagPickerFiltered) - 1
			}
			return m, nil
		case "down", "ctrl+n":
			if len(m.tagPickerFiltered) > 0 {
				if m.tagPickerCursor < len(m.tagPickerFiltered)-1 {
					m.tagPickerCursor++
				} else {
					m.tagPickerCursor = 0
				}
			}
			return m, nil
		case "enter":
			// Insert selected tag
			if len(m.tagPickerFiltered) > 0 {
				selectedTag := m.tagPickerFiltered[m.tagPickerCursor]
				// Remove the filter text and insert the selected tag
				currentText := m.editor.Value()
				// Find the last # and replace the filter text with the selected tag
				lastHash := strings.LastIndex(currentText, "#")
				if lastHash >= 0 {
					// Calculate where the filter text ends
					filterEndPos := lastHash + 1 + len(m.tagPickerFilter)
					// Preserve text before # and after filter
					beforeHash := currentText[:lastHash+1]
					afterFilter := ""
					if filterEndPos < len(currentText) {
						afterFilter = currentText[filterEndPos:]
					}
					newText := beforeHash + selectedTag + afterFilter
					m.editor.SetValue(newText)
					// Position cursor right after the inserted tag
					cursorPos := lastHash + 1 + len(selectedTag)
					m.editor.SetCursor(cursorPos)
					m.editor.MarkDirty()
				}
			}
			m.showTagPicker = false
			m.tagPickerFilter = ""
			m.tagPickerFiltered = nil
			m.tagPickerCursor = 0
			return m, nil
		case "esc":
			m.showTagPicker = false
			m.tagPickerFilter = ""
			m.tagPickerFiltered = nil
			m.tagPickerCursor = 0
			return m, nil
		case "backspace":
			// Remove last character from filter
			if len(m.tagPickerFilter) > 0 {
				m.tagPickerFilter = m.tagPickerFilter[:len(m.tagPickerFilter)-1]
				m.filterTags()
			} else {
				// If filter is empty, close picker
				m.showTagPicker = false
				m.tagPickerFiltered = nil
			}
			cmd = m.editor.Update(msg)
			return m, cmd
		default:
			// Check if it's a valid tag character
			key := msg.String()
			if len(key) == 1 {
				char := key[0]
				if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
					(char >= '0' && char <= '9') || char == '-' || char == '_' {
					// Add to filter
					m.tagPickerFilter += key
					m.filterTags()
					cmd = m.editor.Update(msg)
					return m, cmd
				} else {
					// Invalid character for tag - close picker and let it through
					m.showTagPicker = false
					m.tagPickerFilter = ""
					m.tagPickerFiltered = nil
					m.tagPickerCursor = 0
					cmd = m.editor.Update(msg)
					return m, cmd
				}
			}
			cmd = m.editor.Update(msg)
			return m, cmd
		}
	}

	// Check if # was just typed to trigger tag picker
	if msg.String() == "#" {
		// Get all tags from the root note
		rootNote := m.currentNode
		for rootNote.parent != nil {
			rootNote = rootNote.parent
		}
		m.allTags = getAllTags(rootNote)
		m.showTagPicker = true
		m.tagPickerFilter = ""
		m.tagPickerFiltered = m.allTags
		m.tagPickerCursor = 0
		cmd = m.editor.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+e":
		// Save current content first, then open in external editor
		var noteToUpdate *note
		content := m.editor.Value()

		if m.cursor == -1 { // New note - save it first
			if content != "" {
				lines := strings.SplitN(content, "\n", 2)
				title := strings.TrimSpace(lines[0])
				noteContent := ""
				if len(lines) > 1 {
					noteContent = lines[1]
				}
				sanitizedTitle := sanitizeTitle(title)
				path := filepath.Join(m.currentNode.path, sanitizedTitle+".txt")
				matches := tagRegex.FindAllStringSubmatch(noteContent, -1)
				var tags []string
				for _, match := range matches {
					tags = append(tags, match[2])
				}
				noteToUpdate = newNote(m.currentNode, path, title, noteContent, false, false, nil, tags)
				m.currentNode.children = append(m.currentNode.children, noteToUpdate)
				var contentToSave string
				if noteToUpdate.favorite {
					contentToSave = "favorite: true\n" + noteToUpdate.content
				} else {
					contentToSave = noteToUpdate.content
				}
				os.WriteFile(noteToUpdate.path, []byte(contentToSave), 0644)
				m.editor.ClearDirty()
				return m, openInExternalEditor(noteToUpdate.path)
			}
		} else { // Existing note
			noteToUpdate = m.currentNode.children[m.cursor]
			noteToUpdate.content = content
			var contentToSave string
			if noteToUpdate.favorite {
				contentToSave = "favorite: true\n" + noteToUpdate.content
			} else {
				contentToSave = noteToUpdate.content
			}
			os.WriteFile(noteToUpdate.path, []byte(contentToSave), 0644)
			m.editor.ClearDirty()
			return m, openInExternalEditor(noteToUpdate.path)
		}
		return m, nil
	case "ctrl+s":
		if m.cursor == -1 && m.isNameTaken {
			return m, nil // Don't save if name is taken
		}
		content := m.editor.Value()
		var noteToUpdate *note

		if m.cursor == -1 { // New note
			if content == "" {
				return m, nil
			}
			lines := strings.SplitN(content, "\n", 2)
			title := strings.TrimSpace(lines[0])
			noteContent := ""
			if len(lines) > 1 {
				noteContent = lines[1]
			}
			sanitizedTitle := sanitizeTitle(title)
			path := filepath.Join(m.currentNode.path, sanitizedTitle+".txt")
			matches := tagRegex.FindAllStringSubmatch(noteContent, -1)
			var tags []string
			for _, match := range matches {
				tags = append(tags, match[2])
			}
			noteToUpdate = newNote(m.currentNode, path, title, noteContent, false, false, nil, tags)
			m.currentNode.children = append(m.currentNode.children, noteToUpdate)
			// Set cursor to the newly created note
			m.cursor = len(m.currentNode.children) - 1

			var contentToSave string
			if noteToUpdate.favorite {
				contentToSave = "favorite: true\n" + noteToUpdate.content
			} else {
				contentToSave = noteToUpdate.content
			}
			os.WriteFile(noteToUpdate.path, []byte(contentToSave), 0644)

			// Switch editor to the saved content (without the title line)
			prevCursor := m.editor.GetCursor()
			removedLen := len(lines[0])
			if len(lines) > 1 {
				removedLen++
			}
			m.editor.SetValue(noteToUpdate.content)
			newCursor := prevCursor - removedLen
			if newCursor < 0 {
				newCursor = 0
			}
			m.editor.SetCursor(newCursor)

			m.cursorPositions[noteToUpdate.path] = m.editor.GetCursor()
			saveCursorPositions(m.cursorPositions)
			m.editor.ClearDirty()
			return m, nil
		}

		// Existing note
		noteToUpdate = m.currentNode.children[m.cursor]
		noteToUpdate.content = content
		matches := tagRegex.FindAllStringSubmatch(content, -1)
		var tags []string
		for _, match := range matches {
			tags = append(tags, match[2])
		}
		noteToUpdate.tags = tags

		var contentToSave string
		if noteToUpdate.favorite {
			contentToSave = "favorite: true\n" + noteToUpdate.content
		} else {
			contentToSave = noteToUpdate.content
		}
		err := os.WriteFile(noteToUpdate.path, []byte(contentToSave), 0644)
		if err != nil {
			log.Printf("Error saving note: %v", err)
		}

		// Save cursor position
		m.cursorPositions[noteToUpdate.path] = m.editor.GetCursor()
		saveCursorPositions(m.cursorPositions)
		m.editor.ClearDirty()
		return m, nil
	case "esc":
		if m.cursor == -1 && m.isNameTaken {
			return m, nil // Don't save if name is taken
		}
		m.editor.Blur()
		content := m.editor.Value()
		var noteToUpdate *note

		if m.cursor == -1 { // New note
			if content != "" {
				lines := strings.SplitN(content, "\n", 2)
				title := strings.TrimSpace(lines[0])
				noteContent := ""
				if len(lines) > 1 {
					noteContent = lines[1]
				}
				sanitizedTitle := sanitizeTitle(title)
				path := filepath.Join(m.currentNode.path, sanitizedTitle+".txt")
				matches := tagRegex.FindAllStringSubmatch(noteContent, -1)
				var tags []string
				for _, match := range matches {
					tags = append(tags, match[2])
				}
				noteToUpdate = newNote(m.currentNode, path, title, noteContent, false, false, nil, tags)
				m.currentNode.children = append(m.currentNode.children, noteToUpdate)
				// Set cursor to the newly created note
				m.cursor = len(m.currentNode.children) - 1
			} else {
				// Empty new note, just return to cursor 0
				m.cursor = 0
			}
		} else { // Existing note
			noteToUpdate = m.currentNode.children[m.cursor]
			noteToUpdate.content = content
			matches := tagRegex.FindAllStringSubmatch(content, -1)
			var tags []string
			for _, match := range matches {
				tags = append(tags, match[2])
			}
			noteToUpdate.tags = tags
			// Keep cursor on the same note (m.cursor unchanged)
		}

		if noteToUpdate != nil {
			var contentToSave string
			if noteToUpdate.favorite {
				contentToSave = "favorite: true\n" + noteToUpdate.content
			} else {
				contentToSave = noteToUpdate.content
			}
			err := os.WriteFile(noteToUpdate.path, []byte(contentToSave), 0644)
			if err != nil {
				log.Printf("Error saving note: %v", err)
			}

			// Save cursor position
			m.cursorPositions[noteToUpdate.path] = m.editor.GetCursor()
			saveCursorPositions(m.cursorPositions)
		}
		m.editor.ClearDirty()
		m.mode = navigationView
		return m, nil
	}

	// Update editor
	cmd = m.editor.Update(msg)
	return m, cmd
}

func (m *model) updateCreatingFolderView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "enter":
		if m.isNameTaken {
			return m, nil // Don't save if name is taken
		}
		folderName := m.editor.Value()
		sanitizedName := sanitizeTitle(folderName)
		if sanitizedName != "" {
			newPath := filepath.Join(m.currentNode.path, sanitizedName)
			if err := os.MkdirAll(newPath, 0755); err != nil {
				log.Printf("Error creating directory: %v", err)
			} else {
				n := newNote(m.currentNode, newPath, folderName, "", true, false, nil, nil)
				m.currentNode.children = append(m.currentNode.children, n)
			}
		}
		m.mode = navigationView
		m.editor.Blur()
		return m, nil
	case "esc":
		m.mode = navigationView
		m.editor.Blur()
		return m, nil
	}
	cmd = m.editor.Update(msg)
	return m, cmd
}

func (m model) titleView() string {
	var title string
	switch m.mode {
	case trashView:
		title = "Notes v" + getVersion() + " - Trash"
	case configView:
		title = "Notes v" + getVersion() + " - Configuration"
	case tagBrowserView:
		if len(m.filteredNotes) > 0 {
			title = "Notes v" + getVersion() + " - Tag: #" + m.selectedTag
		} else {
			title = "Notes v" + getVersion() + " - Tags"
		}
	case navigationView:
		if m.currentNode.parent == nil {
			title = "Notes v" + getVersion()
		} else {
			title = "Notes v" + getVersion() + " - " + m.currentNode.title
		}
	default:
		title = "Notes v" + getVersion()
	}

	if m.mode == editingView && m.editor.Dirty() {
		title += " [UNSAVED]"
	}

	w := m.width
	if w <= 0 {
		w = 80
	}
	return titleStyle.Width(w).Render(title)
}


func (m model) tagPickerView() string {
	if !m.showTagPicker {
		return ""
	}

	var tags strings.Builder

	// Style for tag picker bar
	tagBarStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TagBarBg))).
		Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TagBarFg))).
		Padding(0, 1)

	// Style for selected tag (reversed/highlighted)
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TagSelectedBg))).
		Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TagSelectedFg))).
		Bold(true).
		Padding(0, 1)

	// Style for unselected tags (must set background to match bar)
	tagStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TagBarBg))).
		Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.TagBarFg))).
		Padding(0, 1)

	// Build the tag line
	prefix := "Tags"
	if m.tagPickerFilter != "" {
		prefix += ": #" + m.tagPickerFilter
	}
	tags.WriteString(prefix + " │ ")

	if len(m.tagPickerFiltered) == 0 {
		tags.WriteString(tagStyle.Render("No matches"))
	} else {
		// Calculate how many tags we can fit
		availableWidth := m.width - len(prefix) - 4 // Account for prefix and separators
		currentWidth := 0
		displayedCount := 0

		for i, tag := range m.tagPickerFiltered {
			tagText := "#" + tag
			tagWidth := len(tagText) + 3 // +3 for padding and separator

			if currentWidth + tagWidth > availableWidth {
				// Show "... N more" if we can't fit all
				remaining := len(m.tagPickerFiltered) - displayedCount
				if remaining > 0 {
					tags.WriteString(tagStyle.Render(fmt.Sprintf("... %d more", remaining)))
				}
				break
			}

			if i == m.tagPickerCursor {
				tags.WriteString(highlightStyle.Render(tagText))
			} else {
				tags.WriteString(tagStyle.Render(tagText))
			}

			if i < len(m.tagPickerFiltered)-1 {
				tags.WriteString(" ")
			}

			currentWidth += tagWidth
			displayedCount++
		}
	}

	w := m.width
	if w <= 0 {
		w = 80
	}
	return tagBarStyle.Width(w).Render(tags.String())
}

func (m model) getStatusBarHeight() int {
	// Calculate how many lines the status bar will use based on width
	w := m.width
	if w <= 0 {
		w = 80
	}

	switch m.mode {
	case navigationView:
		if w > 100 {
			return 2 // Wide: 2 lines
		} else if w > 60 {
			return 3 // Medium: 3 lines
		} else {
			return 4 // Narrow: 4 lines
		}
	case editingView, creatingFolderView, trashView, tagBrowserView, configView, helpView:
		return 1 // Most other views use single line
	default:
		return 2 // Default fallback
	}
}

func (m model) statusView() string {
	var status string
	w := m.width
	if w <= 0 {
		w = 80
	}

	switch m.mode {
	case navigationView:
		// Responsive status bar based on terminal width
		if w > 100 {
			// Wide: 2 lines (current layout)
			line1 := "↑/↓: nav | ←/esc: back | →/enter: open | n: new note | F: new folder | ctrl+e: external editor"
			line2 := "g: tags | c: config | ?: help | f: fav | t: sort | r: rename | d: del | ctrl+t: trash | q: quit"
			status = line1 + "\n" + line2
		} else if w > 60 {
			// Medium: 3 lines with smart grouping
			line1 := "↑/↓: nav | ←/esc: back | →/enter: open"
			line2 := "n: new note | F: folder | r: rename | d: del | f: fav | t: sort"
			line3 := "g: tags | c: config | ctrl+e: editor | ctrl+t: trash | ?: help | q: quit"
			status = line1 + "\n" + line2 + "\n" + line3
		} else {
			// Narrow: 4 lines with abbreviated shortcuts
			line1 := "↑/↓ k/j  ←/esc  →/enter"
			line2 := "n: note  F: folder  r: rename"
			line3 := "f: fav  t: sort  d: del"
			line4 := "g: tags  c: config  ?: help  q: quit"
			status = line1 + "\n" + line2 + "\n" + line3 + "\n" + line4
		}
	case editingView:
		if m.isNameTaken {
			status = "NAME TAKEN! | esc: cancel"
		} else {
			if w > 80 {
				status = "esc: save and close | ctrl+s: save | ctrl+e: external editor | #: tag picker"
			} else {
				status = "esc: save | ctrl+s: save | ctrl+e: editor | #: tags"
			}
		}
	case creatingFolderView:
		if m.isNameTaken {
			status = "NAME TAKEN! | esc: cancel"
		} else {
			status = "enter: create | esc: cancel"
		}
	case trashView:
		if w > 70 {
			status = "↑/↓: nav | r: restore | d: delete permanently | esc: back"
		} else {
			status = "↑/↓ k/j | r: restore | d: delete | esc: back"
		}
	case tagBrowserView:
		if len(m.filteredNotes) > 0 {
			if w > 70 {
				status = "↑/↓: nav | enter: open note | esc: back to tags"
			} else {
				status = "↑/↓ k/j | enter: open | esc: back"
			}
		} else {
			if w > 70 {
				status = "↑/↓: nav | enter: filter by tag | esc: back"
			} else {
				status = "↑/↓ k/j | enter: filter | esc: back"
			}
		}
	case configView:
		if w > 80 {
			status = "↑/↓: select element | ←/→: adjust color index (0-255) | esc: save & exit"
		} else if w > 60 {
			status = "↑/↓: select | ←/→: adjust color (0-255) | esc: save"
		} else {
			status = "↑/↓ ←/→: adjust color | esc: save"
		}
	case helpView:
		status = "esc/q/?: close help"
	}

	return statusStyle.Width(w).Render(status)
}


func (m model) View() string {
	if m.quitting {
		return ""
	}

	// Calculate dynamic heights based on status bar size
	statusHeight := m.getStatusBarHeight()
	contentHeight := m.height - 1 - statusHeight // total - title - status
	borderedHeight := contentHeight - 2          // account for border padding

	var mainContent string
	switch m.mode {
	case editingView, creatingFolderView:
		editorView := m.editor.View()
		mainContent = contentStyle.Width(m.width).Height(contentHeight).Render(editorView)
	case trashView:
		var s strings.Builder
		if len(m.currentNode.children) == 0 {
			s.WriteString("\n  Trash is empty.")
		} else {
			for i, note := range m.currentNode.children {
				line := ""
				if m.cursor == i {
					line = "> "
				} else {
					line = "  "
				}
				name := note.title
				if note.isDir {
					name = lipgloss.NewStyle().Bold(true).Render(name) + "/"
				}
				if m.cursor == i {
					line += selectedStyle.Render(name)
				} else {
					line += name
				}
				s.WriteString(line + "\n")
			}
		}
		bordered := borderStyle.Width(m.width - 4).Height(borderedHeight).Render(s.String())
		mainContent = contentStyle.Width(m.width).Height(contentHeight).Render(bordered)
	case helpView:
		var s strings.Builder
		s.WriteString("Notes v" + getVersion() + " - Help\n\n")
		s.WriteString("NAVIGATION VIEW\n")
		s.WriteString("  ↑/↓, k/j     Navigate up/down (wraps)\n")
		s.WriteString("  ←, esc       Go back to parent folder\n")
		s.WriteString("  →, enter     Open note/folder\n")
		s.WriteString("  n            Create new note\n")
		s.WriteString("  F            Create new folder\n")
		s.WriteString("  f            Toggle favorite\n")
		s.WriteString("  t            Toggle sort (name/date)\n")
		s.WriteString("  r            Rename note/folder\n")
		s.WriteString("  d            Move to trash\n")
		s.WriteString("  g            Open tag browser\n")
		s.WriteString("  c            Open configuration\n")
		s.WriteString("  ctrl+t       View trash\n")
		s.WriteString("  ctrl+e       Open in external editor\n")
		s.WriteString("  ?            Show this help\n")
		s.WriteString("  q            Quit\n\n")

		s.WriteString("EDITING VIEW\n")
		s.WriteString("  esc          Save and close\n")
		s.WriteString("  #            Trigger tag picker\n")
		s.WriteString("  ctrl+e       Open in external editor\n\n")

		s.WriteString("TAG BROWSER\n")
		s.WriteString("  ↑/↓, k/j     Navigate tags/notes\n")
		s.WriteString("  enter        Filter by tag / Open note\n")
		s.WriteString("  esc          Back to tags / Exit\n\n")

		s.WriteString("TRASH VIEW\n")
		s.WriteString("  ↑/↓, k/j     Navigate items\n")
		s.WriteString("  r            Restore item\n")
		s.WriteString("  d            Delete permanently\n")
		s.WriteString("  esc          Back to notes\n\n")

		s.WriteString("CONFIGURATION\n")
		s.WriteString("  ↑/↓, k/j     Select element\n")
		s.WriteString("  ←/→, h/l     Adjust color index\n")
		s.WriteString("  esc          Save and exit\n\n")

		s.WriteString("GENERAL\n")
		s.WriteString("  ctrl+c       Quit from anywhere\n")

		bordered := borderStyle.Width(m.width - 4).Height(borderedHeight).Render(s.String())
		mainContent = contentStyle.Width(m.width).Height(contentHeight).Render(bordered)
	case configView:
		var s strings.Builder
		s.WriteString("Configuration\n\n")

		// Save Path
		pathCursor := "  "
		if m.configCursor == 0 {
			pathCursor = "> "
		}
		pathValue := config.NotesPath
		if m.editingPath {
			pathValue = m.pathInput + "█" // Show cursor
		}
		pathLine := fmt.Sprintf("%s%-20s %s", pathCursor, "Notes Path:", pathValue)
		if m.configCursor == 0 {
			pathLine = selectedStyle.Render(pathLine)
		}
		s.WriteString(pathLine + "\n")
		if m.editingPath {
			s.WriteString("  (Type path, Enter to save, Esc to cancel)\n")
		} else if m.configCursor == 0 {
			s.WriteString("  (Press Enter to edit)\n")
		}
		s.WriteString("\n")

		// External Editor
		editorCursor := "  "
		if m.configCursor == 1 {
			editorCursor = "> "
		}
		editorValue := config.ExternalEditor
		if m.editingEditor {
			editorValue = m.editorInput + "█" // Show cursor
		}
		editorLine := fmt.Sprintf("%s%-20s %s", editorCursor, "External Editor:", editorValue)
		if m.configCursor == 1 {
			editorLine = selectedStyle.Render(editorLine)
		}
		s.WriteString(editorLine + "\n")
		if m.editingEditor {
			s.WriteString("  (Type editor command, Enter to save, Esc to cancel)\n")
		} else if m.configCursor == 1 {
			s.WriteString("  (Press Enter to edit)\n")
		}
		s.WriteString("\n")

		// Color Elements
		colorElements := []struct {
			name  string
			value int
		}{
			{"Title Background", m.tempConfig.TitleBg},
			{"Title Foreground", m.tempConfig.TitleFg},
			{"Status Background", m.tempConfig.StatusBg},
			{"Status Foreground", m.tempConfig.StatusFg},
			{"Border Color", m.tempConfig.BorderColor},
			{"Selected Item", m.tempConfig.SelectedFg},
			{"Favorite Marker", m.tempConfig.FavoriteColor},
			{"Tag Bar Background", m.tempConfig.TagBarBg},
			{"Tag Bar Foreground", m.tempConfig.TagBarFg},
			{"Tag Selected Bg", m.tempConfig.TagSelectedBg},
			{"Tag Selected Fg", m.tempConfig.TagSelectedFg},
		}

		for i, elem := range colorElements {
			cursor := "  "
			if m.configCursor == i+2 { // +2 because path is at 0, editor is at 1
				cursor = "> "
			}
			line := fmt.Sprintf("%s%-20s %3d", cursor, elem.name+":", elem.value)
			if m.configCursor == i+2 {
				line = selectedStyle.Render(line)
			}
			s.WriteString(line + "\n")
		}

		s.WriteString("\n--- Live Preview ---\n\n")

		// Preview title bar
		previewTitle := titleStyle.Render(" Notes v" + getVersion() + " - Preview ")
		s.WriteString(previewTitle + "\n\n")

		// Preview navigation with border
		previewNav := "  Sample Folder/\n"
		previewNav += selectedStyle.Render("> Selected Note") + "\n"
		previewNav += "  " + favoriteStyle.Render("★") + " Favorite Note\n"
		previewNav += "  Regular Note\n"
		previewBordered := borderStyle.Width(40).Render(previewNav)
		s.WriteString(previewBordered + "\n\n")

		// Preview tag bar
		tagBarPreviewStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(fmt.Sprintf("%d", m.tempConfig.TagBarBg))).
			Foreground(lipgloss.Color(fmt.Sprintf("%d", m.tempConfig.TagBarFg))).
			Padding(0, 1)

		tagSelectedPreviewStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(fmt.Sprintf("%d", m.tempConfig.TagSelectedBg))).
			Foreground(lipgloss.Color(fmt.Sprintf("%d", m.tempConfig.TagSelectedFg))).
			Bold(true).
			Padding(0, 1)

		tagUnselectedPreviewStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(fmt.Sprintf("%d", m.tempConfig.TagBarBg))).
			Foreground(lipgloss.Color(fmt.Sprintf("%d", m.tempConfig.TagBarFg))).
			Padding(0, 1)

		previewTagBar := "Tags: #filter │ " +
			tagUnselectedPreviewStyle.Render("#example") + " " +
			tagSelectedPreviewStyle.Render("#selected") + " " +
			tagUnselectedPreviewStyle.Render("#another")

		s.WriteString(tagBarPreviewStyle.Width(40).Render(previewTagBar) + "\n\n")

		// Preview status bar
		previewStatus := statusStyle.Render(" Status bar example ")
		s.WriteString(previewStatus + "\n")

		bordered := borderStyle.Width(m.width - 4).Height(borderedHeight).Render(s.String())
		mainContent = contentStyle.Width(m.width).Height(contentHeight).Render(bordered)
	case tagBrowserView:
		var s strings.Builder
		if len(m.filteredNotes) > 0 {
			// Showing filtered notes by tag
			s.WriteString("Notes with tag: #" + m.selectedTag + "\n\n")
			for i, note := range m.filteredNotes {
				line := ""
				if m.cursor == i {
					line = "> " + selectedStyle.Render(note.title)
				} else {
					line = "  " + note.title
				}
				s.WriteString(line + "\n")
			}
		} else if len(m.allTags) == 0 {
			s.WriteString("\n  No tags found. Add tags to your notes using #tagname.")
		} else {
			s.WriteString("All Tags:\n\n")
			for i, tag := range m.allTags {
				line := ""
				if m.cursor == i {
					line = "> " + selectedStyle.Render("#"+tag)
				} else {
					line = "  #" + tag
				}
				s.WriteString(line + "\n")
			}
		}
		bordered := borderStyle.Width(m.width - 4).Height(borderedHeight).Render(s.String())
		mainContent = contentStyle.Width(m.width).Height(contentHeight).Render(bordered)
	default: // navigationView
		var s strings.Builder

		// Add current folder title
		folderTitle := m.currentNode.title
		if m.currentNode.parent == nil {
			folderTitle = "All Notes"
		}
		// Make title bold and prominent
		s.WriteString("\n" + lipgloss.NewStyle().Bold(true).Render(folderTitle) + "\n")
		s.WriteString(strings.Repeat("─", len(folderTitle)) + "\n\n")

		if len(m.currentNode.children) == 0 {
			s.WriteString("  No notes yet. Press 'n' to create one or 'F' for a new folder.")
		} else {
			for i, note := range m.currentNode.children {
				line := ""
				if m.cursor == i {
					line = "> "
				} else {
					line = "  "
				}

				name := note.title
				if note.isDir {
					name = lipgloss.NewStyle().Bold(true).Render(name) + "/"
				}

				// Apply favorite marker
				if note.favorite {
					name = favoriteStyle.Render("★") + " " + name
				}

				// Apply selection style
				if m.cursor == i {
					line += selectedStyle.Render(name)
				} else {
					line += name
				}

				s.WriteString(line + "\n")
			}
		}
		// No border, just render content like editing view
		mainContent = contentStyle.Width(m.width).Height(contentHeight).Render(s.String())
	}

	// Build the view components
	components := []string{
		m.titleView(),
		mainContent,
	}

	// Add tag picker bar if active (appears above status bar)
	tagPicker := m.tagPickerView()
	if tagPicker != "" {
		components = append(components, tagPicker)
	}

	// Add status bar last
	components = append(components, m.statusView())

	baseView := lipgloss.JoinVertical(lipgloss.Left, components...)

	// Overlay rename popup if active
	if m.showRenamePopup {
		// Create popup box
		popupStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.BorderColor))).
			Padding(1, 2).
			Background(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusBg))).
			Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusFg)))

		var content strings.Builder
		itemType := "note"
		if m.renamingNode != nil && m.renamingNode.isDir {
			itemType = "folder"
		}

		content.WriteString(lipgloss.NewStyle().Bold(true).Render("Rename "+itemType) + "\n\n")
		inputDisplay := m.renameInput + "█"
		content.WriteString(inputDisplay + "\n\n")

		if m.isNameTaken {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
			content.WriteString(errorStyle.Render("⚠ Name already exists!") + "\n\n")
		}

		helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusFg)))
		content.WriteString(helpStyle.Render("Enter: confirm | Esc: cancel"))

		popup := popupStyle.Render(content.String())

		// Split base view into lines
		baseLines := strings.Split(baseView, "\n")
		popupLines := strings.Split(popup, "\n")

		// Calculate popup position (centered)
		popupHeight := len(popupLines)
		popupWidth := lipgloss.Width(popup)
		startRow := (len(baseLines) - popupHeight) / 2
		if startRow < 0 {
			startRow = 0
		}

		// Overlay popup lines onto base view lines
		for i, popupLine := range popupLines {
			row := startRow + i
			if row >= 0 && row < len(baseLines) {
				baseLine := baseLines[row]
				baseWidth := lipgloss.Width(baseLine)
				startCol := (baseWidth - popupWidth) / 2
				if startCol < 0 {
					startCol = 0
				}

				// Replace the middle portion of the base line with the popup line
				// This is a simplified overlay - just center the popup
				if startCol < baseWidth {
					// Build the overlaid line
					prefix := ""
					suffix := ""
					if startCol > 0 {
						// Extract prefix (before popup)
						prefix = lipgloss.NewStyle().Width(startCol).Render(baseLine[:min(startCol, len(baseLine))])
					}
					endCol := startCol + popupWidth
					if endCol < baseWidth {
						// Extract suffix (after popup)
						suffix = baseLine[min(endCol, len(baseLine)):]
					}
					baseLines[row] = prefix + popupLine + suffix
				} else {
					baseLines[row] = popupLine
				}
			}
		}

		return strings.Join(baseLines, "\n")
	}

	// Overlay folder creation popup if active
	if m.showFolderPopup {
		// Create popup box
		popupStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.BorderColor))).
			Padding(1, 2).
			Background(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusBg))).
			Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusFg)))

		var content strings.Builder

		content.WriteString(lipgloss.NewStyle().Bold(true).Render("New Folder") + "\n\n")
		inputDisplay := m.folderInput + "█"
		content.WriteString(inputDisplay + "\n\n")

		if m.isNameTaken {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
			content.WriteString(errorStyle.Render("⚠ Name already exists!") + "\n\n")
		}

		helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("%d", config.Colors.StatusFg)))
		content.WriteString(helpStyle.Render("Enter: create | Esc: cancel"))

		popup := popupStyle.Render(content.String())

		// Split base view into lines
		baseLines := strings.Split(baseView, "\n")
		popupLines := strings.Split(popup, "\n")

		// Calculate popup position (centered)
		popupHeight := len(popupLines)
		popupWidth := lipgloss.Width(popup)
		startRow := (len(baseLines) - popupHeight) / 2
		if startRow < 0 {
			startRow = 0
		}

		// Overlay popup lines onto base view lines
		for i, popupLine := range popupLines {
			row := startRow + i
			if row >= 0 && row < len(baseLines) {
				baseLine := baseLines[row]
				baseWidth := lipgloss.Width(baseLine)
				startCol := (baseWidth - popupWidth) / 2
				if startCol < 0 {
					startCol = 0
				}

				// Replace the middle portion of the base line with the popup line
				if startCol < baseWidth {
					// Build the overlaid line
					prefix := ""
					suffix := ""
					if startCol > 0 {
						// Extract prefix (before popup)
						prefix = lipgloss.NewStyle().Width(startCol).Render(baseLine[:min(startCol, len(baseLine))])
					}
					endCol := startCol + popupWidth
					if endCol < baseWidth {
						// Extract suffix (after popup)
						suffix = baseLine[min(endCol, len(baseLine)):]
					}
					baseLines[row] = prefix + popupLine + suffix
				} else {
					baseLines[row] = popupLine
				}
			}
		}

		return strings.Join(baseLines, "\n")
	}

	return baseView
}

func openInExternalEditor(path string) tea.Cmd {
	editor := config.ExternalEditor
	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		return nil
	})
}

func main() {
	versionFlag := flag.Bool("v", false, "Print version and exit")
	versionFlagLong := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag || *versionFlagLong {
		fmt.Println("notes version", getVersion())
		os.Exit(0)
	}

	// Load configuration
	config = loadConfig()
	notesPath = config.NotesPath
	applyColorConfig()

	if err := os.MkdirAll(notesPath, 0755); err != nil {
		log.Fatal("Could not create notes directory:", err)
	}
	trashPath := filepath.Join(notesPath, ".trash")
	if err := os.MkdirAll(trashPath, 0755); err != nil {
		log.Fatal("Could not create trash directory:", err)
	}

	rootNote := loadNotes(notesPath)
	trashNote := loadNotes(trashPath)

	// Load cursor positions
	cursorPositions := loadCursorPositions()

	// Initialize custom editor
	editor := NewEditor()
	editor.SetPlaceholder("Start typing your note...")

	initialModel := model{
		mode:            navigationView,
		currentNode:     rootNote,
		trashNode:       trashNote,
		editor:          editor,
		cursorPositions: cursorPositions,
	}
	initialModel.sortNotes()

	p := tea.NewProgram(&initialModel, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
