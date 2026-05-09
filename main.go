package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-isatty"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

type Config struct {
	MaxDepth    int
	TopN        int
	MinSize     int64
	SortByName  bool
	Flat        bool
	NoColor     bool
	Concurrency int
}

type DirNode struct {
	Path     string
	Name     string
	Size     int64
	DiskSize int64 // actual on-disk allocation; 0 means not measured
	Depth    int
	Children []*DirNode
}

type scanner struct {
	sizes      map[string]int64
	diskDeltas map[string]int64 // per-dir sum of (diskSize - logicalSize) for files >= 10GB
	mu         sync.Mutex
	sem        chan struct{}
	fileCount  int64
	dirCount   int64
	errCount   int64
}

func newScanner(concurrency int) *scanner {
	return &scanner{
		sizes:      make(map[string]int64),
		diskDeltas: make(map[string]int64),
		sem:        make(chan struct{}, concurrency),
	}
}

func (s *scanner) scan(path string) (logical, diskDelta int64) {
	atomic.AddInt64(&s.dirCount, 1)

	entries, err := os.ReadDir(path)
	if err != nil {
		atomic.AddInt64(&s.errCount, 1)
		return 0, 0
	}

	var fileSize, fileDiskDelta int64
	var childSize, childDiskDelta int64
	var childMu sync.Mutex
	var wg sync.WaitGroup

	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			wg.Add(1)
			select {
			case s.sem <- struct{}{}:
				go func(p string) {
					defer wg.Done()
					defer func() { <-s.sem }()
					sz, dd := s.scan(p)
					childMu.Lock()
					childSize += sz
					childDiskDelta += dd
					childMu.Unlock()
				}(fullPath)
			default:
				sz, dd := s.scan(fullPath)
				childMu.Lock()
				childSize += sz
				childDiskDelta += dd
				childMu.Unlock()
				wg.Done()
			}
		} else {
			info, ferr := entry.Info()
			if ferr != nil {
				atomic.AddInt64(&s.errCount, 1)
				continue
			}
			atomic.AddInt64(&s.fileCount, 1)
			lsz := info.Size()
			fileSize += lsz
			if lsz >= 10<<30 {
				disk := getDiskSize(fullPath, lsz)
				fileDiskDelta += disk - lsz
			}
		}
	}
	wg.Wait()

	total := fileSize + childSize
	delta := fileDiskDelta + childDiskDelta
	s.mu.Lock()
	s.sizes[path] = total
	if delta != 0 {
		s.diskDeltas[path] = delta
	}
	s.mu.Unlock()

	return total, delta
}

func main() {
	maxDepth := flag.Int("depth", -1, "最大表示深さ (-1 = 無制限)")
	topN := flag.Int("top", 0, "表示件数の上限 (0 = 全て表示。ツリーモード: 各階層の上位N件)")
	minSizeStr := flag.String("min", "0", "最小サイズフィルタ (例: 1MB, 500KB, 2GB)")
	sortByName := flag.Bool("name", false, "名前順でソート (デフォルト: サイズ降順)")
	flat := flag.Bool("flat", false, "フラットリスト表示 (デフォルト: ツリー表示)")
	noColor := flag.Bool("no-color", false, "カラー出力を無効化")
	concurrency := flag.Int("j", runtime.NumCPU(), "並列スキャン数 (SSDでは増やすと高速化)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "使い方: dux [オプション] [パス]\n\n")
		fmt.Fprintf(os.Stderr, "フォルダ毎のディスク使用量を再帰的に集計します。\n\n")
		fmt.Fprintf(os.Stderr, "オプション:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n例:\n")
		fmt.Fprintf(os.Stderr, "  dux .\n")
		fmt.Fprintf(os.Stderr, "  dux C:\\Users\n")
		fmt.Fprintf(os.Stderr, "  dux -depth 2 -top 10 C:\\Users\n")
		fmt.Fprintf(os.Stderr, "  dux -min 100MB -flat C:\\Users\n")
		fmt.Fprintf(os.Stderr, "  dux -no-color C:\\Users > output.txt\n")
	}

	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	minSize, err := parseSize(*minSizeStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: 無効なサイズ指定 '%s': %v\n", *minSizeStr, err)
		os.Exit(1)
	}

	cfg := Config{
		MaxDepth:    *maxDepth,
		TopN:        *topN,
		MinSize:     minSize,
		SortByName:  *sortByName,
		Flat:        *flat,
		NoColor:     *noColor,
		Concurrency: *concurrency,
	}

	// -flat またはパイプ出力のときのみ ANSI テキストモード
	textMode := cfg.Flat || cfg.NoColor || !isatty.IsTerminal(os.Stdout.Fd())
	if textMode && !cfg.NoColor {
		enableANSI()
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: パスの解決に失敗: %v\n", err)
		os.Exit(1)
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: '%s' にアクセスできません: %v\n", absRoot, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "エラー: '%s' はディレクトリではありません\n", absRoot)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "スキャン中: %s (並列数: %d)\n", absRoot, cfg.Concurrency)
	start := time.Now()

	sc := newScanner(cfg.Concurrency)

	stopProgress := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r  %s ディレクトリ, %s ファイル スキャン中...",
					formatCount(atomic.LoadInt64(&sc.dirCount)),
					formatCount(atomic.LoadInt64(&sc.fileCount)))
			case <-stopProgress:
				return
			}
		}
	}()

	sc.scan(absRoot)
	close(stopProgress)

	fmt.Fprintf(os.Stderr, "\r                                                          \r")

	elapsed := time.Since(start)
	fmt.Fprintf(os.Stderr, "スキャン完了: %s ディレクトリ, %s ファイル (%.2f秒)\n",
		formatCount(sc.dirCount), formatCount(sc.fileCount), elapsed.Seconds())
	if sc.errCount > 0 {
		fmt.Fprintf(os.Stderr, "アクセスエラー: %s 件スキップ\n", formatCount(sc.errCount))
	}
	fmt.Fprintln(os.Stderr)

	if cfg.Flat {
		printFlat(sc.sizes, absRoot, cfg)
	} else {
		rootNode := buildTree(sc.sizes, sc.diskDeltas, absRoot)
		if rootNode == nil {
			return
		}
		if !textMode {
			if err := runTUI(rootNode, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "TUIエラー: %v\n", err)
			}
		} else {
			printTree(rootNode, cfg, "", true)
		}
	}
}

func buildTree(dirSizes map[string]int64, diskDeltas map[string]int64, absRoot string) *DirNode {
	nodes := make(map[string]*DirNode)

	for path, size := range dirSizes {
		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			continue
		}
		var depth int
		if relPath != "." {
			depth = strings.Count(relPath, string(filepath.Separator)) + 1
		}
		var diskSize int64
		if delta, ok := diskDeltas[path]; ok {
			diskSize = size + delta
		}
		nodes[path] = &DirNode{
			Path:     path,
			Name:     filepath.Base(path),
			Size:     size,
			DiskSize: diskSize,
			Depth:    depth,
		}
	}

	for path, node := range nodes {
		if node.Depth == 0 {
			continue
		}
		parent := filepath.Dir(path)
		if parentNode, ok := nodes[parent]; ok {
			parentNode.Children = append(parentNode.Children, node)
		}
	}

	return nodes[absRoot]
}

// diskNote returns a parenthetical disk-size annotation when on-disk allocation
// differs from logical size by >= 1 MB (non-zero DiskSize means it was measured).
func diskNote(node *DirNode, noColor bool) string {
	if node.DiskSize == 0 {
		return ""
	}
	diff := node.DiskSize - node.Size
	if diff < 0 {
		diff = -diff
	}
	if diff < 1<<20 {
		return ""
	}
	note := fmt.Sprintf(" (disk: %s)", formatSize(node.DiskSize))
	return colorStr(note, colorDim, noColor)
}

func printTree(node *DirNode, cfg Config, prefix string, isLast bool) {
	if cfg.MaxDepth >= 0 && node.Depth > cfg.MaxDepth {
		return
	}
	if node.Depth > 0 && node.Size < cfg.MinSize {
		return
	}

	sizeStr := colorizeSize(node.Size, cfg.NoColor, node.Depth == 0)
	note := diskNote(node, cfg.NoColor)

	if node.Depth == 0 {
		fmt.Printf("%s  %s%s\n", sizeStr, colorStr(node.Path, colorBold, cfg.NoColor), note)
	} else {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		fmt.Printf("%s  %s%s%s\n",
			sizeStr,
			colorStr(prefix+connector, colorDim, cfg.NoColor),
			colorStr(node.Name, colorCyan, cfg.NoColor),
			note,
		)
	}

	children := sortedChildren(node.Children, cfg)
	if cfg.TopN > 0 && len(children) > cfg.TopN {
		children = children[:cfg.TopN]
	}

	childPrefix := prefix
	if node.Depth > 0 {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	for i, child := range children {
		printTree(child, cfg, childPrefix, i == len(children)-1)
	}
}

func printFlat(dirSizes map[string]int64, absRoot string, cfg Config) {
	type entry struct {
		path string
		size int64
	}

	var entries []entry
	for path, size := range dirSizes {
		if size >= cfg.MinSize {
			entries = append(entries, entry{path, size})
		}
	}

	if cfg.SortByName {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].path < entries[j].path
		})
	} else {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].size != entries[j].size {
				return entries[i].size > entries[j].size
			}
			return entries[i].path < entries[j].path
		})
	}

	if cfg.TopN > 0 && len(entries) > cfg.TopN {
		entries = entries[:cfg.TopN]
	}

	for _, e := range entries {
		sizeStr := colorizeSize(e.size, cfg.NoColor, e.path == absRoot)
		fmt.Printf("%s  %s\n", sizeStr, colorStr(e.path, colorCyan, cfg.NoColor))
	}
}

func sortedChildren(nodes []*DirNode, cfg Config) []*DirNode {
	var result []*DirNode
	for _, n := range nodes {
		if n.Size >= cfg.MinSize {
			result = append(result, n)
		}
	}

	if cfg.SortByName {
		sort.Slice(result, func(i, j int) bool {
			return result[i].Name < result[j].Name
		})
	} else {
		sort.Slice(result, func(i, j int) bool {
			if result[i].Size != result[j].Size {
				return result[i].Size > result[j].Size
			}
			return result[i].Name < result[j].Name
		})
	}

	return result
}

func colorizeSize(size int64, noColor, isRoot bool) string {
	s := formatSize(size)
	if noColor {
		return s
	}
	if isRoot {
		return colorStr(s, colorBold, noColor)
	}
	switch {
	case size >= 1<<30:
		return colorStr(s, colorRed, noColor)
	case size >= 100<<20:
		return colorStr(s, colorYellow, noColor)
	case size >= 1<<20:
		return colorStr(s, colorGreen, noColor)
	default:
		return colorStr(s, colorDim, noColor)
	}
}

func colorStr(s, color string, noColor bool) string {
	if noColor || color == "" {
		return s
	}
	return color + s + colorReset
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%8.2f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%8.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%8.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%8.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%8d  B", bytes)
	}
}

func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	start := len(s) % 3
	if start > 0 {
		b.WriteString(s[:start])
	}
	for i := start; i < len(s); i += 3 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	upper := strings.ToUpper(s)
	units := []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	}

	for _, u := range units {
		if strings.HasSuffix(upper, u.suffix) {
			numPart := strings.TrimSpace(s[:len(s)-len(u.suffix)])
			n, err := strconv.ParseFloat(numPart, 64)
			if err != nil {
				return 0, fmt.Errorf("数値の解析に失敗: %w", err)
			}
			return int64(n * float64(u.mult)), nil
		}
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("サイズの解析に失敗 (例: 100MB, 1GB, 500KB)")
	}
	return n, nil
}
