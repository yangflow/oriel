# 产品

## 定位

Oriel 是单用户的混合信息流阅读器：订阅原生 RSS 和 RSSHub，一条时间线混排文章、视频、社交和图片，摘要和翻译按需走用户自己的 OpenAI 兼容接口。它用 Go 和 MyGo 原生 UI，不用 WebView。

## 范围

计划的范围，按 [roadmap](roadmap.md) 的顺序逐项落地：

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
| `mute_keywords` | 按行分隔的屏蔽词。 |
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
3. RSSHub 源用设置 `rsshub_base_url` 加 `rsshub_path` 拼地址，换实例只改设置。没设根地址时这次抓取记为失败。
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
- 已有的路由：`POST /auth/login`、`POST /auth/logout`、`GET /api/sessions`（列出未过期的会话，标出当前会话）、`DELETE /api/sessions/{id}`。其余路由随各自的功能加进来，并写进这一节。

## 当前界面

只有一个 Example 界面，是项目初始化时带的计数示例：加、减、重置按钮和上下方向键改计数，计数为 0 时重置不可用，计数只在内存里，关掉最后一个窗口就退出。它用来验证工具链、多语言和无头测试，产品第一个界面落地时删掉。

## 更新

用 MyGo 的原生更新插件。基线没有配置更新源和公钥，所以不检查更新。配好以后，只从公开 HTTPS 源拉签名过的更新，安装前弹原生确认窗口。

## 技术基线

Go 1.27.1，MyGo v0.3.6，SQLite 用纯 Go 的 `modernc.org/sqlite`，生产代码不用 cgo，没有前端工具链。macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 程序需要 glibc（MyGo 动态加载系统库），无窗口模式也一样：可以跑在不带 GTK 的 Debian 等系统上，但不能跑在 Alpine 等 musl 系统上。界面只有简体中文和英文：中文系统（含繁体）显示简体中文，其他系统显示英文。没在真机上测过的性能不写进文档。
