# dux

ディスク使用量をフォルダ単位で再帰的に集計・表示するGo製CLIツール。
不要な大容量ファイルの整理を主な用途とする。

## ファイル構成

- [main.go](main.go) — スキャン・出力モード切り替え・フラグ定義
- [tui.go](tui.go) — インタラクティブTUI（tviewベース）
- [console_windows.go](console_windows.go) — Windows向けANSIエスケープ有効化（`kernel32.dll`経由）
- [console_other.go](console_other.go) — 非Windows向けスタブ

## 動作モード

| 条件 | モード |
|---|---|
| stdout がターミナル | TUIモード（デフォルト） |
| `-flat` / `-no-color` / パイプ出力 | テキストモード |

## フラグ

| フラグ | TUI | テキスト | 説明 |
|---|---|---|---|
| `-j N` | ✓ | ✓ | 並列スキャン数（デフォルト: CPU数。SSDでは増やすと高速化） |
| `-min SIZE` | ✓ | ✓ | 最小サイズフィルタ（例: `1MB`, `500KB`, `2GB`） |
| `-name` | ✓ | ✓ | 名前順ソート（デフォルト: サイズ降順） |
| `-flat` | — | ✓ | フラットリスト表示（TUI無効化） |
| `-no-color` | — | ✓ | カラー出力を無効化（TUI無効化） |
| `-depth N` | — | ✓ | 最大表示深さ（-1 = 無制限） |
| `-top N` | — | ✓ | 表示件数の上限（ツリー: 各階層の上位N件） |

## TUIキー操作

| キー | 動作 |
|---|---|
| `↑` / `↓` | カーソル移動 |
| `Enter` / `Space` / `→` | 展開 / 折りたたみ |
| `←` | 折りたたむ |
| `q` / `Esc` | 終了 |

## 主要な構造・関数

- `Config` — フラグ設定をまとめた構造体
- `DirNode` — ツリー表示用ノード（パス・サイズ・深さ・子ノード）
- `scanner` — 並列スキャナ。セマフォチャネルで並列数を制御し、`sync.Mutex` + `sync/atomic` でサイズを集計
- `buildTree` — `dirSizes` マップからツリー構造を構築
- `printTree` / `printFlat` — テキストモードの出力
- `runTUI` — tviewベースのインタラクティブTUI（遅延展開）
- `parseSize` — `"100MB"` 等の文字列をバイト数にパース

## ビルド

```powershell
go build -o dux.exe .
```

`rsrc.syso` はリポジトリに含まれており、ビルド時に自動リンクされる。
マニフェストを変更した場合のみ再生成が必要:

```powershell
# go install github.com/akavel/rsrc@latest
~\go\bin\rsrc.exe -manifest dux.exe.manifest -o rsrc.syso
```

## 注意事項

- スキャン進捗はstderrに出力、結果はstdoutに出力（パイプ・リダイレクト可能）
- アクセス拒否ディレクトリはスキップして集計を継続
- `dux.exe.manifest` は `asInvoker` を宣言しUAC昇格を防止（`C:\Windows\System32\diskusage.exe` との名前衝突が解消済みのため予防的措置）
- TUI は `tview` + `tcell`（Win32 Console APIドライバ）を使用。入力レイテンシあり → `bubbletea` への移行を検討中
