<div align="center">

<img src="work/resources/icon.png" width="128" alt="Candle 图标">

# Candle

一个照着 TradingView 做的 Binance K 线图

Go 写的原生 Windows / macOS 程序，不是网页套壳。拖动、缩放、十字光标、指标、画线、盈亏比，手感和 TradingView 对得上；一个 exe 或一个 app，不用账号，不用 API Key。

![Windows 10 / 11](https://img.shields.io/badge/Windows-10%20%2F%2011-23272e?style=flat-square&logo=windows&logoColor=white) ![macOS 12+](https://img.shields.io/badge/macOS-12%2B-23272e?style=flat-square&logo=apple&logoColor=white) ![Go 1.27](https://img.shields.io/badge/Go-1.27-23272e?style=flat-square&logo=go&logoColor=white) ![MyGo](https://img.shields.io/badge/UI-MyGo-26a69a?style=flat-square) ![Binance 现货](https://img.shields.io/badge/数据-Binance%20现货-f0b90b?style=flat-square) ![MIT](https://img.shields.io/badge/license-MIT-26a69a?style=flat-square)

<img src="docs/screenshots/hero.webp" width="900" alt="主图加 MA、成交量和 MACD，浅色和深色">

</div>

## 为什么用它

**手感和 TradingView 一样。** 拖动平移，滚轮以鼠标所在的那根 K 线为中心缩放，双击回到最新。价格轴上滚轮或上下拖可以单独缩放价格，常规、百分比、对数三种坐标随时切。十字光标贯穿所有窗格，轴上直接读数；最新价标签带这根 K 线的收盘倒计时，按 Binance 服务器的时钟算。

**指标按 TradingView 的公式算。** MA、EMA、BOLL 叠在 K 线上，MACD、RSI、KDJ、成交量各占一栏，栏与栏之间的线可以拖。同一种指标能加多个（MA5、MA20……），参数、源、偏移、线色、线宽都能改，价格轴上标出每条线的最新值。EMA 首值用 SMA，RSI 用 Wilder 平滑，BOLL 用总体标准差，RSI 的结果和 StockCharts 的公开算例逐个对过。实时推送只重算最后一根；离屏测试里 3000 根 K 线加 8 个指标，一帧约 6 毫秒。

**画线和盈亏比。** 趋势线、射线、水平线、水平射线、垂直线、平行通道、矩形、斐波那契回撤、价格区间、文字，外加多头和空头盈亏比：点一下放好入场、止盈、止损，填上账户金额和风险比例，就能算出该开多少仓、打到止盈止损各赚亏多少。有磁吸，能撤销重做、复制粘贴、锁定。画线按交易对保存，锚点记的是时间，换个周期也落在同一时刻。

**全部交易对，实时跳动。** 右栏搜索 Binance 现货的 1300 多个交易对，点一下就看、顺手加进自选；自选列表的 24 小时行情实时更新。断线会自动重连并补齐漏掉的 K 线，往左拖到头会自动加载更早的历史。

**设置能调的都给你。** 五种图表类型、涨跌色、按前收盘价着色、标签颜色（白涨黑跌配白底也看得清）、时区、日期和时间格式、网格、十字光标样式、水印、边距……改了立刻生效，点取消就还原。浅色、深色或跟随系统。

**小、安静。** Windows 上一个 13 MB 的 exe，macOS 上一个 12 MB 的 app，不用装运行时。只用 Binance 的公开行情，不需要登录，也不碰你的账户。

## 截图

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/indicators-dark.webp" alt="BOLL、RSI、KDJ，深色"></td>
    <td width="50%"><img src="docs/screenshots/drawings-light.webp" alt="趋势射线、支撑线和多头盈亏比，浅色"></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/settings-light.webp" alt="图表设置"></td>
    <td width="50%"><img src="docs/screenshots/indicators-list-dark.webp" alt="添加指标"></td>
  </tr>
</table>

## 开始使用

目前没有发布编译好的版本，自己编译一下就行（要装 [Go](https://go.dev/dl/)，版本低于 1.27.3 会自动下载对应的工具链）：

```powershell
git clone https://github.com/margbug01/Candle
cd Candle/work
go build -trimpath -ldflags="-H windowsgui -s -w" -o ../outputs/Candle.exe .
```

打开 `outputs/Candle.exe` 就能用。

macOS 上用 MyGo 打包成 app（Apple 芯片）：

```sh
git clone https://github.com/margbug01/Candle
cd Candle/work
go run github.com/egoist/mygo/cmd/mygo@v0.6.4 build -skip-dmg
```

打开 `outputs/darwin-arm64/Candle.app`，或者把它拖进「应用程序」。去掉 `-skip-dmg` 会再打一个 dmg。

- 行情来自 Binance 的公开行情服务 `data-api.binance.vision` 和 `data-stream.binance.vision`，不需要 API Key。
- 网络连不上 Binance 时，Candle 会走系统代理：Windows 上是「设置 › 网络和 Internet › 代理」，macOS 上是「系统设置 › 网络 › 代理」，Clash、v2rayN、Shoes 打开的就是它；也认 `HTTPS_PROXY` / `HTTP_PROXY` 环境变量。
- 自选、周期、指标、画线和图表设置都存在 `%APPDATA%\Candle\settings.json`（macOS 上是 `~/Library/Application Support/Candle/settings.json`），指标是所有交易对共用的，画线跟着交易对走。

## 操作

| 操作 | 作用 |
|---|---|
| 拖动图表 | 平移时间；价格被手动缩放过时也上下移动 |
| 滚轮 | 以鼠标为中心缩放时间，所有窗格一起 |
| 价格轴上滚轮、上下拖 | 只缩放这一栏的价格 |
| 双击价格轴 | 这一栏的价格回到自动 |
| 双击图表 | 回到最新一根，恢复默认缩放 |
| 拖窗格之间的线 | 调整副图高度 |
| Esc | 放下画线工具，取消选中 |
| Delete、Backspace（Mac 键盘上的 delete） | 删除选中的画线 |
| Ctrl+Z、Ctrl+Y（macOS：⌘Z、⇧⌘Z） | 撤销、重做画线（Ctrl+Shift+Z、⌘Y 也是重做） |
| Ctrl+C、Ctrl+V（macOS：⌘C、⌘V） | 复制、粘贴画线 |
| 右键图表 | 重置图表、坐标模式、隐藏画线、设置 |
| ⌘,（macOS） | 打开设置 |

## 指标怎么算

<details>
<summary>展开看公式</summary>

| 指标 | 默认参数 | 算法 |
|---|---|---|
| MA | 5 | 源（默认收盘价）的简单平均 |
| EMA | 12 | α = 2/(n+1)，第一个值是前 n 根的 SMA |
| BOLL | 20, 2 | 中轨 = SMA(20)；上下轨 = 中轨 ± 2 × 总体标准差 |
| MACD | 12, 26, 9 | DIF = EMA(12) − EMA(26)；DEA = EMA(DIF, 9)；柱 = DIF − DEA |
| RSI | 14 | 涨幅、跌幅各做 Wilder 平滑（RMA，α = 1/n，首值是 SMA） |
| KDJ | 9, 3, 3 | RSV 是收盘价在 9 根最高最低之间的位置；K、D 是逐级的移动平均，从 0 起（同 TradingView 社区的 KDJ 脚本）；J = 3K − 2D |
| VOL | 20 | 成交量和它的 20 根均量 |

MA、EMA、BOLL、MACD、RSI 可以换源（开、高、低、收、hl2、hlc3、ohlc4），MA、EMA、BOLL 可以设偏移。

</details>

## 还没做的

- 只接了 Binance 现货；合约和美股还没有
- 警报、脚本、多图布局、指标模板
- macOS 只做了 Apple 芯片的版本，没有 Intel 的

## 常见问题

<details>
<summary><b>指标数值和别的软件对不上？</b></summary>

EMA、RSI、MACD 都是递推的，开头几十根会受起点影响，加载的历史越多越接近；Candle 一次加载 1000 根，往左拖还会继续补。KDJ 和国内软件（同花顺、通达信）的差别在起点：它们的 K、D 从 50 起，TradingView 的脚本和 Candle 从 0 起，开头几根不同，之后会收敛到一起。

</details>

<details>
<summary><b>K 线一直加载不出来？</b></summary>

先确认浏览器能打开 <https://data-api.binance.vision/api/v3/ping>。打不开的话，打开代理软件的「系统代理」再重试；Candle 会重新读系统代理（Windows 上每次连接都读，macOS 上最多隔 3 秒），不用重启。目前不支持 PAC 脚本；SOCKS 代理只有 macOS 上认。

</details>

<details>
<summary><b>为什么不把字体打包进去？</b></summary>

主题写的是 `MiSans, system-ui`：装了 [MiSans](https://hyperos.mi.com/font/zh/download/) 就用，没装就用系统字体。

</details>

## 构建与开发

```powershell
cd work
go test ./...
go build -trimpath -ldflags="-H windowsgui -s -w" -o ../outputs/Candle.exe .
```

macOS 上：

```sh
cd work
go test ./...
go run github.com/egoist/mygo/cmd/mygo@v0.6.4 build -skip-dmg
```

app 的名字、bundle id 和版本在 `work/mygo.json`。没配开发者证书时是 ad-hoc 签名，本机直接能用；发给别人的话，对方第一次打开会被拦下，要到「系统设置 › 隐私与安全性」里点「仍要打开」。

- 界面测试用 MyGo 的 `ui.Tester` 离屏渲染，真的去点按钮、拖图表；测试不联网。`CANDLE_SHOTS=<文件夹> go test` 会把画面存成 PNG。
- `CANDLE_LIVE=1 go test -run TestLive .` 真连 Binance，跑一遍加载、实时推送、往前补页，并核对指标的增量计算和整段重算一致。
- README 的截图是假数据：`CANDLE_README=<文件夹> go test -run TestReadmeShots .`，再回到仓库根目录 `python tools/shots.py <文件夹> docs/screenshots`（要 Pillow）。
- 图标的母版是 [docs/icon.svg](docs/icon.svg)，顶栏用的是去掉底板的 [docs/logo.svg](docs/logo.svg)。exe 的图标、版本和 manifest 在 `work/rsrc_windows_amd64.syso`，改了用 go-winres 重新生成：

```powershell
go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --icon resources/icon.png --manifest gui --product-name Candle --file-description "Candle K 线图" --product-version 0.1.0 --file-version 0.1.0 --original-filename Candle.exe
```

- macOS 的图标另有母版 [docs/icon-macos.svg](docs/icon-macos.svg)：同一个图案，按 Mac 图标的网格缩到 824 像素，带阴影。改了在 Mac 上回到仓库根目录重新生成 `work/assets/icon-macos.png`（用系统的 Quick Look 渲染，不用装别的）：

```sh
go run tools/macicon.go docs/icon-macos.svg work/assets/icon-macos.png
```

## 致谢

- [MyGo](https://github.com/egoist/mygo)：Go 的原生界面库，Candle 的界面全靠它
- [coder/websocket](https://github.com/coder/websocket)：实时行情的 WebSocket
- [Binance](https://developers.binance.com/docs/binance-spot-api-docs)：公开行情接口
- [TradingView](https://www.tradingview.com/)：界面、交互和指标口径的参照
- [MiSans](https://hyperos.mi.com/font/zh/)：小米的免费商用字体（不随 Candle 分发）

Candle 是个人项目，和 TradingView、Binance 没有关系，也不构成任何投资建议。

## 许可证

[MIT](LICENSE)
