# Agent 与 Dashboard 设计评审

状态：**评审（第 1 轮，2026-10-24）**。发现等待逐项确认；其中两个是缺陷，已修（D19、B10）。

画布：[`agent-dashboard-review/`](./agent-dashboard-review/)（Tingly Design 项目，`npx tingly-design open .design/agent-dashboard-review`；
截图在它的 `uploads/` 里）。
判断标准：[ux-principles.md](./ux-principles.md) 的 P1–P12。编号接着 [ux-review.md](./ux-review.md) §3：
Agent 页 A12 起，Profile 页 B7 起，Dashboard D7 起，跨页 X1 起。相关：[agent-page-redesign.md](./agent-page-redesign.md)、
[ui-flow-analysis.md](./ui-flow-analysis.md)（Agent 页头部的八轮修正）。

---

## 0. 结论先行

两页的骨架已经对了：Agent 页按"接上了吗 → 怎么连 → 请求怎么走"排，Dashboard 是一条工具栏、五张指标卡、一个 Tab 面板。
剩下的乱不在版式，而在五件小事反复出现：

1. **同样的按钮，不同的行为。** Agent 头部 Requests / Usage / Log 长得一样，一个开弹窗、一个跳到 Dashboard、一个开另一种弹窗（A12）。
2. **提示与修复分开放。** "Config out of date" 是左边一行字，修它的按钮在最右（A13）；新手还没配就先看到"过期"（A19）。
3. **没数据时也下结论。** 零流量时缓存命中率红色告警、错误率绿色健康（D7）；空状态不给下一步（D18）。
4. **一个控件三种长相。** Dashboard 三个子页三种时间段控件（D15）；"看最近的请求"有三处、三套列（X1）。
5. **名字没对齐。** Agent / 智能应用 / Scenario / `claude_code`；Quick Start / Setup Guide；Plugins 指两样东西；中文漏翻（X2、X3）。

前三条都能在现有组件里小改；后两条要先定词，再一次改齐。

## 1. 方法与范围

- 范围：`/agent/*`（Claude Code 默认页、Codex、OpenAI SDK、Claude Code Profile，以及 Requests / Log 弹窗）和
  `/dashboard/*`（Usage 的 Summary / By Request / Activity，Team usage，Quota history）。图标栏、落地页不在范围内（见 §6）。
- 方法：`.claude/skills/ui-preview` 的 mock 模式，1440×900；浅色英文为主，中文与深色抽查；`?mockData=newcomer` 看第一次打开。
  截图在 Tingly Design 画布上逐格标注，提案用线框画在同一画布里，颜色只用 `@tingly/ui` 的浅色 token。
- 局限：mock 数据。标"待真实后端核对"的两条（A19、D17）取决于真实状态来源，mock 说明不了。

## 2. 已经变好的

- Dashboard 单条工具栏，Agent 筛选可用 `?scenario=` 深链（#2030 等）。
- 指标卡去掉装饰图标，只有缓存命中率和错误率带状态点。
- Agent 页头部状态行是真实数字：已接入 · 最近请求 · 今日次数 · 错误率。
- Requests / Log 只在打开时加载，不占版面（ui-flow-analysis.md 第 1 轮的教训）。
- Connection 常驻；规则标题行 Smart 与 Direct 统一；Plugins 面板改成浮层后路由图不再被挤。
- OpenAI SDK 页（三个按钮、一张卡、规则）是现在最安静的一页，可以作为其他 Agent 页收敛的参照。

## 3. 发现

"状态"一列：**待定**＝等负责人确认是否算问题；**已修**＝本轮修了；**待核对**＝要在真实后端上看；**旧编号**＝ux-review 已记、仍在。

### Agent 页（画布板 1、2）

| # | 观察 | 原则 | 状态 |
|---|---|---|---|
| A12 | 头部五个按钮一个样子、三种行为：Requests、Log 开弹窗；Usage 跳到 Dashboard（图标栏也切走）；Quick Start 是"重置并重新显示"；Auto Config 才是动作 | P2 · P12 | 待定 |
| A13 | "Config out of date · 1 difference" 是不可点的字，修复按钮在 600px 外；英文下状态行折成三行，中文和 Codex 页是一行，头部高度随语言和 Agent 跳 | P11 | 待定 |
| A14 | 页面级 Plugins 一整张卡只有一行；规则行的 "Plugins 4" 是另一种东西；中文下页面级不翻译、规则级译成"插件" | P3 · P9 | 待定 |
| A15 | 规则标题行 7 种元素；灰色说明（"Smart routing + tiered fallback"）复述下面的路由图，中文下不翻译 | P9 | 待定 |
| A16 | 每条规则的第一列是 Direct / Smart 大按钮：规则的模式，和 1M、Endpoint 这些同级设置不在一处 | P2 · P4 | 待定 |
| A17 | Provider 卡上的 O / A 圆圈（协议）、空心红圈（健康）没有文字 | P5 · P9 | 待定 |
| A18 | 新手第一次打开：Quick Start（这页唯一要做的事）排在 Connection、Plugins 之后，首屏只露一半 | P1 | 待定 |
| A19 | 新手还没应用过配置，头部就显示 "Config out of date · 1 difference" | P6 · P7 | 待核对 |
| A20 | 新手页同屏两个实心蓝按钮（Auto Config、Quick Start 第 1 步的 Connect AI）；Connect AI 在规则区又出现一次 | P9 | 待定 |
| A5 | Local \| Docker 离它改的 URL 很远 | — | 旧编号 |

### Claude Code Profile 页（画布板 2）

| # | 观察 | 原则 | 状态 |
|---|---|---|---|
| B7 | 标题旁常驻红色删除图标，与编辑并排 | P9 · P12 | 待定 |
| B8 | 同一件事，默认页叫 Quick Start，Profile 页叫 Setup Guide | P3 | 待定 |
| B9 | 状态行写 profile 的内部 id（`p1`），不是用户起的名字（ds） | P5 | 待定 |
| B10 | 规则为空时提示"点击 New Model 添加"，这页没有 New Model（`allowAddRule={false}`） | P11 | **已修** |
| B2、B4 | Start \| Settings 与 NPX \| Global 叠在 Local \| Docker 上；Profile Overrides 大半是说明文字 | — | 旧编号 |

### Dashboard（画布板 4、5）

| # | 观察 | 原则 | 状态 |
|---|---|---|---|
| D7 | 零流量时缓存命中率整卡红色告警、错误率绿色"健康"：n = 0 也给了结论 | P7 | 待定 |
| D8 | Usage by Model 在 Tab 面板之外，三个 Tab 下都出现；看起来像 Summary 的一部分，By Request 时页面有两张表 | P1 | 待定 |
| D9 | Activity 固定看 12 个月，时间段下拉仍显示 Today；By Request 只在 Today / Yesterday 出现，换成 7 days 时 Tab 消失 | P4 · P12 | 待定 |
| D10 | 表头大写并折两行（CACHE / READ）；模型名灰色，比 Provider 淡 | P9 | 待定 |
| D11 | By Request 的 Scenario 列是内部 key（`claude_code`、`custom`），筛选器叫 Agent、显示 "Claude Code" | P3 · P5 | 待定 |
| D12 | By Request 表比面板宽：TTFT 之后的列被截，时间折两行 | P9 | 待定 |
| D13 | 数字按列上色（输入蓝、输出绿、延迟红黄绿），颜色不表达状态 | P9 | 待定 |
| D14 | 热力图月份标签重叠（"OctNov"） | P9 | 待定 |
| D15 | 三个子页三种时间段控件：Usage 下拉（Today … 90 days）、Team usage 右侧分段（Today / 7D / 30D / 90D，整条工具栏只有它）、Quota history 居中分段（5H / 1D / 7D / 30D） | P3 | 待定 |
| D16 | 状态两种写法：Team usage 整卡染红，Usage 只用一个点 | P9 | 待定 |
| D17 | Team usage 某账号同时显示 "Never used" 和 1.88K 请求：副标题读的是 key 的 `last_used_at`，不是用量 | P7 | 待核对 |
| D18 | 空状态写"换个时间段或稍后再看"；新手真正要做的是去接一个 Agent | P11 | 待定 |
| D19 | 深色下 Token 图的坐标轴文字看不见：recharts 收到的 `fill` 是 MUI 的 `'text.secondary'` 字符串，不是颜色 | P9 | **已修** |
| D5、D6 | Response Performance 行距大；二级侧栏三项 | — | 旧编号 |

### 跨页（画布板 7）

| # | 观察 | 原则 | 状态 |
|---|---|---|---|
| X1 | "看最近的请求"三处三套列：Agent › Requests（20 条，无表头，错误写成错误码）、Agent › Log › Requests（管线时间线）、Dashboard › By Request（表格） | P1 · P3 | 待定 |
| X2 | 内部 key 外露：Log 标题旁的 `claude_code`，Scenario 列的 `openai` / `anthropic` | P5 | 待定 |
| X3 | 词没对齐：Agent（图标栏、筛选器）/ 智能应用（中文图标栏、侧栏标题）/ Scenario（表头）；Plugins 指页面级卡片和规则上的插件；中文漏翻：页面级 Plugins、Add Smart Rule、Smart routing + tiered fallback、Latency、Image（图标栏）、Quota 历史 | P3 | 待定 |

## 4. 提案

每条可以单独做、单独回滚。线框在画布板 3（Agent）和板 6（Dashboard）。

**小改（同一组件内，各自不到半天）**

| 对应 | 提案 |
|---|---|
| A12 | Requests · Usage ↗ · Log 合成一个分段按钮组；Usage 带 ↗，说明会离开这页。实心蓝只给"这一刻该做的事"：Quick Start 没完成时是它的下一步，配置过期时是 Review & apply，否则头部没有实心按钮 |
| A13 | "Config out of date · 1 difference" 后面直接是 Review & apply（打开现有 Auto Config 弹窗，差异在里面）；状态行固定一行，放不下时截断次要信息 |
| A15、A17 | 规则展开时去掉灰色说明（折叠时保留一行摘要）；协议写成字（"DeepSeek · OpenAI, Anthropic"） |
| B7、B8、B9 | 删除收进"⋯"；Profile 页按钮叫 Quick Start；状态行写 profile 名 |
| D7、D18 | n = 0 时指标卡显示 "—" 与 "no requests yet"，不带点、不染色；空状态给 "Connect an agent →"（去 `/agent`），有筛选时才提示换范围 |
| D11、X2 | 表格与 Log 里的 Agent 列显示名称（与筛选器同名同值），列名叫 Agent |
| D13、D10 | Token 数字用正文色，只有错误率、延迟超阈值才着色；表头不全大写 |
| X3（翻译部分） | 补齐 zh / ru。`localeParity` 只查键是否齐，查不出"值仍是英文"，可以加一条检查 |

**中改（跨组件）**

| 对应 | 提案 |
|---|---|
| D15、D16 | 抽一个 Dashboard 工具栏组件给三个子页共用：同一种时间段控件（下拉），选项按页面能力取；指标卡状态一种写法（点） |
| D8、D9 | Usage by Model 收进 Summary；Activity 下范围显示 "Last 12 months" 并禁用；By Request 在日级范围下保留 Tab，内容里说明只看今天和昨天 |
| D12 | By Request 精简列：缓存读写合成一列，推理 Token 全空时隐藏；时间不折行 |
| A16 | Direct \| Smart 进规则标题行，和 1M、Endpoint、Plugins 同样大小；路由图第一列还给条件 |
| X1 | 一个"请求行"组件：Requests 弹窗是它的精简版（有表头），Log 是它展开后的管线，By Request 是它的表格形态 |

**需要拍板（是对已定方案的复议，只画不改）**

| 对应 | 提案 | 冲突的决定 |
|---|---|---|
| A18 | Quick Start 未完成时排在 Connection 之前，完成后照旧消失 | ui-flow-analysis.md 8.13："回到原来的位置" |
| A14 | 页面级 Plugins 回到 Connection 卡作为一行，或改名（如 "Client behavior"）以和规则上的 Plugins 分开 | ux-review.md §1 Agent 页 v4："页面级 Plugins 单独成卡" |
| X3（用词） | 中文图标栏 "智能应用" 与筛选器 "Agent" 统一成一个词 | — |

## 5. 本轮已修

| # | 改动 | 位置 |
|---|---|---|
| D19 | 坐标轴 `fill` 改为 `theme.palette.text.secondary`（小时图、日图） | `components/dashboard/TokenHistoryChart/{HourlyChart,DailyChart}.tsx` |
| B10 | 不能新建规则时，空状态改为"还没有模型规则"（新键 `templatePage.noRulesFixed`，en / zh / ru） | `pages/scenario/components/TemplatePage.tsx` |

## 6. 不碰 / 已定

- Dashboard 在图标栏的位置、落地页（`/agent`）：负责人还在考虑，本轮只记录，不提案。
- 路由图里每个 tier 行末常驻的 "+ Add model"：ux-review §1 已定保留（Quick Start 的聚光灯依赖它）。1440 宽下右侧被截是既有问题。
- Connection 卡常驻、Requests / Usage 不做常驻卡片：ui-flow-analysis 第 1、2 轮已定。

## 7. 还开着的问题

- A19、D17 的真实状态来源：从未应用配置时 `clientConfigStatus` 给的是 `outdated` 还是别的；Team usage 的 `last_used_at` 是否随请求更新。
- X1 的"请求行"组件要不要包含管线时间线（Log 的展开），还是只统一列和措辞。
- Team usage 的 Top accounts 与表格排序重复，是否保留。
- 窄屏（< 1280）没看；By Request 与规则路由图在窄屏下更挤。

## 8. 怎么继续

- 画布是"现在的样子 + 要改成的样子"：某条提案落地时，在同一个 PR 里更新对应板的截图（`uploads/`）、把状态改成"已修"，并重新导出 `images/` 里的预览。
- 下一轮直接改 §3 的状态列和 §4 的提案，新发现接着编号（A21、B11、D20、X4 起），旧编号不重排；被否决的提议留在表里，状态写"否决"并附理由。第一行的状态随之更新。
