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
	root     *DirNode
	cfg      Config
	items    []tuiItem
	expanded map[*DirNode]bool
	cursor   int
	offset   int
	height   int
	width    int
}

func newTUIModel(root *DirNode, cfg Config) tuiModel {
	expanded := map[*DirNode]bool{root: true}
	return tuiModel{
		root:     root,
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
					m.expanded[node] = false
					m.items = buildTUIItems(m.root, m.expanded, m.cfg)
				}
			}
		}
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
		sb.WriteString(infoSt.Width(m.width).Render(
			fmt.Sprintf(" %s  %s", formatSize(node.Size), node.Path),
		))
	} else {
		sb.WriteString(infoSt.Width(m.width).Render(""))
	}
	sb.WriteByte('\n')

	// ステータスバー（キー操作ヒント）
	sb.WriteString(statusSt.Width(m.width).Render(
		" ↑↓/jk: 移動   Enter/Space/→/l: 展開   ←/h: 折りたたみ   q: 終了",
	))

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
	expander := "  "
	if len(it.node.Children) > 0 {
		if m.expanded[it.node] {
			expander = "▼ "
		} else {
			expander = "▶ "
		}
	}

	sized := sizeStyleOf(it.node.Size).Render(formatSize(it.node.Size))

	name := it.node.Name
	if it.depth == 0 {
		name = it.node.Path
	}
	if selected {
		name = boldSt.Render(name)
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

func runTUI(root *DirNode, cfg Config) error {
	p := tea.NewProgram(newTUIModel(root, cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
