# 产品

## 定位

Oriel 是单用户的混合信息流阅读器：订阅原生 RSS 和 RSSHub，一条时间线混排文章、视频、社交和图片，摘要和翻译按需走用户自己的 OpenAI 兼容接口。它用 Go 和 MyGo 原生 UI，不用 WebView。

## 范围

计划的范围，按 [milestone](https://github.com/yangflow/oriel/milestones) 的顺序逐项落地（规则见 [roadmap](roadmap.md)）：

- 时间线混排四种形态：`article`、`video`、`social`、`image`。形态按源决定，条目跟随源。
- 添加订阅：博客 RSS、YouTube / B 站 / X / GitHub / Telegram / 微博链接，或 RSSHub 路径。
- 文章阅读器：全文抽取，抽不到就显示 RSS 正文并标明不是全文。
- 视频和社交条目显示封面和摘要，点开交给系统浏览器打开原站。没有 WebView，所以不内嵌官方播放器。
- 按需 AI 摘要和翻译，prompt 可全局改，也可按源覆盖。
- 导入、导出 OPML。

不做：多用户、发现台、推荐、高亮笔记、插件、MCP、播客、邮件 inbox、自建播放器、登录原站账号。

## 架构

同一个程序既是服务端又是客户端（见 [ADR 0002](adr/0002-standalone-client-and-server.md)）：

- 服务端负责抓取、正文抽取、存储、AI 调用和清理，并提供 HTTP API。
- 客户端是原生界面，只通过 HTTP API 读写数据。
- 桌面模式在本机启动服务端、只监听 `127.0.0.1`，再打开窗口。无窗口模式只跑服务端，供其他设备上的 Oriel 客户端连接。
- 桌面模式没有首次设置，打开就进时间线。也可以改连另一台电脑上的无窗口模式：填服务端地址和口令登录，口令只在登录时发送，不保存。

## 运行模式

| | 桌面模式 | 无窗口模式 |
|---|---|---|
| 启动 | 打开应用，或不带参数运行 `oriel` | `oriel serve [-data 目录] [-port 端口]` |
| 数据目录 | 系统的用户数据目录：macOS `~/Library/Application Support/Oriel`，Windows `%AppData%\Oriel`，Linux `~/.config/Oriel` | 同左，`-data` 可改 |
| 监听 | `127.0.0.1` 的随机空闲端口，不要口令 | 默认端口 7470。没设口令只听 `127.0.0.1`；设了口令听所有网卡 |
| 口令 | 不用 | 只从环境变量 `ORIEL_PASSWORD` 读，不接受命令行参数，免得出现在进程列表里 |
| 退出 | 关最后一个窗口或退出菜单；服务端随之关闭 | SIGINT 或 SIGTERM，最多等 10 秒让请求完成 |

- 桌面模式只开一个实例。再次打开时，已有的实例把窗口带到前面。
- 同一个数据目录同时只能有一个进程（见「存储」）。桌面模式打不开数据时，用系统语言弹出原生错误框，然后退出。
- 无窗口模式不启动界面事件循环，不加载 GTK 等界面库，可以跑在没有图形环境的服务器上。
- 版本号只写在 `mygo.json`，打包和未打包的程序都从这里读，`/health` 返回它。

### 连接远程服务端

桌面应用默认读写本机的服务端。应用菜单的「连接服务端…」打开连接界面，填地址和口令，改连另一台电脑上的 `oriel serve`。

- 地址没写协议时补 `http://`；没写协议也没写端口时补端口 7470。
- 先请求 `GET /health`：连不上、不是 Oriel、数据库不可写各有一句提示。不要口令的服务端直接连上；要口令的再 `POST /auth/login`，口令为空或不对时提示在口令框下。
- 连上后，服务端地址和会话 token 写进数据目录的 `remote.json`，只有当前用户能读写（0600）。口令只在登录时发送，不写进任何文件。
- 再次打开应用时，用保存的 token 请求 `GET /api/settings` 检查会话。401 时打开连接界面，写明哪个服务端的登录失效了，地址已填好，焦点在口令框；连不上时也打开连接界面，显示连不上。
- 连接界面的「本机」按钮回到本机服务端，删掉 `remote.json`。
- 网络和磁盘操作都在后台做，界面不卡；连接中再点「连接」不会重复发请求。

## 数据模型

时间都是 Unix 毫秒整数。布尔是 `0` / `1`。

### 源

一条订阅。每个源有一个形态：`article`、`video`、`social`、`image`，没有第五种。条目的形态跟随源，条目表里不存形态，改源的形态，条目跟着变。

| 字段 | 说明 |
|---|---|
| `url` | 订阅地址。RSSHub 源为空串。 |
| `rsshub_path` | RSSHub 路径，以 `/` 开头。普通源为空。抓取时拼上当前的 RSSHub 根地址，换实例只改设置。 |
| `title`、`site_url`、`icon_url` | 展示用。 |
| `kind` | 四种形态之一。 |
| `etag`、`last_modified` | 条件请求用。 |
| `last_fetched_at`、`last_success_at`、`last_error`、`consecutive_failures` | 抓取健康状况。 |
| `fetch_interval_sec` | 默认 900。 |
| `next_fetch_at` | 到期时间。「更新」只把它改成现在，不在请求里抓。 |
| `prompt_summary`、`prompt_translate` | 覆盖全局 prompt，空就用全局的。 |
| `auto_summary`、`auto_translate` | 按源开关，默认关。没有全局开关。 |
| `mute_keywords` | 按行分隔的屏蔽词，见「屏蔽词」。 |
| `paused` | 暂停的源不抓，也不能标到期。 |

`url` 和 `rsshub_path` 合起来唯一，同一订阅不能加两次。

### 条目

源里的一条内容，按 `(source_id, guid)` 去重；没有 guid 时用规范化后的条目 URL。删源时条目一起删。

| 字段 | 说明 |
|---|---|
| `guid`、`title`、`url`、`author`、`published_at` | 来自订阅。`published_at` 只用来展示，会被重新发布覆盖。 |
| `rss_summary`、`rss_content_html` | 订阅里的摘要和正文。 |
| `extracted_html`、`extract_status`、`extract_attempts` | 抽取的正文。状态 `pending` → `ok` / `failed`；非文章形态为 `skipped`。 |
| `thumbnail_url`、`duration_sec`、`platform` | 视频和社交的展示信息。 |
| `images` | JSON 数组，每项 `{ "url", "width"?, "height"? }`，没有图为 `[]`。 |
| `is_read`、`read_at`、`starred` | 阅读状态。收藏的条目不被保留期限清理。 |
| `search_text` | 抽取正文的纯文本，进全文索引。 |

时间线按 `created_at, id` 排序和分页，不用 `published_at`。

### 设置和会话

设置是键值表。API key 存在这里，接口不下发，只回「已配置 / 未配置」。

会话一行一台设备：token 的 SHA-256、User-Agent、创建时间、最近使用时间。不存 token 原文。

## 存储

- SQLite，一个文件 `oriel.sqlite`，WAL，开外键。驱动是纯 Go 的 `modernc.org/sqlite`（见 [ADR 0003](adr/0003-pure-go-sqlite.md)）。
- 全文检索 FTS5，分词 trigram，列为标题、RSS 摘要、`search_text`。由触发器和条目表同步。
- 迁移是内嵌的 SQL 文件，按 `PRAGMA user_version` 逐个执行，每个一个事务。只加新迁移，不改已发布的。
- 同一个数据目录只能有一个 Oriel 进程：打开数据库时对旁边的 `oriel.sqlite.lock` 加系统文件锁，第二个进程直接报错。进程崩溃时系统会释放锁。
- 数据库文件只有当前用户可读写（`0600`），里面有会话和 API key。

## 抓取

抓取循环跑在服务端进程里，两种运行模式都有，没有单独的 worker。

1. 每 15 秒取到期（`next_fetch_at` 已到）且未暂停的源，最多 4 个，同时抓。
2. 带 ETag / Last-Modified 条件请求。超时 25 秒，响应最大 8 MiB，User-Agent `Oriel/<版本>`。
3. RSSHub 源用设置 `rsshub_base_url` 加 `rsshub_path` 拼地址，换实例只改设置。没设根地址时这次抓取记为失败。完整的 RSSHub 地址（例如 `https://rsshub.app/bilibili/hot-search`）按普通源抓，用不到根地址；根地址只给只填路由的订阅用。
4. 解析 RSS、Atom 和 JSON Feed，按 `(source_id, guid)` 写入。相对链接按订阅地址补全，只留 http 和 https 地址。
5. 同一次抓到的条目从最后一条往前写，`created_at` 相同时新的条目 `id` 大，在时间线上排前面。
6. 已有的条目只在内容变了时更新，`updated_at` 也只在这时改；已读和收藏不动。
7. 文章形态的新条目 `extract_status` 为 `pending`，其他形态为 `skipped`。
8. Discourse 论坛帖子里引用别人的 `<aside class="quote">` 去掉，免得回帖把别人的话当成自己的正文。
9. 源的标题和站点地址为空时，用订阅里的补上，不覆盖用户改过的。

成功（含 304）后按 `fetch_interval_sec` 排下一次，清掉失败计数和 `last_error`。失败时 `consecutive_failures` 加一，原因写进 `last_error`：连续失败 `n ≤ 5` 次时 60 秒后再试，之后 `min(60 × 2^(n−5), 600)` 秒。Cloudflare 拦截和 RSSHub 路由 404 有专门的说明。

订阅地址是用户自己填的，可以指向本机或内网（例如自建的 RSSHub），不做 SSRF 检查。订阅内容带出来的地址由正文抽取检查（见下一节）。

抓订阅时认 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量，不读系统代理设置。

## 正文抽取

每次抓取之后，同一个循环再取最多 4 条等待抽取的文章，同时抽：先取从没试过的（新的在前），再取失败但没满 3 次的。源后来改成别的形态时，它的条目不再抽。

1. 下载条目链接的网页，超时 25 秒，最大 8 MiB，按响应头和网页声明的编码解码。
2. Discourse 论坛的帖子页只取链接指向的那一楼（`#post_N` 或 `/t/<slug>/<主题>/<N>`，没有就取一楼），并去掉引用块；其他网页用 Readability 取正文。相对链接按最终地址（跟随重定向之后）补全。
3. 成功时 `extract_status` 为 `ok`，写 `extracted_html` 和它的纯文本 `search_text`（进全文索引）；失败为 `failed`。每次都给 `extract_attempts` 加一，满 3 次不再自动重试。
4. 抽取的结果是派生内容，不改条目的 `updated_at`。

条目链接来自订阅内容，不是用户填的，所以只能访问公网地址：

- 检查发生在建立连接的那一刻，对象是实际要连的 IP，所以 DNS 解析和每一次重定向都绕不过去。重定向最多 5 次，只能到 http 或 https。
- 拒绝回环、私有网段（`10/8`、`172.16/12`、`192.168/16`、`fc00::/7`）、链路本地（含云元数据地址 `169.254.169.254`）、组播、`0.0.0.0/8`、`100.64/10`、`240/4`，IPv4 映射的 IPv6 地址按 IPv4 判断。
- `198.18.0.0/15` 不拦：Clash 等代理的 TUN 模式会把所有域名解析到这个网段。
- 直接连接，不走代理：经过代理时看不到真正的目标地址。

## 保留期限

设置 `retention_days`，单位天：`0` 或不设是永久（默认），最大 3650，不是整数的值按永久处理。

- 抓取循环每小时最多清理一次：删除未收藏且过期的条目，已读未读无关。过期按发布时间算，没有发布时间按写入时间。
- 抓取时，发布时间早于保留期限的新条目直接跳过。否则订阅里还留着的旧条目会在清理后被当成新条目抓回来，以未读身份出现在时间线最上面，一小时后又被删掉，如此反复。已经在库里的条目不受影响，等清理删除。

## 鉴权

- 没设口令时，服务端只听 `127.0.0.1`，不校验。桌面模式默认这样。
- 设了口令时听所有网卡，除 `GET /health` 和 `POST /auth/login` 外都要会话。
- 口令不写进日志和数据库。启动时用 PBKDF2-SHA256（60 万轮、随机盐）派生一次，登录时同样派生后用常数时间比较。
- 登录请求的坏 JSON 返回 401，不是字段校验错误。
- 会话放在 cookie `oriel_session`（HttpOnly，SameSite=Lax，30 天；HTTPS 或带 `X-Forwarded-Proto: https` 时加 Secure），或 `Authorization: Bearer <token>`。空闲满 30 天失效。最近使用时间最多一分钟写一次。
- 每次登录是一条设备会话，可以单独吊销，也可以登出当前会话。

## HTTP 约定

- 只用标准库 `net/http`。JSON 字段用 camelCase。
- 出错时返回 `{ "error": "<代码>" }`，代码是稳定的小写英文，界面据此显示本地化文案。
- `GET /health` 不要鉴权，返回版本、是否要求口令、数据库是否可写。数据库不可写时 503。
- 已有的路由：`POST /auth/login`、`POST /auth/logout`、`GET /api/sessions`（列出未过期的会话，标出当前会话）、`DELETE /api/sessions/{id}`，以及「订阅接口」「时间线接口」「设置接口」「OPML 接口」「图片代理」五节的路由。其余路由随各自的功能加进来，并写进这一节。
- 请求体是 JSON，最大 64 KiB；坏 JSON、多出未知字段或类型不对返回 400 `invalid_body`。

## 订阅接口

都要会话（设了口令时）。源在 JSON 里是一个对象：`id`、`url`、`rsshubPath`、`title`、`siteUrl`、`iconUrl`、`kind`、`fetchIntervalSec`、`nextFetchAt`、`lastFetchedAt`、`lastSuccessAt`（从没有过为 `null`）、`lastError`、`consecutiveFailures`、`promptSummary`、`promptTranslate`、`autoSummary`、`autoTranslate`、`muteKeywords`、`paused`、`unreadCount`、`createdAt`、`updatedAt`。`etag` 和 `last_modified` 只给抓取用，不下发。

| 路由 | 作用 |
|---|---|
| `GET /api/sources` | 列出所有源，按标题（空标题用地址）排序，返回 `{ "sources": [...] }`。 |
| `POST /api/sources` | 添加，返回 201 和新源。新源马上到期，由抓取循环去抓，请求里不抓。 |
| `GET /api/sources/{id}` | 一个源。 |
| `PATCH /api/sources/{id}` | 改字段，只改请求里带的，返回改后的源。 |
| `DELETE /api/sources/{id}` | 删源和它的全部条目，含收藏的，返回 204。 |
| `POST /api/sources/{id}/refresh` | 「更新」：把 `next_fetch_at` 改成现在，返回 204。 |

添加时的字段：`url` 和 `rsshubPath` 恰好给一个，`kind` 必填，`title` 和 `fetchIntervalSec` 可选。标题去掉首尾空白。

- `url` 去掉首尾空白后必须是带主机名的 http 或 https 绝对地址，否则 `invalid_url`。可以指向本机或内网（见「抓取」）。
- `rsshubPath` 去掉首尾空白后必须以 `/` 开头、不以 `//` 开头，否则 `invalid_rsshub_path`。
- `url` 和 `rsshubPath` 都给或都不给时 `invalid_source`。
- `kind` 不是四种形态之一时 `invalid_kind`。
- `fetchIntervalSec` 是 60 到 604800（7 天）的整数，默认 900，否则 `invalid_interval`。
- 同一订阅已经有了，返回 409 `source_exists`。

能改的字段：`title`、`kind`、`fetchIntervalSec`、`promptSummary`、`promptTranslate`、`autoSummary`、`autoTranslate`、`muteKeywords`、`paused`。订阅地址不能改，要换就删了重加。

- 标题改成空串时，下次抓取用订阅里的标题补上。
- 改 `kind` 后条目的形态跟着变（见「数据模型」）；改成别的形态的源不再抽取正文。
- 改 `fetchIntervalSec` 从下一次抓取之后生效；想马上抓就「更新」。
- `muteKeywords` 按行保存，每行去掉首尾空白，丢掉空行。
- 暂停的源「更新」返回 409 `source_paused`。恢复后按原来的 `next_fetch_at` 抓，已经过了就马上抓。
- 源不存在时 404 `not_found`，`id` 不是整数时 400 `invalid_id`。用户改字段时更新源的 `updated_at`；抓取记账不改。

## 时间线接口

都要会话（设了口令时）。条目按 `created_at, id` 从新到旧排，不用 `published_at`（见「数据模型」）。

| 路由 | 作用 |
|---|---|
| `GET /api/entries` | 一页条目，返回 `{ "entries": [...], "nextCursor": "..." }`；没有下一页时 `nextCursor` 为 `null`。 |
| `GET /api/entries/{id}` | 一个条目，多带正文。 |
| `PATCH /api/entries/{id}` | 改 `isRead`、`starred`，只改请求里带的，返回改后的条目。 |
| `POST /api/entries/read` | 「全部已读」，返回 `{ "marked": n }`。 |

列表里的条目：`id`、`sourceId`、`sourceTitle`（源的标题，空就用地址）、`kind`（源的形态）、`title`、`url`、`author`、`publishedAt`、`summary`（订阅里的摘要）、`thumbnailUrl`、`durationSec`、`platform`、`images`、`extractStatus`、`isRead`、`readAt`、`starred`、`createdAt`。没有的时间和时长为 `null`。单个条目再多带 `contentHtml`（订阅里的正文）和 `extractedHtml`（抽取的正文，没有为空串）。

`GET /api/entries` 的查询参数，都可选，可以组合：

- `source`：只看这个源；`kind`：只看这种形态的源。值不对时 `invalid_source`、`invalid_kind`。
- `unread=1`：只看未读；`starred=1`：只看收藏。其他值当没给。
- `q`：全文检索标题、订阅摘要和抽取的正文。去掉首尾空白后按整段文字匹配，不认检索语法，不分大小写。trigram 分词要求至少 3 个字符，短了返回 `query_too_short`。
- `limit`：一页几条，1 到 100，默认 50，否则 `invalid_limit`。
- `cursor`：上一页的 `nextCursor`，取它之后更旧的一页。是不透明的字符串，客户端不要拼；不对时 `invalid_cursor`。

新条目写在最上面，按游标翻页不会重复也不会漏，翻页时新到的条目要从头再取。

`PATCH /api/entries/{id}`：

- `isRead` 改成 `true` 时记下 `read_at`，已经是已读就不变；改成 `false` 时清空 `read_at`。
- `starred` 改成 `false` 后，过了保留期限的条目在下次清理时删掉。
- 不改条目的 `updated_at`：它只记订阅内容的变化。

`POST /api/entries/read` 的请求体可以为空，字段都可选：`sourceId`、`kind`、`upTo`。`upTo` 是条目 `id`，只标时间线上它和比它旧的条目，免得把用户还没看到的新条目一起标掉；条目不存在时 `not_found`。

### 屏蔽词

源的 `muteKeywords` 每行一个屏蔽词。条目的标题或订阅摘要里包含其中任何一个，就算被屏蔽：

- 按子串匹配，不分大小写，不认通配和正则。只看标题和订阅摘要（纯文本），不看正文和抽取的正文。
- 被屏蔽的条目不出现在 `GET /api/entries` 的任何结果里，检索也一样；不计入源的 `unreadCount`；「全部已读」不改它们。
- 收藏的条目不屏蔽：用户特意留下的，不因为后来加了屏蔽词就看不见。
- 屏蔽只是隐藏，不删条目，也不改阅读状态。按 `id` 仍能取到和修改。改了屏蔽词马上生效，清空后条目重新出现。

## 设置接口

都要会话（设了口令时）。设置在 JSON 里是一个对象：`rsshubBaseUrl`（RSSHub 根地址，没设为空串）、`retentionDays`（保留期限的天数，`0` 是永久）。

| 路由 | 作用 |
|---|---|
| `GET /api/settings` | 当前设置。 |
| `PATCH /api/settings` | 改字段，只改请求里带的，返回改后的设置。 |

- `rsshubBaseUrl` 去掉首尾空白，空串表示不设；否则必须是带主机名的 http 或 https 绝对地址，可以指向本机或内网，不然 `invalid_rsshub_base_url`。
- `retentionDays` 是 0 到 3650 的整数，否则 `invalid_retention_days`；不是整数时 `invalid_body`。库里存的值读不成天数时按永久处理，返回 `0`（见「保留期限」）。
- 有一个字段不对就整个请求不生效。
- 抓取循环每次抓取和清理都重新读设置，改了从下一次抓取和清理起生效，不用重启。
- 只下发上面列出的字段。API key 一类的密钥不进任何响应、日志和错误，只回是否已配置；AI 的设置由 AI 接口加进来。

## OPML 接口

都要会话（设了口令时）。

| 路由 | 作用 |
|---|---|
| `POST /api/opml/import` | 请求体是 OPML 文本，逐条添加订阅，返回 `{ "added": n, "existing": n, "failed": [...] }`。 |
| `GET /api/opml/export` | 所有源导出成一个 OPML 2.0 文件，`Content-Type: text/x-opml`，文件名 `oriel.opml`。 |

OPML 没有形态和 RSSHub 路径，Oriel 用 `outline` 上的两个自定义属性保留它们：`orielKind`（四种形态之一）和 `orielRsshubPath`。别的阅读器不认这两个属性，照常读 `xmlUrl`。

导入：

- 有 `xmlUrl` 或 `orielRsshubPath` 的 `outline` 是订阅，其余是分组。分组可以嵌套，逐层往下找；分组本身不导入。
- 有 `orielRsshubPath` 时按 RSSHub 源加，不看 `xmlUrl`；否则按 `xmlUrl` 加。没有 `orielKind` 时形态是 `article`。
- 标题取 `title`，没有就取 `text`，去掉首尾空白；和订阅地址一样时当没给，留给抓取补上。
- 每条按「订阅接口」添加的规则校验，抓取间隔用默认值。已经订阅了的（包括同一文件里重复出现的）计入 `existing`，不算失败。
- 不合规则的条目不影响别的条目，放进 `failed`，每项是 `{ "title", "address", "error" }`，`error` 是添加时的错误码，比如 `invalid_url`、`invalid_kind`。
- 文件最大 1 MiB，超了返回 413 `opml_too_large`；不是 XML、根元素不是 `opml` 时返回 400 `invalid_opml`，什么都不加。

导出：

- 每个源一个 `outline`，不分组，带 `text`、`title`、`type="rss"`、`xmlUrl`、`htmlUrl` 和 `orielKind`。标题为空时 `text` 用地址。
- RSSHub 源另带 `orielRsshubPath`。设了 RSSHub 根地址时，`xmlUrl` 是根地址加路径，别的阅读器能直接用；没设时没有 `xmlUrl`。
- 导出的文件再导入，得到同样的订阅：地址、RSSHub 路径、形态和标题都一样。别的字段（抓取间隔、prompt、屏蔽词、暂停）不导出。

## 图片代理

`GET /api/image?url=<地址>` 替界面取缩略图、视频封面和条目里的图片。要会话（设了口令时）。界面不直接连这些地址：它们来自订阅内容，远程模式下客户端和服务端也不在同一个网络，地址检查只在服务端做一处。

- `url` 必须是带主机名的 http 或 https 绝对地址，否则 400 `invalid_url`。
- 按「正文抽取」的规则只连公网地址，超时 25 秒，重定向最多 5 次，直接连接不走代理。不带 `Referer`。
- 最大 8 MiB。按内容判断类型，只放行界面能解码的 PNG、JPEG、GIF、WebP 和 BMP。
- 连不上、地址被拦、上游不是 2xx、超过大小或类型不对，都返回 502 `image_unavailable`。
- SVG 不放行：它能带脚本，在服务端的源下打开时能读到会话 cookie；界面也解不了它。
- 成功时带判断出的 `Content-Type`、`X-Content-Type-Options: nosniff` 和 `Cache-Control: private, max-age=86400`。服务端不存图片。

## 当前界面

- 连接界面（`internal/ui/connect/`）：见「连接远程服务端」。
- 主界面是时间线（`internal/ui/timeline/`），下面一节。
- 订阅的添加、修改和删除（`internal/ui/subscriptions/`）是时间线上的对话框，见「订阅管理」。

### 时间线

数据都经 HTTP 接口取（「订阅接口」「时间线接口」「图片代理」），界面不碰存储。连本机时用本机服务端，连远程时用当前会话；换了服务端就从头加载。

左栏：

- 上面是五个形态按钮：全部、文章、视频、社交、图片，各带未读数（源的 `unreadCount` 按形态加起来，暂停的源也算）。
- 下面是订阅列表，按 `GET /api/sources` 的顺序：形态色的首字母图标、标题、未读数；上次抓取失败（`lastError` 不为空）的带警告图标，鼠标悬停显示「上次抓取失败」；暂停的标题变淡，带暂停图标，不显示未读数。标题为空（还没抓过）时显示地址的主机名，RSSHub 源显示路由。
- 选一个形态只看这种形态的条目，下面的订阅列表也只列这种形态的源；选一个订阅只看它，它所属形态的按钮保持高亮；再选「全部」回到混合时间线。
- 最下面是服务端栏：本机显示「本机」，远程显示主机名，点了打开连接界面（见「连接远程服务端」）。
- 「订阅」标题右边是添加按钮，没有订阅时也在。右键一个订阅弹出菜单：更新、编辑…、暂停或恢复、删除…（见「订阅管理」）。

工具栏：当前范围的标题和未读数，选了一个订阅时标题后面有编辑按钮；「全部 / 未读 / 收藏」三段（对应 `unread=1`、`starred=1`）；检索框；「全部标为已读」。

- 检索框停止输入 300 毫秒后检索，回车立即检索，Esc 清空。去掉首尾空白后不到 3 个字时不发请求，在工具栏下面提示「至少输入 3 个字才能搜索」，列表保持原样。检索时标题旁显示结果数（已加载的条数，还有下一页时带「+」）。
- 「全部标为已读」只在「全部」和「未读」下、没有检索、当前范围有未读时可用。先弹确认窗口写明范围和条数，确认后发 `POST /api/entries/read`，`upTo` 是已加载的最上面那条，打开列表之后新到的条目仍是未读。

列表：

- 每页 50 条，按游标翻页，滚到离底部不到 10 条时取下一页；取失败时底部显示重试。
- 「全部」「文章」「社交」和单个订阅用一列，每行按条目的形态画：文章是标题、来源和时间、两行摘要，有缩略图时放右边；视频是 16:9 封面加时长；社交是作者、正文（最多 6 行）和最多 4 张图的网格，多出的在最后一张上写「+N」；图片是一张 3:2 的大图。
- 「视频」用封面网格，「图片」用瀑布流：按图片的宽高放进当前最矮的一列，没有宽高时按 4:3。
- 未读的条目左边有一个强调色圆点，标题加粗；已读的标题变淡。
- 时间显示为相对时间（「5 分钟前」「3 小时前」「昨天」「3 天前」），一周以前显示日期，不是今年时带年份。用 `publishedAt`，没有就用 `createdAt`。
- 图片经图片代理取，在后台解码，同时最多取 4 张；内存里缓存最近解码过的图，取失败的显示占位底色，不再重试，直到重新加载。

打开条目：点一行（或回车）同时标为已读；文章在列表的位置打开阅读器（见「阅读器」），视频、社交和图片用系统浏览器打开条目链接。行上悬停或选中时右上角显示收藏、已读/未读切换和在浏览器中打开三个按钮，文章也可以这样直接去浏览器。改状态先改界面，请求失败时改回来并提示。

新条目：每 60 秒在后台重新取一次订阅列表和当前范围的第一页。有比列表最上面那条更新的条目时，列表不动，顶部出现「N 条新内容」，点了重新加载并回到顶部；N 只数第一页里的，最多 50。检索时不检查新条目。

空状态：没有订阅时提示「还没有订阅」和「添加第一个订阅」按钮；「未读」下没有条目时显示「都读完了」；检索没有结果时显示「没有找到“…”」；第一页取不到时显示「时间线加载不出来」和重试按钮。会话失效（401）时打开连接界面。

键盘：

- ↑ ↓ 或 J K 在条目间移动，回车或 O 打开，S 收藏或取消，M 切换已读，`/` 把焦点移到检索框，Cmd/Ctrl+N 添加订阅。对话框开着时这些键不起作用。阅读器开着时用阅读器的键（见「阅读器」）。
- 每一行对辅助技术读作「来源、标题、时间、未读或已读、收藏」；收藏、标为已读、全部标为已读、加载完和出错都用播报告知。

### 阅读器

文章在列表的位置打开，左栏留着；经 `GET /api/entries/{id}` 取正文，界面不碰存储。

- 工具栏：左边「回到〈范围〉」；右边上一篇、下一篇、收藏、已读/未读切换、在浏览器中打开。上一篇、下一篇只在当前列表里的文章之间走，跳过别的形态；下一篇走到已加载的最后一篇时先取下一页。正文还没取到或取不到时，收藏、已读切换和在浏览器中打开不可用。
- 页面：来源和时间（完整日期加时刻，不是今年时带年份），标题，再是正文，最宽 680。正文开头和标题相同的标题行去掉。
- 打开时标为已读，已读切换可以撤销。点左栏、换「全部 / 未读 / 收藏」或检索都会关上阅读器。

状态，按 `extractStatus` 和带不带正文分：

- 全文：`ok` 且抽出的正文不为空，只显示正文。
- 只有 RSS 正文：抽取失败（或这个条目不抽），显示订阅里带的正文（没有正文就用摘要），上面一条警告色提示「这不是全文」，带「打开原文」链接。
- 正在取全文：`pending` 时先显示 RSS 正文，提示正在取全文；连 RSS 正文也没有时显示「正在取全文」和「打开原文」按钮。每 5 秒重新取一次，`ok` 了就换成全文并播报「全文已载入」；中途取失败保持原样，不报错。
- 抽取失败也没有 RSS 正文：显示「没有可显示的正文」和「打开原文」按钮。
- 取不到条目：显示「这篇加载不出来」和重试按钮。会话失效（401）时打开连接界面。

正文在客户端从 HTML 画成原生视图，不用 WebView：

- 画：标题（h1、h2 一级，h3 到 h6 一级）、段落、换行、有序和无序列表（含嵌套和 `start`）、引用块、代码块（保留换行，横向滚动）、行内代码、粗体、斜体、删除线、链接、图片和图注、分隔线；表格每行一段，单元格之间用「·」隔开。
- 不画：脚本、样式、表单、iframe、object、embed、svg、math、视频、音频、canvas。
- 链接：相对地址按条目链接补全；只留 http、https 和 mailto，页内锚点和 `javascript:` 只留文字。点了用系统浏览器打开。
- 图片：`src` 为空或是 data URI 时用 `data-src`；宽或高不超过 2 像素的（计数用的像素图）去掉。经图片代理取，在后台解码，按正文顺序同时最多取 4 张；按 `width`、`height` 留出位置，没有时按图片本身，取到之前按 16:9；取失败的不显示。

键盘：J K 上一篇、下一篇，S 收藏，M 切换已读，O 在浏览器中打开，Esc 回到列表，焦点回到那一行。焦点在正文上，方向键、空格、PageUp/PageDown、Home/End 滚动；Tab 依次经过正文里的链接。正文可以选中复制。播报「全文已载入」和出错。

### 订阅管理

都经「订阅接口」和「设置接口」，界面不碰存储。

添加：侧栏的添加按钮、没有订阅时的「添加第一个订阅」和 Cmd/Ctrl+N 打开添加对话框。字段是地址、形态、标题（可空，空就用订阅里的）和检查间隔（15 分钟、30 分钟、1 小时、3 小时、6 小时、12 小时、1 天，默认 15 分钟）。

- 地址框边输入边识别，在框下面说清会订阅什么（平台和对象，加上实际的订阅地址或路由），发请求之前就能看到；服务端照样再查一遍。识别出的平台带出形态，用户自己选过形态之后就不再改。
- 以 `/` 开头、不以 `//` 开头的是 RSSHub 路由。以设置里的 RSSHub 根地址开头的完整地址存成路由（去掉根地址），以后换实例只改设置。
- 别的必须是带主机名的 `http://` 或 `https://` 地址；不是时点「添加」才在框下面报错，把焦点放回地址框，改了地址错误就消失。
- 要存成路由、而设置里还没有 RSSHub 根地址时，框下面提示先填根地址，多一个根地址的输入框。点「添加」先存设置，再加订阅；根地址不对时报在根地址框下面。
- 已经订阅过（409 `source_exists`）时提示「已经订阅过了，叫“…”」，带「去看看」，点了关掉对话框、选中那个订阅。连不上服务端时在对话框里提示，不关对话框。
- 加好后关掉对话框，订阅出现在侧栏，提示「已添加“…”」。条目由抓取循环去抓，随时间线的定时检查出现。

平台链接的识别（主机名去掉 `www.` 和 `m.`）：

| 填的地址 | 订阅 | 形态 |
|---|---|---|
| `youtube.com/channel/<id>` | `https://www.youtube.com/feeds/videos.xml?channel_id=<id>` | `video` |
| `youtube.com/@<handle>` | 路由 `/youtube/user/@<handle>` | `video` |
| `youtube.com/playlist?list=<id>` | `https://www.youtube.com/feeds/videos.xml?playlist_id=<id>` | `video` |
| `space.bilibili.com/<uid>` | 路由 `/bilibili/user/video/<uid>` | `video` |
| `x.com/<name>`、`twitter.com/<name>` | 路由 `/twitter/user/<name>` | `social` |
| `github.com/<owner>/<repo>` | `https://github.com/<owner>/<repo>/releases.atom` | `article` |
| `github.com/<owner>` | `https://github.com/<owner>.atom` | `article` |
| `t.me/<name>`、`t.me/s/<name>` | 路由 `/telegram/channel/<name>` | `social` |
| `weibo.com/u/<uid>`、`weibo.com/<uid>`（数字） | 路由 `/weibo/user/<uid>` | `social` |

X 的 `home`、`i`、`search`、`explore`、`settings`、`messages`、`notifications` 等站内页面不算用户名。路径以 `.atom`、`.rss`、`.xml` 结尾的本来就是订阅，不再转换。别的地址原样当订阅地址。

修改：右键菜单的「编辑…」或工具栏的编辑按钮打开编辑对话框。最上面一行是抓取状况：「5 分钟前检查过 · 10 分钟后再查」，还没抓过时「还没检查过」，失败时「连续失败 N 次：原因」，暂停时「已暂停，恢复之前不抓取」，旁边是「更新」（暂停时没有）。下面是标题、检查间隔、形态、暂停开关和屏蔽词（每行一个）。「保存」只发改了的字段。订阅地址不能改。按源的摘要和翻译 prompt、自动开关留到 AI 那一版。

暂停和恢复：右键菜单或编辑对话框的开关，发 `PATCH` 改 `paused`。

更新：右键菜单或编辑对话框的「更新」，发 `POST /api/sources/{id}/refresh`，提示「已排进抓取」；新条目随定时检查出现。

删除：右键菜单或编辑对话框的「删除…」先弹确认窗口：「删除“…”？它的条目会一起删掉，收藏的也是。删了不能恢复。」确认后发 `DELETE`，从侧栏去掉；正看着它时回到「全部」。

出错：请求失败时提示原因，对话框不关；会话失效（401）时打开连接界面。

窗口默认 960×640，最小 640×480，用 Oriel 自己的配色（暖灰和青绿，见 `design/tokens.md`），图标用 Lucide。

## 更新

用 MyGo 的原生更新插件。基线没有配置更新源和公钥，所以不检查更新。配好以后，只从公开 HTTPS 源拉签名过的更新，安装前弹原生确认窗口。

## 技术基线

Go 1.27.1，MyGo v0.3.6，SQLite 用纯 Go 的 `modernc.org/sqlite`，生产代码不用 cgo，没有前端工具链。macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 程序需要 glibc（MyGo 动态加载系统库），无窗口模式也一样：可以跑在不带 GTK 的 Debian 等系统上，但不能跑在 Alpine 等 musl 系统上。界面只有简体中文和英文：中文系统（含繁体）显示简体中文，其他系统显示英文。没在真机上测过的性能不写进文档。
