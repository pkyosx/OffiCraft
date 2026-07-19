# officraft

single-owner 的 AI 工作室平台：一位人類 **owner** 帶著若干 **AI member**，跑在你自己的機器上。server 是唯一真相源（chat、task、member、global-context 全收斂在這裡），每台機器上的 warden 管 agent 生命週期，agent 拿 server 發的 token 幹活。server 只綁 loopback，對外一律走 tunnel。

## 安裝

### 方式一:官方 release(建議)

到 [GitHub Releases](https://github.com/pkyosx/OffiCraft/releases) 下載
`officraft-<tag>-darwin-arm64.tar.gz`,解開後跑一句:

```bash
./install.sh
```

它會:

1. 只支援 macOS Apple Silicon(darwin/arm64),其他平台直接拒絕。
2. **偵測既有安裝**(`~/.officraft/bin` 的 binary 或既有資料庫):有的話大聲警告並要求確認(互動 y/N,預設否;非互動要帶 `--force` 才會覆蓋),否則中止、什麼都不動。
3. 裝 `ocserverd`/`ocwarden`/`ocagent` 到 `~/.officraft/bin`,跑資料庫 migration(資料保留、原地升級)。
4. **預設埠 8780**(OffiCraft 標準埠;`$OC_CONFIG` / `./oc.toml` 可覆蓋)。埠被占用時明確報錯並提示換埠,不會默默裝下去。
5. 前景啟動 server;首次安裝 log 會印一次性設定連結(`http://127.0.0.1:8780/?code=…`),打開設定 owner 密碼。Ctrl-C 停止,之後用 `~/.officraft/bin/ocserverd serve` 再啟動(launchd 自動啟動是後續 ticket)。

之後的升級不必重跑 install.sh:設定 › 軟體更新 有「檢查更新」與一鍵升級(從 GitHub Releases 下載、sha256 驗證後原地抽換重啟);打開「自動更新」則在背景自動升級。「接收 Beta」= 也吃 GitHub prerelease。

### 方式二:從原始碼(開發機)

在 repo 根目錄跑一句:

```bash
bin/ocserver install
```

裝完成功會印出一格 banner,裡面有一次性的**啟用碼**:照指示打開瀏覽器、貼上啟用碼、設定你自己的 owner 密碼。密碼一設好,啟用碼立即失效;重裝(密碼已設)不會再印任何憑證。

其他用法:

```bash
bin/ocserver install --dry-run  # 只印出打算做什麼,什麼都不動
bin/ocserver install --force    # 重跑每一步(reinstall;不動既有密碼)
```

前置需求:macOS(launchd)、Go、node/npm、python3 ≥ 3.11;`cloudflared` 只有要開 tunnel 才需要。
以下「裝完機器上多了什麼 / 移除」兩節描述的是這條 `bin/ocserver` 原始碼路徑;release tarball 路徑只落 `~/.officraft/bin` + 資料庫,不裝任何 launchd job。

## 裝完機器上多了什麼

所有東西都落在 `~/.officraft/server/`：

```
~/.officraft/server/
  repo/     server 程式碼（autodeploy 會自己 pull + 重建）
  data/     SQLite 資料庫（你的所有資料都在這）
  oc.toml   本機設定（port、DSN；重裝不會被覆蓋。密碼不在這——DB 只存雜湊）
  log/      執行 log
```

外加三個 launchd job（`~/Library/LaunchAgents/`，開機自動起）：

| job | 做什麼 |
| --- | --- |
| `com.officraft.serve` | server 本體（`bin/ocserver` 路徑沿用 port 8770;oc.toml 可改，只綁 loopback） |
| `com.officraft.autodeploy` | 盯 git 遠端，有新 code 自動 pull → build → 重啟 |
| `com.officraft.tunnel` | cloudflared 對外通道（**選用**：機器上沒有 cloudflared 設定就自動略過） |

除此之外不碰你機器上任何其他東西。

## 移除

```bash
bin/ocserver uninstall             # 卸掉三個 launchd job、刪 ~/.officraft/server
                                   # 但保留 data/（資料庫，含密碼雜湊）與 oc.toml（設定）——之後重裝即回原狀
bin/ocserver uninstall --purge     # 全部刪光,含資料庫與密碼（會要求輸入確認；--yes 跳過）
bin/ocserver uninstall --dry-run   # 只印出會做什麼，什麼都不動
```

## 開發者

技術棧、repo 結構、怎麼跑測試與 CI，見 [docs/dev.md](docs/dev.md)。
