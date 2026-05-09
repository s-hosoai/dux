package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// スタイル定義
var (
	sizeRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	sizeYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	sizeGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	sizeFaint  = lipgloss.NewStyle().Faint(true)
	cursorSt   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	boldSt     = lipgloss.NewStyle().Bold(true)
	infoSt     = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252"))
	statusSt   = lipgloss.NewStyle().Background(lipgloss.Color("238")).Foreground(lipgloss.Color("245"))
)

type tuiItem struct {
	node  *DirNode
	depth int
}

type tuiModel struct {
	root      *DirNode
	absRoot   string
	cfg       Config
	items     []tuiItem
	expanded  map[*DirNode]bool
	cursor    int
	offset    int
	height    int
	width     int
	reloading bool
}

type reloadMsg struct{ root *DirNode }

func startReload(absRoot string, cfg Config) tea.Cmd {
	return func() tea.Msg {
		sc := newScanner(cfg.Concurrency)
		sc.scan(absRoot)
		return reloadMsg{root: buildTree(sc.sizes, sc.diskDeltas, sc.fileSizes, sc.fileDiskSizes, absRoot)}
	}
}

func newTUIModel(root *DirNode, absRoot string, cfg Config) tuiModel {
	expanded := map[*DirNode]bool{root: true}
	return tuiModel{
		root:     root,
		absRoot:  absRoot,
		cfg:      cfg,
		items:    buildTUIItems(root, expanded, cfg),
		expanded: expanded,
	}
}

func buildTUIItems(root *DirNode, expanded map[*DirNode]bool, cfg Config) []tuiItem {
	var items []tuiItem
	var walk func(*DirNode, int)
	walk = func(node *DirNode, depth int) {
		items = append(items, tuiItem{node, depth})
		if expanded[node] {
			for _, child := range sortedChildren(node.Children, cfg) {
				walk(child, depth+1)
			}
		}
	}
	walk(root, 0)
	return items
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case tea.KeyMsg:
		visible := m.height - 2
		if visible < 1 {
			visible = 1
		}
		switch msg.String() {
		case "q", "Q", "esc", "ctrl+c":
			return m, tea.Quit

		case "r", "R":
			if !m.reloading {
				m.reloading = true
				return m, startReload(m.absRoot, m.cfg)
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				if m.cursor >= m.offset+visible {
					m.offset = m.cursor - visible + 1
				}
			}

		case "enter", " ", "right", "l":
			if m.cursor < len(m.items) {
				node := m.items[m.cursor].node
				if len(node.Children) > 0 {
					m.expanded[node] = !m.expanded[node]
					m.items = buildTUIItems(m.root, m.expanded, m.cfg)
					if m.cursor >= len(m.items) {
						m.cursor = len(m.items) - 1
					}
					if m.cursor >= m.offset+visible {
						m.offset = m.cursor - visible + 1
					}
				}
			}

		case "left", "h":
			if m.cursor < len(m.items) {
				node := m.items[m.cursor].node
				if m.expanded[node] {
					// collapse this node
					m.expanded[node] = false
					m.items = buildTUIItems(m.root, m.expanded, m.cfg)
				} else {
					// already collapsed or leaf: jump to parent and collapse it
					curDepth := m.items[m.cursor].depth
					if curDepth > 0 {
						for i := m.cursor - 1; i >= 0; i-- {
							if m.items[i].depth == curDepth-1 {
								m.cursor = i
								m.expanded[m.items[i].node] = false
								m.items = buildTUIItems(m.root, m.expanded, m.cfg)
								if m.cursor < m.offset {
									m.offset = m.cursor
								}
								break
							}
						}
					}
				}
			}
		}
	case reloadMsg:
		if msg.root != nil {
			m.root = msg.root
			m.expanded = map[*DirNode]bool{msg.root: true}
			m.items = buildTUIItems(msg.root, m.expanded, m.cfg)
			if m.cursor >= len(m.items) {
				m.cursor = len(m.items) - 1
			}
		}
		m.reloading = false
	}
	return m, nil
}

func (m tuiModel) View() string {
	if m.height == 0 {
		return ""
	}
	visible := m.height - 2
	if visible < 1 {
		visible = 1
	}

	var sb strings.Builder

	end := m.offset + visible
	if end > len(m.items) {
		end = len(m.items)
	}
	for i := m.offset; i < end; i++ {
		sb.WriteString(m.renderLine(i))
		sb.WriteByte('\n')
	}
	// 残りを空行で埋める
	for i := end - m.offset; i < visible; i++ {
		sb.WriteByte('\n')
	}

	// 情報バー（現在ノードのフルパス＋サイズ）
	if m.cursor < len(m.items) {
		node := m.items[m.cursor].node
		info := fmt.Sprintf(" %s  %s", formatSize(node.Size), node.Path)
		if node.DiskSize != 0 {
			diff := node.DiskSize - node.Size
			if diff < 0 {
				diff = -diff
			}
			if diff >= 1<<20 {
				info += fmt.Sprintf("  (disk: %s)", formatSize(node.DiskSize))
			}
		}
		sb.WriteString(infoSt.Width(m.width).Render(info))
	} else {
		sb.WriteString(infoSt.Width(m.width).Render(""))
	}
	sb.WriteByte('\n')

	// ステータスバー（キー操作ヒント）
	hint := " ↑↓/jk: 移動   Enter/Space/→/l: 展開   ←/h: 折りたたみ   r: 再読み込み   q: 終了"
	if m.reloading {
		hint = " 再読み込み中..."
	}
	sb.WriteString(statusSt.Width(m.width).Render(hint))

	return sb.String()
}

func (m tuiModel) renderLine(idx int) string {
	it := m.items[idx]
	selected := idx == m.cursor

	// カーソルマーカー
	cur := " "
	if selected {
		cur = cursorSt.Render(">")
	}

	indent := strings.Repeat("  ", it.depth)

	// 展開インジケータ
	var expander string
	if it.node.IsFile {
		expander = "· "
	} else if len(it.node.Children) > 0 {
		if m.expanded[it.node] {
			expander = "▼ "
		} else {
			expander = "▶ "
		}
	} else {
		expander = "  "
	}

	sized := sizeStyleOf(it.node.Size).Render(formatSize(it.node.Size))

	name := it.node.Name
	if it.depth == 0 {
		name = it.node.Path
	}
	if selected {
		name = boldSt.Render(name)
	} else if it.node.IsFile {
		name = sizeFaint.Render(name)
	}

	return cur + " " + indent + expander + sized + "  " + name
}

func sizeStyleOf(size int64) lipgloss.Style {
	switch {
	case size >= 1<<30:
		return sizeRed
	case size >= 100<<20:
		return sizeYellow
	case size >= 1<<20:
		return sizeGreen
	default:
		return sizeFaint
	}
}

func runTUI(root *DirNode, absRoot string, cfg Config) error {
	p := tea.NewProgram(newTUIModel(root, absRoot, cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
