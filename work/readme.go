package main

// readme is the indicators folder's README: for the user, and for an agent
// asked to write an indicator. It holds no backquotes, being one string.
const readme = `<!-- ` + readmeVersion + ` -->
# Candle 自定义指标

这个文件夹里的 .pine 文件就是 Candle 的自定义指标。写法是 TradingView 的 Pine Script（v5 / v6 的大部分，v4 也能跑一部分）。

- Candle 开着时，每 1.5 秒看一次这个文件夹。**新放进来的文件会自动加到当前图表上**；已经在图上的，文件一改马上重算。
- 顶栏「指标」›「自定义」里能新建、从剪贴板导入、编辑、删除，也能把这里的指标加到图上。
- 写错了不会崩：图上会提示错在第几行，编辑器里也会标出来。

## 给 Agent 的说明

用户让你给 Candle 写指标时，照下面做：

1. 一个指标一个文件，放在这个文件夹，扩展名 .pine，UTF-8 编码。**文件名就是它在 Candle 里的身份**，改名等于换了一个指标（图上那个会变成「找不到脚本」）。显示的名字取 indicator() 的第一个参数。
2. 第一行写 //@version=6，必须有一行 indicator("名字", overlay = true 或 false)：overlay = true 画在 K 线上，false 画在 K 线下面单独一栏。
3. 写完等 2 秒，读同一文件夹的 _status.json：每个文件一条，ok 为 false 时 error 里写着第几行、什么错，改好再看一次。warnings 是 Candle 跳过的东西（比如 label）。
4. 改已有的指标就直接改它的文件；要新的就新建文件，别改别人的文件名。
5. 不要写这个文件夹以外的东西，也不用碰 Candle 的设置。

## 支持什么

- 声明：indicator(title, shorttitle, overlay, format, precision)。study() 也认。
- 输入（会出现在指标设置里，用户能改）：input.int、input.float（minval、maxval、step、options）、input.bool、input.source、input.string（带 options 是下拉框）、input.color，以及 input()。
- 语法：变量、var / varip、:= 和 += -= *= /=、if / else if / else、for i = a to b by s、while、break / continue、switch（有值和无值两种）、if 和 switch 当表达式用、三元 ?:、历史引用 x[n]、自定义函数（单行和多行，可以返回 [a, b]）、[a, b] = f() 解构、类型写法（float x = na）。
- 数据：open high low close volume hl2 hlc3 ohlc4 hlcc4 time time_close bar_index last_bar_index，barstate.*、syminfo.mintick / ticker、timeframe.period / isintraday / isdaily…、timeframe.change("D")、time("D")、year month dayofmonth dayofweek hour minute（按交易所时区：加密货币是 UTC，美股纽约，期货芝加哥）。
- ta：sma ema rma wma vwma hma alma swma linreg stdev variance dev median percentrank highest lowest highestbars lowestbars range change mom roc rsi cci stoch mfi cmo tsi wpr tr atr bb bbw kc kcw macd dmi sar supertrend vwap obv accdist cum max min cross crossover crossunder rising falling barssince valuewhen pivothigh pivotlow correlation nvi pvi。
- math：abs max min avg sum sqrt pow exp log log10 round floor ceil sign sin cos tan asin acos atan todegrees toradians round_to_mintick pi e。
- 其他：na nz fixnan int float bool、color.new color.rgb color.from_gradient color.r/g/b/t 和 color.red 这类颜色、str.tostring str.format。
- 画：plot（style_line linebr stepline histogram columns circles cross area，color 可以每根 K 线不同，linewidth、offset、display = display.none）、hline、fill（两个 plot 或两个 hline）、bgcolor、plotshape、plotchar、plotarrow。

## 不支持什么

- request.security 这类拿别的品种、别的周期的数据：会报错。
- strategy（策略回测）、library、import、type、method、array / matrix / map：会报错。
- label、line、box、table、polyline：不报错，但会跳过不画（warnings 里会写）。
- plotcandle、plotbar、barcolor、alertcondition：跳过。

## 一个例子

~~~
//@version=6
indicator("双均线交叉", overlay = true)
fastLen = input.int(9, "快线", minval = 1)
slowLen = input.int(21, "慢线", minval = 1)
fast = ta.ema(close, fastLen)
slow = ta.ema(close, slowLen)
plot(fast, "快线", color = color.orange)
plot(slow, "慢线", color = color.blue)
plotshape(ta.crossover(fast, slow), "金叉", shape.triangleup, location.belowbar, color.green, size = size.small)
plotshape(ta.crossunder(fast, slow), "死叉", shape.triangledown, location.abovebar, color.red, size = size.small)
~~~
`
