package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func runTUI(root *DirNode, cfg Config) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("端末初期化失敗: %w", err)
	}

	app := tview.NewApplication().SetScreen(screen)

	rootTNode := tview.NewTreeNode(rootLabel(root)).
		SetReference(root).
		SetSelectable(true).
		SetExpanded(true)
	addTUIChildren(rootTNode, root, cfg)

	tree := tview.NewTreeView().
		SetRoot(rootTNode).
		SetCurrentNode(rootTNode)

	infoBar := tview.NewTextView().
		SetDynamicColors(true).
		SetText(infoText(root))

	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetText("[gray]↑↓: 移動   Enter/Space/→: 展開   ←: 折りたたみ   q: 終了[-]")

	tree.SetChangedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if ref == nil {
			return
		}
		infoBar.SetText(infoText(ref.(*DirNode)))
	})

	toggle := func(node *tview.TreeNode) {
		if node == rootTNode {
			return
		}
		ref := node.GetReference()
		if ref == nil {
			return
		}
		dirNode := ref.(*DirNode)
		if len(dirNode.Children) == 0 {
			return
		}
		expanded := !node.IsExpanded()
		if expanded && len(node.GetChildren()) == 0 {
			addTUIChildren(node, dirNode, cfg)
		}
		node.SetExpanded(expanded)
		node.SetText(nodeLabel(dirNode, expanded))
	}

	tree.SetSelectedFunc(toggle)

	tree.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q', 'Q':
				app.Stop()
				return nil
			case ' ':
				toggle(tree.GetCurrentNode())
				return nil
			}
		case tcell.KeyEscape:
			app.Stop()
			return nil
		case tcell.KeyRight:
			cur := tree.GetCurrentNode()
			if cur == nil {
				return event
			}
			ref := cur.GetReference()
			if ref == nil {
				return event
			}
			dirNode := ref.(*DirNode)
			if !cur.IsExpanded() && len(dirNode.Children) > 0 {
				if len(cur.GetChildren()) == 0 {
					addTUIChildren(cur, dirNode, cfg)
				}
				cur.SetExpanded(true)
				cur.SetText(nodeLabel(dirNode, true))
				return nil
			}
		case tcell.KeyLeft:
			cur := tree.GetCurrentNode()
			if cur == nil || cur == rootTNode {
				return event
			}
			if cur.IsExpanded() {
				ref := cur.GetReference()
				if ref != nil {
					cur.SetExpanded(false)
					cur.SetText(nodeLabel(ref.(*DirNode), false))
				}
				return nil
			}
		}
		return event
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tree, 0, 1, true).
		AddItem(infoBar, 1, 0, false).
		AddItem(statusBar, 1, 0, false)

	return app.SetRoot(layout, true).Run()
}

func addTUIChildren(tNode *tview.TreeNode, dirNode *DirNode, cfg Config) {
	for _, child := range sortedChildren(dirNode.Children, cfg) {
		childTNode := tview.NewTreeNode(nodeLabel(child, false)).
			SetReference(child).
			SetSelectable(true).
			SetExpanded(false)
		tNode.AddChild(childTNode)
	}
}

func rootLabel(node *DirNode) string {
	return fmt.Sprintf("[::b]%s  %s[-:-:-]", sizeTag(node.Size), tview.Escape(node.Path))
}

func nodeLabel(dirNode *DirNode, expanded bool) string {
	indicator := "  "
	if len(dirNode.Children) > 0 {
		if expanded {
			indicator = "▼ "
		} else {
			indicator = "▶ "
		}
	}
	return fmt.Sprintf("%s%s  %s", indicator, sizeTag(dirNode.Size), tview.Escape(dirNode.Name))
}

func sizeTag(size int64) string {
	s := formatSize(size)
	switch {
	case size >= 1<<30:
		return "[red]" + s + "[-]"
	case size >= 100<<20:
		return "[yellow]" + s + "[-]"
	case size >= 1<<20:
		return "[green]" + s + "[-]"
	default:
		return "[::d]" + s + "[-:-:-]"
	}
}

func infoText(node *DirNode) string {
	return fmt.Sprintf("%s  %s", sizeTag(node.Size), tview.Escape(node.Path))
}
