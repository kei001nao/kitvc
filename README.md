# kitvc

low-tide をベースにした、ローカルメディア管理・再生ソフトウェア。

## 特徴

- **ハイブリッド再生**: 音楽は TUI、動画は MPV ウィンドウで再生。
- **自動分類**: ファイル名からシリーズ、シーズン、エピソードを自動抽出。
- **レジューム再生**: 音楽・動画の再生位置を自動保存し、次回再開。
- **視覚的ライブラリ**: `ffmpeg` で生成したサムネイルを TUI 上に表示。
- **カスタムメタデータ**: 動画の種類、区分、シリーズなどを手動で編集可能。

## 依存関係

### システムパッケージ (必須)
- **Python 3.11+**
- **MPV**: 再生エンジンおよび IPC 制御に必要。
- **FFmpeg**: 動画のサムネイル生成に必要。
- **SQLite3**: データベース保存に必要（通常 Python に同梱）。

### Python ライブラリ
- **textual**: TUI フレームワーク。
- **textual-image**: 端末上での画像表示。
- **mutagen**: 音楽メタデータの抽出。
- **Pillow**: 画像処理のバックエンド。

## インストール方法

### CachyOS / Arch Linux の場合
システムパッケージとしてインストールすることをお勧めします。

```bash
sudo pacman -S mpv ffmpeg python-textual python-textual-image python-mutagen python-pillow
```

### その他のディストリビューション (pip) の場合

```bash
# システムパッケージのインストール (例: Ubuntu)
sudo apt install mpv ffmpeg

# Python ライブラリのインストール
pip install textual textual-image mutagen Pillow
```

## 設定

設定ファイルは `~/.config/kitvc/config.toml` に作成されます。

```toml
[music]
directories = ["/home/user/Music"] # 音楽ディレクトリを複数指定可能

[video]
directories = ["/home/user/Videos"] # 動画ディレクトリを複数指定可能

[player]
mpv_args = [] # MPVに渡す追加の引数
```

## 使い方

1. `python3 main.py` を実行してアプリを起動します。
2. 初回起動時に指定されたディレクトリをスキャンします（動画はサムネイル生成のため時間がかかる場合があります）。
3. **操作方法**:
    - `Music` / `Video`: サイドバーで切り替え。
    - `Enter`: 項目を選択、再生。
    - `e`: 動画一覧で選択中にメタデータ編集画面を開く。
    - `Space`: 再生 / 一時停止。
    - `Backspace`: 前の画面に戻る。
    - `q`: 終了。
```bash
python3 main.py
```
