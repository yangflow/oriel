# 接口

HTTP 约定和订阅、时间线、设置、图片代理、活动和存储的接口。索引见 [docs/product.md](../product.md)。

## HTTP 约定

- 只用标准库 `net/http`。JSON 字段用 camelCase。
- 出错时返回 `{ "error": "<代码>" }`，代码是稳定的小写英文，界面据此显示本地化文案。
- `GET /health` 不要鉴权，返回版本、是否要求口令、数据库是否可用。每次请求只做一次读（`SELECT 1`），不拿写锁；写入能力的探测结果缓存 10 秒，期间的请求共用同一次探测，所以轮询不会和抓取、标已读抢写锁。探测拿不到写锁（忙或超时，最多等 5 秒）算可写，返回 200；只有真正的写失败（只读文件、磁盘错误）和读不了才 503，`database` 为 `unwritable`。还没有任何探测结果时（刚启动），同时到达的请求等第一次探测结束；有了结果之后，探测进行中的请求不等，直接返回上一次的结果。
- 已有的路由：`POST /auth/login`、`POST /auth/logout`、`GET /api/sessions`（列出未过期的会话，带 `name`，标出当前会话）、`PATCH /api/sessions/{id}`（改设备名：`{ "name": "…" }`，去掉首尾空白后最多 64 个字符，不能有控制字符，否则 400 `invalid_device_name`；空字符串清掉名字，回到从 User-Agent 推出的名字；成功 204，不存在 404）、`DELETE /api/sessions/{id}`，以及「订阅接口」「时间线接口」「规则接口」「设置接口」「OPML 接口」「备份与恢复」「导入其他阅读器接口」「AI 摘要和翻译」「图片代理」「活动和存储接口」「诊断文件」各节的路由，和 `GET|POST /websub/{key}`（WebSub 回调，不要会话，见[「WebSub 推送」](fetching.md#websub-推送)）。新路由写进它所属的那一节。
- 请求体是 JSON，最大 64 KiB；坏 JSON、多出未知字段或类型不对返回 400 `invalid_body`。

## 订阅接口

都要会话（设了口令时）。源在 JSON 里是一个对象：`id`、`url`、`rsshubPath`、`title`、`siteUrl`、`iconUrl`、`kind`、`fullText`（是否抓取全文）、`fetchIntervalSec`（现在用的间隔，自动时是算出来的）、`fetchIntervalAuto`、`suggestedKind`（没有建议或已经等于 `kind` 时为空串）、`nextFetchAt`、`lastFetchedAt`、`lastSuccessAt`（从没有过为 `null`）、`lastError`、`lastErrorKind`（上次失败的稳定代码，`blocked`、`gone` 或空串，见[「抓取」](fetching.md#抓取)）、`movedTo`（站点永久搬走的新地址，等用户决定；没有建议为空串，见[「抓取」](fetching.md#抓取)的「永久重定向」）、`consecutiveFailures`、`promptSummary`、`promptTranslate`、`autoSummary`、`autoTranslate`、`muteKeywords`、`paused`、`keepDays`（`0` 跟随全局，`-1` 永久，否则是天数）、`paceSec`（`0` 是关，否则是秒数，见[「节奏」](timeline.md#节奏)）、`hideFromAll`（布尔，见[「不在全部里出现」](timeline.md#不在全部里出现)）、`dailyCap`（`0` 是关，见[「每日上限」](timeline.md#每日上限)）、`folderId`（所在文件夹的编号，没有为 `null`，见[「文件夹」](data.md#文件夹)）、`translateTitles`（布尔，见[「AI 摘要和翻译」](ai.md#ai-摘要和翻译)的「标题翻译」）、`extractSelector`、`extractReferer`、`extractRewrites`（按源的抽取规则，都是字符串，空串是不用，见[「正文抽取」](fetching.md#正文抽取)）、`rsshubInstance`（上次成功抓取的 RSSHub 实例的根地址，没有为空串）、`fetchUserAgent`、`fetchReferer`（按源的抓取请求头，空串是不改，见[「抓取」](fetching.md#抓取)的「自定义请求头」）、`openTarget`（`follow`、`reader` 或 `browser`，见[「当前界面」](screens.md#当前界面)的「时间线」里的「点开时」）、`proxy`（`direct`、代理地址，或空串即跟随全局，见[「抓取」](fetching.md#抓取)的「代理」）、`proxyAuthSet`（这个源是否存了代理的用户名和密码；用户名和密码不下发）、`websubHub`（声明的 hub，没有为空串）、`websubActive`（是否在「推送中」）、`unreadCount`、`createdAt`、`updatedAt`。`etag` 和 `last_modified` 只给抓取用，不下发。

| 路由 | 作用 |
|---|---|
| `GET /api/sources` | 列出所有源，按标题（空标题用地址）排序，返回 `{ "sources": [...], "unreadTotal": n }`。`unreadTotal` 是时间线「未读」里的行数，链接相同的条目只算一次（见[「同一篇文章只显示一次」](data.md#同一篇文章只显示一次)）。 |
| `POST /api/sources` | 添加，返回 201 和新源。可带 `folderId` 直接放进文件夹，文件夹不存在时 400 `invalid_folder`。新源马上到期，由抓取循环去抓，请求里不抓。 |
| `GET /api/sources/{id}` | 一个源。 |
| `PATCH /api/sources/{id}` | 改字段，只改请求里带的，返回改后的源。 |
| `DELETE /api/sources/{id}` | 软删除（见[「删除订阅与撤销」](data.md#删除订阅与撤销)）：源和它的条目立刻从所有地方消失，返回 204；撤销窗口内还能恢复，过后真正删掉。已经删除的源再删是 404。 |
| `POST /api/sources/{id}/restore` | 撤销删除：撤销窗口内把源连同条目和设置原样带回，返回 200 和这个源。源不在了（过了窗口、已被清掉）或没有被删除是 404 `not_found`。 |
| `GET /api/folders` | 列出所有文件夹，按名称不分大小写排序，返回 `{ "folders": [{ "id", "name", "sourceCount" }] }`；`sourceCount` 是里面没被删除的订阅数。 |
| `POST /api/folders` | 新建，请求体 `{ "name": ... }`，返回 201 和 `{ "id", "name", "sourceCount" }`。名称去掉首尾空白后为空或超过 60 个字符时 400 `invalid_name`；已有同名（不分大小写）的 409 `folder_exists`，带 `folderId`。 |
| `PATCH /api/folders/{id}` | 重命名，规则和错误同上，返回改后的文件夹；不存在 404 `not_found`。 |
| `DELETE /api/folders/{id}` | 删除，里面的订阅变成没有文件夹，返回 204；不存在 404 `not_found`。 |
| `POST /api/sources/{id}/refresh` | 「更新」：把 `next_fetch_at` 改成现在，返回 204。 |
| `POST /api/sources/refresh` | 「全部更新」：把所有未暂停的源的 `next_fetch_at` 一次改成现在，返回 200 和 `{ "queued": N, "at": T }`，`N` 是改了的源数（没有未暂停的源时为 0），`T` 是服务端此刻的毫秒时间戳。不在请求里抓，也不另开线程：这些源和别的到期的源一样由抓取循环去抓，最久没抓的先取，仍是 5 个工作线程和每主机的限制（见[「抓取」](fetching.md#抓取)），所以几百个源要几分钟抓完，不会同时发出几百个请求。 |
| `GET /api/discover?url=<地址>` | 订阅发现（见[「订阅发现」](fetching.md#订阅发现)）：返回 `{ "candidates": [...] }`，没找到为空数组。每项：`url`（订阅地址，路由候选为空串）、`rsshubPath`（RSSHub 路由，普通订阅为空串）、`title`（页面或订阅给的标题，可为空）、`kind`（建议的形态，可为空）、`origin`（`rule`、`feed`、`alternate`、`common`）、`subscribedId`（已经订阅了同一个订阅的那个订阅的编号，按「源」一节的规则比较，没有为 `0`）；`origin` 为 `rule` 时还有 `site`（`youtube`、`bilibili`、`weibo`、`zhihu`、`x`）、`label`（`videos`、`dynamic`、`posts`、`activities`、`answers`、`column`、`topic`、`collection`、`question`、`channel`、`playlist`）和 `subject`（用户名、编号或话题名），界面据此按语言拼出名字。不写库。 |
| `GET /api/routes?q=<链接或名字>&refresh=1` | RSSHub 路由查找（见[「RSSHub 路由查找」](fetching.md#rsshub-路由查找)）：返回 `{ "available": 目录是否读得出, "instance": 读目录的实例根地址（没有为空串）, "total": 匹配总数, "routes": [...] }`，`available` 为 `false` 或没有匹配时 `routes` 为空数组。每项：`path`（路由模板，例如 `/github/issue/:user/:repo`）、`name`、`namespace`（命名空间的键）、`namespaceName`、`example`（可为空）、`parameters`（按出现顺序的 `{ name, description, optional }`）和 `values`（从链接取到的参数值，可为空对象）。`refresh=1` 重读目录。`q` 为空时 400 `invalid_query`。不写库。 |
| `GET /api/routes/preview?path=<路由>` | 预览一条填好参数的路由：返回 `{ "instance": 答了的实例根地址, "title": 订阅标题, "entries": [{ "title", "url", "publishedAt" }] }`，最多 5 条，`publishedAt` 是毫秒时间戳或 `null`。`path` 不以 `/` 开头或以 `//` 开头时 400 `invalid_rsshub_path`；没设实例 422 `no_rsshub_instance`；实例答 404 时 502 `route_not_found`；连不上或别的状态码 502 `route_unavailable`；答的内容不是订阅 502 `not_a_feed`。不写库。 |
| `GET /api/feeds/preview?url=<地址>` | 预览一个普通订阅地址，只读不存：经 `guardedClient` 抓一次（只连公网地址，超时和大小限制同「订阅发现」），返回 `{ "url": 最终地址, "title": 订阅标题, "format": 格式和版本（如 `RSS 2.0`、`Atom`、`JSON Feed`）, "updatedAt": 最新条目的毫秒时间戳或 `null`, "perDay": 按订阅里的日期估算的每天条数或 `null`, "entries": [{ "title", "url", "publishedAt" }] }`，`entries` 最多 3 条有标题的条目，条目都有日期时从新到旧。地址不是带主机名的 http 或 https 时 400 `invalid_url`；不是公网地址时 422 `address_not_public`；取不到 502 `page_unavailable`；内容不是订阅 502 `not_a_feed`。 |
| `POST /api/sources/{id}/move` | 接受搬家建议：把订阅地址改成 `movedTo`，返回改后的源。请求体 `{ "url": ... }` 必须等于现在的 `movedTo`，否则 409 `move_changed`；没有建议时也是 `move_changed`；新地址已是另一个订阅时 409 `source_exists`（带 `sourceId`）。见[「抓取」](fetching.md#抓取)的「永久重定向」。 |
| `POST /api/sources/{id}/dismiss-move` | 忽略搬家建议：返回改后的源（`movedTo` 为空串）。请求体和错误同上，除了没有 `source_exists`。 |
| `GET /api/sources/health` | 订阅健康（见[「订阅健康」](data.md#订阅健康)）：只返回有问题的源，不带别的源。按需请求，不轮询，也不参与 `GET /api/sources` 的刷新。 |
| `POST /api/sources/{id}/noisy/dismiss` | 忽略「条目太多」的建议，见[「订阅健康」](data.md#订阅健康)：成功 204；源现在不是 `noisy` 时 409 `not_noisy`；源不存在 404 `not_found`。 |

`GET /api/sources/health` 返回 `{ "windowDays": 30, "sources": [...] }`。每项：`id`、`url`、`rsshubPath`、`title`、`iconUrl`、`kind`、`state`（`dead`、`failing`、`moved`、`noisy`、`rarelyRead`）、`cause`（`state` 为 `dead` 时是 `failing` 或 `silent`，其余为空串）、`days`（见[「订阅健康」](data.md#订阅健康)）、`consecutiveFailures`、`lastError`、`movedTo`（只在 `state` 为 `moved` 时有，其余为空串），以及窗口内的 `recent`（写入条数）、`recentRead`、`recentStarred`，还有 `perDay`（`state` 为 `noisy` 时每天的条数，其余为 `0`）、`paceSec` 和 `dailyCap`（源现在的设置，界面用来决定显示哪些按钮）。顺序：已失效、总在失败、已搬家、条目太多、很少读；同一档里按标题（空标题用地址）不分大小写排序。没有问题时 `sources` 是空数组。

添加时的字段：`url` 和 `rsshubPath` 恰好给一个，`kind` 必填，`title`、`fetchIntervalSec`、`fetchIntervalAuto` 和 `allowDuplicate` 可选。标题去掉首尾空白。

- `url` 去掉首尾空白后必须是带主机名的 http 或 https 绝对地址，否则 `invalid_url`。可以指向本机或内网（见[「抓取」](fetching.md#抓取)）。
- `rsshubPath` 去掉首尾空白后必须以 `/` 开头、不以 `//` 开头，否则 `invalid_rsshub_path`。
- `url` 和 `rsshubPath` 都给或都不给时 `invalid_source`。
- `kind` 不是五种形态之一时 `invalid_kind`。
- `fetchIntervalSec` 是 60 到 604800（7 天）的整数，否则 `invalid_interval`。给了它就是固定间隔；`fetchIntervalAuto` 是布尔值，缺省为 `true`，即两个都不给时间隔「自动」，起点 900。`fetchIntervalAuto` 为 `true` 又给了 `fetchIntervalSec`，返回 `invalid_interval`。
- 同一订阅已经有了（订阅地址按「源」一节规范化后比较：末尾多一个 `/`、主机名大小写、默认端口、`http` 对 `https`、`www.`、跟踪参数、查询参数的顺序不同，或同一个上游走了 RSSHub 路由和普通地址两条路，都算已有），返回 409，`{ "error": "source_exists", "sourceId": 已有的那个订阅的编号 }`；有几个重复时取最早的。`allowDuplicate` 为 `true` 时不做这个比较，照样添加（用户看了提示仍要加第二个）；写法完全相同的地址仍然加不了，也是 409 `source_exists`。

能改的字段：`title`、`kind`、`fetchIntervalSec`、`fetchIntervalAuto`、`fullText`、`promptSummary`、`promptTranslate`、`autoSummary`、`autoTranslate`、`muteKeywords`、`paused`、`keepDays`、`paceSec`、`hideFromAll`、`dailyCap`、`folderId`、`translateTitles`、`extractSelector`、`extractReferer`、`extractRewrites`、`fetchUserAgent`、`fetchReferer`、`openTarget`、`proxy`、`proxyUser`、`proxyPassword`。订阅地址不能用 `PATCH` 改：换地址只有一条路，接受服务端的搬家建议（`POST /api/sources/{id}/move`，见[「抓取」](fetching.md#抓取)的「永久重定向」）；别的换地址要删了重加。

- `keepDays` 是 -1 到 3650 的整数，否则 `invalid_keep_days`。
- `paceSec` 是 `0` 或 3600 到 2592000 的整数，否则 `invalid_pace`。
- `openTarget` 是 `follow`、`reader`、`browser` 之一，否则 400 `invalid_open_target`；不改就不带，新订阅是 `follow`。
- `hideFromAll` 是布尔值，其他类型当作请求体错误（和 `paused` 一样）。
- `dailyCap` 是 `0` 或 1 到 1000 的整数，否则 `invalid_daily_cap`；改了它就对这个源重新算折叠，见[「每日上限」](timeline.md#每日上限)。
- `folderId` 是 `0`（移出文件夹）或已有文件夹的编号，否则 400 `invalid_folder`；移进、移出、换文件夹都是这一个字段，一个订阅最多在一个文件夹里。
- `translateTitles` 是布尔值，其他类型当作请求体错误；和 `autoSummary` 一样，没配置好 AI 时也能改，只是抓取不排队。改成 `false` 时，这个源已有的译文和排队中的标题一并清掉（`translated_title` 清空，`title_tr_status` 改回 `''`）。
- `extractSelector` 去掉首尾空白后最长 500 个字符，且是能解析的 CSS 选择器，否则 `invalid_extract_selector`；空串是不用。
- `extractReferer` 去掉首尾空白后是空串，或带主机名的 http 或 https 绝对地址、最长 2000 个字符，否则 `invalid_extract_referer`。
- `fetchUserAgent`、`fetchReferer` 的规则和错误码见[「抓取」](fetching.md#抓取)的「自定义请求头」（`invalid_fetch_user_agent`、`invalid_fetch_referer`、只填路由的源是 `fetch_headers_unsupported`）；空串清除。
- `extractRewrites` 按行保存，每行去掉首尾空白，丢掉空行。最多 10 条，每条最长 500 个字符，写法是「正文抽取」里的两种，否则 `invalid_extract_rewrites`。
- `proxy` 去掉首尾空白后是空串（跟随全局）、`direct`，或带主机名和端口的代理地址（`http`、`https`、`socks5`、`socks5h`，不带路径、查询串、片段和用户信息，最长 500 个字符），否则 `invalid_proxy`。`proxyUser`、`proxyPassword`（各最长 255 个字节，SOCKS5 的上限，超出 `invalid_proxy`）只能写：不带就不动，空串删掉；`proxy` 改了，这个源存的用户名和密码一并删掉，除非同一个请求带了新的；用户名和密码不管 `proxy` 是什么值都能设。从下一次抓取起生效。
- 标题改成空串时，下次抓取用订阅里的标题补上。
- 改 `kind` 后条目的形态跟着变（见[「数据模型」](data.md#数据模型)）；改成别的形态的源不再抽取正文。
- 改 `fetchIntervalSec` 从下一次抓取之后生效，同时把间隔改成固定（`fetchIntervalAuto` 变 `false`）；想马上抓就「更新」。`fetchIntervalAuto` 改成 `true` 后，现在用的间隔留着，下一次成功抓取时重新算；同时给 `fetchIntervalSec` 和 `true` 返回 `invalid_interval`。
- `muteKeywords` 按行保存，每行去掉首尾空白，丢掉空行。
- 暂停的源「更新」返回 409 `source_paused`；「全部更新」跳过暂停的源，不报错。恢复后按原来的 `next_fetch_at` 抓，已经过了就马上抓。
- 「全部更新」没有进度接口。想知道抓到哪了，就用 `GET /api/sources`：源的 `lastFetchedAt` 不早于 `at`，就是在这次之后抓过了（成功和失败都记 `lastFetchedAt`），其中 `lastError` 不空的是失败的。
- 源不存在时 404 `not_found`，`id` 不是整数时 400 `invalid_id`。用户改字段时更新源的 `updated_at`；抓取记账不改。

## 时间线接口

都要会话（设了口令时）。条目按 `created_at, id` 从新到旧排，不用 `published_at`（见[「数据模型」](data.md#数据模型)）。

| 路由 | 作用 |
|---|---|
| `GET /api/entries` | 一页条目，返回 `{ "entries": [...], "nextCursor": "..." }`；没有下一页时 `nextCursor` 为 `null`。 |
| `GET /api/entries/{id}` | 一个条目，多带正文。 |
| `GET /api/entries/{id}/revision` | 条目更新前后的标题和段落，见[「条目的修订」](data.md#条目的修订)；没有修订 404 `not_found`。 |
| `PATCH /api/entries/{id}` | 改 `isRead`、`starred`、`readProgress`、`remindAt`，只改请求里带的，返回改后的条目。 |
| `POST /api/entries/{id}/extract` | 「重试」取全文：只对抽取失败（`failed`）的文章条目，把 `extract_attempts` 清零、`extract_status` 改回 `pending`，返回 202 和改后的条目（同 `GET /api/entries/{id}`），抽取在下一轮循环里做。已经是 `pending` 时不变，同样返回 202；条目不存在 404 `not_found`；已是 `ok`，或不是文章形态（`skipped`）时 409 `not_retryable`，什么都不改。 |
| `POST /api/entries/read` | 「全部已读」「把上面的标为已读」「把下面的标为已读」「把订阅标为已读」「把可见的标为已读」，可以带 `since` 只标时间范围内的，返回 `{ "marked": n, "ids": [...], "readAt": 毫秒 }`。 |
| `POST /api/entries/unread` | 撤销上面几种已读（含「把更早的标为已读」和 V）：请求体 `{ "ids": [...], "readAt": 毫秒 }`，一个事务里把 `ids` 里仍已读的条目标回未读（`read_at` 清空），返回 `{ "marked": n, "ids": [...] }`，`ids` 是真正改回未读的那些。`readAt` 可选，给了就只改 `read_at` 正好等于它的条目，也就是那次批量标记标的、之后没被别的动作再标过的；这样撤销时，期间被单独改过（比如改成未读又读了一遍）的条目不动。没给 `readAt` 时不做这个限制。`ids` 为空、超过 10000 个或 `readAt` 为负数时 400 `invalid_ids`。 |

列表里的条目：`id`、`sourceId`、`sourceTitle`（源的标题，空就用地址）、`kind`（源的形态）、`title`、`url`、`author`、`publishedAt`、`summary`（订阅里的摘要）、`aiSummary`（见下）、`group`（见[「同一篇文章只显示一次」](data.md#同一篇文章只显示一次)，成员不止一个时才有）、`thumbnailUrl`、`durationSec`、`platform`、`images`、`language`（没有为空串）、`translatedTitle`（标题的译文，只在有译文时出现，见[「标题翻译」](ai.md#标题翻译)；`title` 始终是订阅里的原文）、`readingSec`（没有为 `null`）、`readProgress`（从没滚动过时不出现）、`remindAt`（有提醒时才出现，Unix 毫秒）、`folded`（只在被折叠的条目上出现，值为 `true`，见[「每日上限」](timeline.md#每日上限)）、`extractStatus`、`isRead`、`readAt`、`starred`、`createdAt`、`revisedAt`（条目更新过时才有，见[「条目的修订」](data.md#条目的修订)），源有节奏、条目未读且未收藏时再有 `fadesAt` 和 `expiresAt`（Unix 毫秒，见[「节奏」](timeline.md#节奏)：渐隐从 `fadesAt` 开始，`expiresAt` 离开「全部」和「未读」），否则这两个字段不出现。没有的时间和时长为 `null`。单个条目再多带 `contentHtml`（订阅里的正文）和 `extractedHtml`（抽取的正文，没有为空串）。

`aiSummary` 是这个条目已存的 AI 摘要，给时间线的行代替订阅里的摘要显示：只在摘要任务的结果是 `ok` 且有文字时出现，否则整个字段不出现。它读的是「AI 结果」里存着的那一行，列表从不触发生成，也不调 AI 接口；阅读器显示的是同一份。为了不让列表变大，文字压平空白后最多 300 个字符，超出时在词界（没有空格的文字就在字符处）截断并加 `…`；完整的摘要用 `GET /api/entries/{id}/ai` 取。单个条目不带这个字段。

`GET /api/entries` 的查询参数，都可选，可以组合：

- `source`：只看这个源；`kind`：只看这种形态的源。值不对时 `invalid_source`、`invalid_kind`。不带 `source` 也不带 `q` 且 `groupSameLink` 开着时，链接相同的条目合成一行，见[「同一篇文章只显示一次」](data.md#同一篇文章只显示一次)；带了就不合并。
- `folder`：只看这个文件夹里的订阅，可以和 `kind` 叠加，见[「文件夹时间线」](timeline.md#文件夹时间线)；不是已有文件夹的编号时 `invalid_folder`。
- `unread=1`：只看未读；`starred=1`：只看收藏。其他值当没给。有节奏的源里到期的条目、开了「不在全部里显示」的源的条目，只在带 `source`、`starred=1` 或 `q` 时返回，见[「节奏」](timeline.md#节奏)「不在全部里出现」。设了「每天最多」的源超出上限的条目，在没带 `source`、`starred=1`、`q` 时也返回，但带 `folded: true`，让客户端折成一行，见[「每日上限」](timeline.md#每日上限)。
- `q`：检索，语法见[「检索语法」](timeline.md#检索语法)：普通词、`"短语"`、`-排除`、`from:`、`kind:`、`after:`、`before:`，认不得的写法当普通词，不报错。不分大小写；标题、标题的译文、订阅摘要和抽取的正文都搜。去掉首尾空白后为空就是没有检索。普通词之间是「并且」，要连着的整句加引号。词元不足 3 个字符时那一个词元走 `LIKE`，规则和 `limited` 见[「检索语法」](timeline.md#检索语法)里「和分词的关系」：只搜标题和订阅摘要，不搜抽取的正文；`%`、`_`、`\` 当普通字符；结果受限：按 `order` 排序，最多返回 200 条，一次回复就完，不带下一页（`nextCursor` 为 `null`，`limit` 不起作用），响应里多一个 `"limited": true`；词元都够 3 个字符的检索没有这个字段。同样遵守其他筛选、静音规则和「到期」规则，也不合并相同链接的条目。界面在受限的结果上说明只搜了标题和摘要、最多 200 条。
- `tz`：可选，IANA 时区名，只在有 `q` 时用，决定 `after:`、`before:` 的日界；缺少或无法识别时用服务端所在的本地时区，不报错。
- `since`：Unix 毫秒时间戳（正整数），只看 `created_at` 不早于它的条目（含正好等于它的），也就是进入 Oriel 的时间，不是 `published_at`。服务端不认时区、不认「今天」：「今天」是客户端按自己的本地午夜算出的绝对时刻，见[「时间范围」](data.md#时间范围)。零、负数或不是整数时 `invalid_since`。没给就不筛。和其他筛选、`order`、`cursor` 叠加，游标照旧按 `(created_at, id)`；检索（`q`）时同样生效，和 `after:`、`before:`（比较的是发布日）互不替代。
- `limit`：一页几条，1 到 100，默认 50，否则 `invalid_limit`。
- `order`：`desc`（默认，最新的在前）或 `asc`（最旧的在前），按 `created_at`、`id` 同方向排；其他值时 `invalid_order`。
- `cursor`：上一页的 `nextCursor`，取它之后的一页：`desc` 时是更旧的，`asc` 时是更新的。是不透明的字符串，客户端不要拼；不对时 `invalid_cursor`。游标属于它发出时的 `order`，换了 `order` 要从头取。

默认新条目写在最上面，按游标翻页不会重复也不会漏，翻页时新到的条目要从头再取。`order=asc` 时新条目排在最后：翻到末尾的那一页会取到它们，所以游标一直取下去就不会漏。

`PATCH /api/entries/{id}`：

- `isRead` 改成 `true` 时记下 `read_at`，已经是已读就不变；改成 `false` 时清空 `read_at`。`groupSameLink` 开着时，`isRead` 和 `starred` 改的是链接相同的所有可见条目（见[「同一篇文章只显示一次」](data.md#同一篇文章只显示一次)），返回的是请求里那一条，带汇总后的状态和 `group`。
- `starred` 改成 `false` 后，过了保留期限的条目在下次清理时删掉。
- `remindAt` 是 Unix 毫秒：大于 0 时设稍后提醒（同时把条目标为已读，见[「稍后提醒我」](#稍后提醒我)），`0` 取消提醒、条目保持已读。必须比现在晚、且不超过一年（366 天），否则 400 `invalid_remind_at`；负数、和 `isRead: false` 同时给也是 `invalid_remind_at`。`isRead` 改成 `false` 同时取消提醒。`groupSameLink` 开着时，`remindAt` 和 `isRead` 一样改的是链接相同的所有可见条目。
- `readProgress` 是 0 到 1 的数（含两端），不是数或超出范围时 400 `invalid_progress`。它让阅读位置跟着用户走，换设备也一样。
- 不改条目的 `updated_at`：它只记订阅内容的变化。

`POST /api/entries/read` 的请求体可以为空，字段都可选：`sourceId`、`kind`、`folder`（文件夹编号，同 `GET /api/entries` 的 `folder`，不对时 400 `invalid_folder`）、`upTo`、`newerThan`、`from`、`olderThan`、`since`。`upTo` 是条目 `id`，只标时间线上它和比它旧的条目，免得把用户还没看到的新条目一起标掉；`newerThan` 也是条目 `id`，只标比它新的条目（不含它自己），和 `upTo` 一起用就标出一段：比 `newerThan` 新、不比 `upTo` 新；`from` 也是条目 `id`，只标它和比它新的条目（含它自己），和 `upTo` 一起用就标出一段：不比 `from` 旧、不比 `upTo` 新，给「把下面的标为已读」只标已加载的条目用。三个条目不存在时都是 `not_found`。`olderThan` 是 Unix 毫秒时间戳（整数），只标比它早的条目（不含正好等于它的）；比较的是列表行上显示的时间：`published_at`，没有时用 `created_at`。它和 `sourceId`、`kind` 一起用，和 `upTo`、`newerThan`、`from` 也可以同时用（几个条件都要满足）；不是正整数（零、负数）时 400 `invalid_older_than`。`since` 同 `GET /api/entries` 的 `since`（比较 `created_at`，不早于它），和其他条件一起要满足，只标进入 Oriel 时间在范围内的条目；不是正整数时 400 `invalid_since`。`ids` 是这次从未读变成已读的条目，给撤销用，`olderThan` 标出的也一样；之前就是已读的不在其中。`readAt` 是这次给它们记下的已读时间（Unix 毫秒），一次请求里的条目都一样，撤销时原样带回。`groupSameLink` 开着、没带 `sourceId` 时标的是时间线上的行，每行连同组里其余可见成员一起标，所以 `ids` 和 `marked` 数的是条目而不是行。每次返回全部 `ids`，不设上限（一次最多撤销 10000 个，超过的撤销请求会被拒绝，所以客户端对超过 10000 个的这次只做不提供撤销）；撤销状态只在客户端内存里，约 8 秒后丢掉，服务端不另存任何东西。

`POST /api/entries/unread` 的请求体是 `{ "ids": [...] }`，最多 10000 个，空或超出时 `invalid_ids`。只改其中已读的条目：清空 `read_at`，收藏不动；不存在的 `id` 跳过，不报错。

### 稍后提醒我

条目可以设「稍后提醒我」：现在不想读，到点再看到它。

- 设了提醒的条目马上变成已读，所以不在「未读」里，也不计入未读数；它在「全部」里仍按原位置显示，并标明设了提醒。
- 到点后，服务端把它改回未读：`is_read` 置 0、`read_at` 清空、`remind_at` 清空，并把 `created_at` 改成 `max(created_at, remind_at)`。时间线按 `(created_at, id)` 排序和翻页，所以它排到最上面，游标不用特殊处理；新到的条目仍可能排在它上面。
- 到点由两处触发，都不依赖客户端在线：`GET /api/entries` 和 `GET /api/sources` 在查询前先处理已到点的提醒（所以重启之后第一次取列表就是对的），抓取循环每分钟处理一次（让托盘和 Dock 上的未读数跟着变）。提醒存在库里，重启不丢。
- 取消：取消提醒（`remindAt: 0`，条目保持已读）、标为未读（`isRead: false`）、在到点前打开条目，都会清掉提醒；打开已是已读的条目不会把它改回未读。
- 选项在客户端按本地时间算成绝对时刻再发：「1 小时后」、「今晚」（本地 18:00，已过则明天 18:00）、「明天上午」（明天 09:00）、「下周」（下个星期一 09:00，今天是星期一则下周一）。服务端不认时区。
- 已知的边界：设了节奏的订阅（见[「节奏」](timeline.md#节奏)）按发布时间算到期，到点变回未读的条目若已过期，仍会离开「全部」和「未读」，只在检索、收藏和订阅自己的页面里看到。

### 屏蔽词

屏蔽词有两份，一份是源自己的 `muteKeywords`，一份是全局的 `muteKeywords`（见[「设置接口」](#设置接口)）。每行一个屏蔽词，两份一起生效：条目的标题或订阅摘要里包含其中任何一个，就算被屏蔽：

- 按子串匹配，不分大小写，不认通配和正则。只看标题和订阅摘要（纯文本），不看正文和抽取的正文。
- 被屏蔽的条目不出现在 `GET /api/entries` 的任何结果里，检索也一样；不计入源的 `unreadCount`；「全部已读」不改它们。
- 收藏的条目不屏蔽：用户特意留下的，不因为后来加了屏蔽词就看不见。规则加了星标的条目也一样。
- 屏蔽只是隐藏，不删条目，也不改阅读状态。按 `id` 仍能取到和修改。改了屏蔽词马上生效，清空后条目重新出现。
- 全局屏蔽词是单独一个列表，不并进规则：屏蔽是查看时的过滤，对已有的条目也生效，随时可以撤销；规则是新条目到来时写一次状态，不回头改。把两件事做成同一种东西，会让“改了屏蔽词为什么旧条目也变了”和“改了规则为什么旧条目没变”两个问题同时出现。

`POST /api/sources/{id}/muted-count` 的请求体是 `{ "muteKeywords": "..." }`，返回 `{ "count": n }`：这些屏蔽词现在会隐藏该源多少条条目。词不会保存，所以订阅的设置页能在用户打字时给出效果。词按上面的规则处理（每行去掉首尾空白、丢掉空行，匹配和时间线用同一个函数），收藏的条目不计入；词为空时 `count` 是 `0`。源不存在时 404 `not_found`。「设置 → 订阅」里订阅的页面随输入显示这个数。

`POST /api/muted-count` 是全局屏蔽词的同一个接口：请求体 `{ "muteKeywords": "..." }`，返回 `{ "count": n }`，是这些词现在会隐藏全部条目中的多少条（收藏的不计，已删除的源不计；条目已被源自己的屏蔽词隐藏的也算，所以这是这份列表一共隐藏的数量，不是新增的），词不保存。

## 设置接口

都要会话（设了口令时）。设置在 JSON 里是一个对象：

- `rsshubBaseUrls`：RSSHub 根地址的列表，按优先级排，第一个是主实例，没设为 `[]`。
- `rsshubBaseUrl`：兼容字段，等于 `rsshubBaseUrls` 的第一个，没设为空串。客户端读列表。
- `proxyUrl`：全局抓取代理的地址，没设为空串，见[「抓取」](fetching.md#抓取)的「代理」。`proxyAuthSet`：是否存了代理的用户名和密码；用户名和密码本身只能写（`PATCH` 的 `proxyUser`、`proxyPassword`），不下发。
- `retentionDays`：保留期限的天数，`0` 是永久。
- `fetchIntervalSec`：默认抓取间隔的秒数，`0` 是「自动」，默认 `0`；`fullTextDefault`：新订阅是否抓取全文，默认 `true`。见[「抓取」](fetching.md#抓取)的「全局默认」。
- `groupSameLink`：链接相同的条目在时间线上是否只显示一次，默认 `true`，见[「同一篇文章只显示一次」](data.md#同一篇文章只显示一次)。
- `muteKeywords`：全局屏蔽词，按行分隔，没设为空串，见[「屏蔽词」](#屏蔽词)。
- `websubBaseUrl`：WebSub 回调用的公开根地址，没设（功能关闭）为空串，见[「WebSub 推送」](fetching.md#websub-推送)。
- `aiBaseUrl`：OpenAI 兼容接口的根地址，例如 `https://api.openai.com/v1`，没设为空串。
- `aiModel`：模型名，没设为空串。
- `aiApiKeySet`：是否存了 API key。key 本身只能写（`PATCH` 的 `aiApiKey`），不下发。
- `aiLanguage`：摘要和翻译的目标语言，`zh-Hans` 或 `en`，默认 `zh-Hans`。
- `aiSummaryLimit`、`aiChunkLimit`：摘要最多送的字符数和翻译每块最多的字符数，用来配合小上下文的本地模型。没改过时是默认值 24 000 和 6000，返回的总是现在生效的值。这两个值跟着当前配置的这一个模型走，换模型不会自动改它们。
- `defaultAiSummaryLimit`、`defaultAiChunkLimit`：上面两个的默认值，只读，界面用它们作占位和「恢复默认」。
- `aiAutoDailyLimit`、`aiAutoFetchLimit`：自动处理每天最多发多少个请求、一次抓取最多给多少条新条目排自动任务，默认 200 和 10，返回的总是现在生效的值，见[「发送与费用」](ai.md#发送与费用)。`defaultAiAutoDailyLimit`、`defaultAiAutoFetchLimit` 是它们的默认值，只读。
- `aiAutoCallsToday`：今天（服务端本地时间）自动处理已发出的请求数，只读。到了 `aiAutoDailyLimit` 就是已到上限。
- `aiLastTest`：上次自检的结果，没有或已失效时为 `null`，只读，规则见[「调用」](ai.md)的自检。对象是 `{ "ok", "at", "durationMs", "error", "model", "modelFound", "contextTokens", "contextFits" }`，`at` 是 Unix 毫秒，`error` 是失败的错误码，成功时为空串。
- `aiEndpointLocal`：`aiBaseUrl` 是否指向服务端所在的这台电脑，只读，规则见[「发送与费用」](ai.md#发送与费用)。
- `promptSummary`、`promptTranslate`：全局 prompt。没改过时是内置的默认 prompt。
- `defaultPromptSummary`、`defaultPromptTranslate`：内置的默认 prompt，只读，界面用它判断 prompt 改没改过、恢复默认。
- `autoBackupEnabled`：是否自动备份，默认 `false`，见[「自动备份」](import-export.md#自动备份)。
- `autoBackupFolder`：自动备份写进的文件夹，是这台服务端上的绝对路径，没设为空串。
- `autoBackupFrequency`：`daily` 或 `weekly`，默认 `daily`。
- `autoBackupKeep`：最多留几份自动备份，1 到 100，默认 7。
- `autoBackupLastAt`、`autoBackupLastFile`：最近一次成功的自动备份的时间（Unix 毫秒，从没成功过为 `0`）和文件名（不含文件夹，没有为空串），只读。
- `autoBackupFailedAt`、`autoBackupError`：最近一次自动备份失败的时间（Unix 毫秒）和原因代码，没有失败或之后又成功了为 `0` 和空串，只读。原因代码：`folder_missing`、`folder_not_directory`、`folder_not_writable`、`disk_full`、`failed`（别的原因，细节在日志里，见[「诊断文件」](running.md#诊断文件)）。

| 路由 | 作用 |
|---|---|
| `GET /api/settings` | 当前设置。 |
| `PATCH /api/settings` | 改字段，只改请求里带的，返回改后的设置。 |

- `rsshubBaseUrls` 是字符串数组，每个去掉首尾空白，必须是带主机名的 http 或 https 绝对地址，可以指向本机或内网，不然 `invalid_rsshub_base_url`；空数组表示不设；末尾的 `/` 去掉，重复的（不分大小写）只留第一个，空串忽略；规整后超过 5 个时 `too_many_rsshub_base_urls`。库里按行存（设置 `rsshub_base_url`，一行一个）。
- `rsshubBaseUrl` 是兼容字段，也可写：去掉首尾空白，空串表示不设，否则等同于只有这一个地址的 `rsshubBaseUrls`，同样的规则和错误码。同一个请求里两个字段都带时，`rsshubBaseUrls` 为准。
- `proxyUrl` 去掉首尾空白后是空串（不用代理）或带主机名和端口的代理地址（`http`、`https`、`socks5`、`socks5h`，不带路径、查询串、片段和用户信息，最长 500 个字符），不然 `invalid_proxy_url`。`proxyUser`、`proxyPassword` 各最长 255 个字节（SOCKS5 的上限），否则 `invalid_proxy_credentials`；不带就不动，空串删掉；代理地址改了，存的用户名和密码一并删掉，除非同一个请求带了新的。库里存在 `fetch_proxy`、`fetch_proxy_user`、`fetch_proxy_password`；一次 `PATCH` 的所有设置在同一个事务里写入，不会出现新地址配旧凭据的中间状态。
- `muteKeywords` 每行去掉首尾空白、丢掉空行后存下，最多 200 行、每行最多 100 个字符，否则 `invalid_mute_keywords`。库里存在 `mute_keywords`。改了马上对时间线和未读数生效。
- `retentionDays` 是 0 到 3650 的整数，否则 `invalid_retention_days`；不是整数时 `invalid_body`。库里存的值读不成天数时按永久处理，返回 `0`（见[「保留期限」](data.md#保留期限)）。
- `fetchIntervalSec` 是 `0` 或 60 到 604800 的整数，否则 `invalid_interval`。`fullTextDefault` 是布尔值。库里的值读不成合法间隔时按「自动」，`default_full_text` 只有 `0` 是关。
- `groupSameLink` 是布尔值，不是布尔值时 `invalid_body`。库里存在 `group_same_link`，`0` 是关，没存或别的值都按开处理。
- `websubBaseUrl` 去掉首尾空白，空串表示关闭；否则必须是带主机名的 https 绝对地址，不带用户信息、查询串和片段，不然 `invalid_websub_base_url`。末尾的 `/` 去掉。库里存在 `websub_base_url`。
- `aiBaseUrl` 去掉首尾空白，空串表示不设；否则必须是带主机名的 http 或 https 绝对地址，可以指向本机或内网（例如本机的 Ollama），不然 `invalid_ai_base_url`。末尾的 `/` 去掉。
- `aiModel` 去掉首尾空白，最多 200 个字符，否则 `invalid_ai_model`。
- `aiApiKey` 去掉首尾空白后存下，空串表示删掉；最多 4096 个字符，否则 `invalid_ai_api_key`。本机的模型可以不要 key。
- `aiLanguage` 不是 `zh-Hans` 或 `en` 时 `invalid_ai_language`。
- `aiSummaryLimit` 是 500 到 100 000 的整数，`aiChunkLimit` 是 500 到 20 000 的整数，`aiAutoDailyLimit` 是 1 到 10 000 的整数，`aiAutoFetchLimit` 是 1 到 100 的整数，否则 `invalid_ai_limit`；`0` 恢复默认值。库里存在 `ai_summary_limit`、`ai_chunk_limit`、`ai_auto_daily_limit` 和 `ai_auto_fetch_limit`，空串即默认；存的值读不成范围内的整数时按默认处理。
- `promptSummary`、`promptTranslate` 最多 4000 个字符，否则 `invalid_prompt`；空串（去掉首尾空白后）恢复内置的默认 prompt。
- `autoBackupFrequency` 不是 `daily` 或 `weekly` 时 `invalid_backup_frequency`；`autoBackupKeep` 不是 1 到 100 的整数时 `invalid_backup_keep`。库里存在 `auto_backup_enabled`（`1` 或空）、`auto_backup_folder`、`auto_backup_frequency`、`auto_backup_keep`；存的值读不出来时按默认处理。
- `autoBackupFolder` 去掉首尾空白，空串表示不设；否则必须是绝对路径，不然 `invalid_backup_folder`。改文件夹，或把 `autoBackupEnabled` 设成 `true`（此时文件夹也不能是空的，否则 `invalid_backup_folder`）时，服务端当场检查文件夹：不存在 `backup_folder_missing`，不是文件夹 `backup_folder_not_directory`，写不进去（试着在里面建一个临时文件再删掉）`backup_folder_not_writable`。只关掉开关时不检查。服务端不会替用户建文件夹。
- 有一个字段不对就整个请求不生效。
- 抓取循环每次抓取和清理都重新读设置，改了从下一次抓取和清理起生效，不用重启。
- 只下发上面列出的字段。API key 一类的密钥不进任何响应、日志和错误，只回是否已配置。

## 图片代理

`GET /api/image?url=<地址>` 替界面取缩略图、视频封面和条目里的图片。要会话（设了口令时）。界面不直接连这些地址：它们来自订阅内容，远程模式下客户端和服务端也不在同一个网络，地址检查只在服务端做一处。

- `url` 必须是带主机名的 http 或 https 绝对地址，否则 400 `invalid_url`。
- 按「正文抽取」的规则只连公网地址，超时 25 秒，重定向最多 5 次，设了全局代理时经代理（见[「抓取」](fetching.md#抓取)的「代理」）。不带 `Referer`。
- 同一地址同时有多个请求时只向上游取一次，等着的请求共用结果；某个请求自己取消只是不再等，不会中断给别人的下载，等的人都走了才停。同时进行的上游下载最多 8 个，其余排队；25 秒从开始下载算起，排队的时间不计。
- 最大 8 MiB。按内容判断类型，只放行界面能解码的 PNG、JPEG、GIF、WebP 和 BMP。
- 已知的跟踪图片地址（见[「阅读器」](screens.md#阅读器)的「图片」）不去连，直接返回 502 `image_unavailable`，和被拦的地址一样；客户端本来就不会请求它们，这是给别的调用方挡一道。
- 连不上、地址被拦、上游不是 2xx、超过大小或类型不对，都返回 502 `image_unavailable`。
- SVG 不放行：它能带脚本，在服务端的源下打开时能读到会话 cookie；界面也解不了它。
- 成功时带判断出的 `Content-Type`、`X-Content-Type-Options: nosniff` 和 `Cache-Control: private, max-age=86400`。
- 地址若有收藏条目存下的本地副本（见[「收藏的本地副本」](data.md#收藏的本地副本)），在上面的连接和图片缓存之前就返回副本，响应头相同；原图失效也照常显示。跟踪地址的拒绝在这之前。

图片缓存：

- 成功取到的图片存在数据目录的 `image-cache/` 里（用到时才建），一个地址一个文件，文件名是地址的 SHA-256 十六进制；文件里是内容类型、存入时间和图片字节。同一地址的下一次请求先读缓存，命中就不再连上游。
- 总量上限 256 MiB，有效期 30 天（从存入算起，命中不续期）。过期的文件按未命中处理，重新取后覆盖。
- 超过上限时按最近使用淘汰：最近使用的时间记在内存里（重启后按各文件的存入时间），从最久没用的删起，删到上限的 90%。刚存入的那个不删。淘汰在后台进行，读和存不会等它排序、删文件；命中也不改文件的时间。
- 只缓存通过上面全部检查的成功响应；失败（连不上、被拦、非 2xx、超大小、类型不对）不缓存。已知的跟踪图片地址在读缓存之前就拒绝，不会被缓存，也不会从缓存里返回。
- 写入先写同目录的临时文件再改名，崩溃不会留下半个文件被读到；启动后第一次用到时扫描目录，清掉遗留的临时文件，沿用已有的文件。读不出或格式不对的文件当作未命中并删掉。
- 缓存只是加速：随时可以删掉整个 `image-cache/` 目录，下次请求会重新取。
- `DELETE /api/storage/image-cache` 清空这个缓存：删掉缓存里的每个图片文件，返回 204；缓存本来就空时也是 204。收藏的副本存在数据库里（见[「存储」](data.md#存储)），不受影响。删不掉的文件（Windows 上正被读取的）留在缓存里照常计数，下次清除再删。清除时正在下载的图片下完后照常存进缓存。

## 活动和存储接口

都要会话（设了口令时）。给设置里的「活动与存储」页和「抓取」页用，按需请求，不参与别的接口：「活动与存储」页打开时才定时重取，RSSHub 的健康只在「抓取」页打开时和保存实例之后读一次。

| 路由 | 作用 |
|---|---|
| `GET /api/activity` | 现在在抓什么，以及每个源最近的抓取结果。 |
| `GET /api/storage` | 数据库大小、条目数、图片缓存，以及压缩的状态。 |
| `GET /api/rsshub/health` | 设置里每个 RSSHub 实例的成功率、中位延迟和冷却（见[「RSSHub 实例健康」](data.md#rsshub-实例健康)）。 |
| `POST /api/storage/vacuum` | 在后台开始压缩数据库，返回 202 和 `{ "vacuum": {...} }`；已经在压缩时返回 409 `vacuum_running`。 |
| `DELETE /api/storage/premigrate` | 删除迁移前的副本，返回 204；本来就没有副本时也是 204。 |
| `GET /api/storage/read-entries` | 返回 `{ "count" }`：删除已读条目现在会删掉多少条。 |
| `DELETE /api/storage/read-entries` | 删除已读且未收藏的条目（见[「删除所有已读条目」](data.md#删除所有已读条目)），完成后返回 200 和 `{ "deleted" }`；没有可删的时 `deleted` 是 0。 |
| `DELETE /api/storage/image-cache` | 清空代理图片缓存，返回 204（见[「图片代理」](#图片代理)）。 |

`GET /api/rsshub/health` 返回 `{ "keep": 50, "days": 7, "instances": [...] }`。

- `keep`、`days` 是窗口的两个上限（见[「RSSHub 实例健康」](data.md#rsshub-实例健康)），界面用它们写说明，不自己写死数字。
- `instances` 按设置里的顺序，第一个是主实例，每项有 `url`（根地址，同设置里存的规整后的写法）、`requests`、`successRate`（`null` 或 0 到 1）、`medianMs`（`null` 或毫秒整数）、`coolingUntil`（`null` 或毫秒时间戳）。没设实例时是空数组；有实例而没有统计的，`requests` 是 0，另外三项是 `null`。

`GET /api/activity` 返回 `{ "keep": 10, "fetching": [...], "sources": [...] }`。

- `keep` 是每个源保留的次数（见[「抓取记录」](data.md#抓取记录)）。
- `fetching` 是此刻有工作线程在抓的源，按 `id` 排，每项有 `id`、`url`、`rsshubPath`、`title`、`elapsedMs`、`attempt`、`instance`（后三项的含义见[「抓取」](fetching.md#抓取)，`instance` 没有时为空串）。没有时是空数组。主机忙、还在队列里等的源不算。
- `sources` 是至少有一次记录的源，最近一次抓取最新的排前面（同一时刻按 `id`）；暂停的源只要还有记录也列出。每项有 `id`、`url`、`rsshubPath`、`title`、`paused`，和 `attempts`：该源最近的记录，最新的在前，最多 `keep` 条，每条 `at`（毫秒时间戳）、`durationMs`、`ok`、`status`、`newEntries`、`error`（含义见[「抓取记录」](data.md#抓取记录)）。

`GET /api/storage` 返回：

- `databaseBytes`：`oriel.sqlite` 的大小，即页数乘页大小（`PRAGMA page_count`、`page_size`），不含 WAL。
- `reclaimableBytes`：库里空闲页占的字节（`PRAGMA freelist_count` 乘页大小），压缩大约能腾出这么多。
- `entries`：条目总数。
- `copies`、`copyBytes`：收藏条目的图片副本（见[「收藏的本地副本」](data.md#收藏的本地副本)）的张数和字节数；它们存在数据库里，已经计入 `databaseBytes`，单列只是让用户知道其中收藏占多少。
- `images`、`imageBytes`：代理图片缓存（`image-cache/`）里的图片数和占用字节；缓存是可再生的，这里只是让用户知道数据目录里还有多大。
- `premigrate`：迁移前的副本（见[「存储」](data.md#存储)的「迁移前的副本」）；没有副本时是 `null`。有时是 `{ "bytes", "version" }`：`bytes` 是副本文件的大小；`version` 是副本文件头里记的 `user_version`，即它是在哪一版数据库结构上做的（迁移前的结构号，不是应用的版本号），直接读文件头的 4 个字节，不把副本当数据库打开，所以很便宜，也不会改动它；读不出来时是 0，界面就只显示大小。响应里没有副本的路径。`DELETE /api/storage/premigrate` 只删这一个文件，不删别的，目录或别的非普通文件同名时不动它。
- `vacuum`：`state` 是 `idle`（从没压缩过）、`running`、`done` 或 `failed`；`startedAt`、`finishedAt`（毫秒，没有为 0）；`freedBytes`（`done` 时压缩前后 `databaseBytes` 之差，不小于 0）；`error`（`failed` 时是错误文字）。状态在进程内存里，重启后回到 `idle`。

压缩数据库（`VACUUM`）怎么安全地在后台跑：

- 请求只负责开始：服务端起一个自己的 goroutine 去做，不用请求的上下文，所以客户端断开或超时不会打断它，也不会在 HTTP 的写超时里等它。同一时刻只跑一个，再请求时 409。
- `VACUUM` 重写整个数据库文件，期间持有写锁，需要的临时空间约等于数据库大小；磁盘不够时它失败，原库不受影响，`state` 为 `failed`，`error` 里是原因。进程中途退出也不会损坏数据库。
- 它持有写锁，别的写操作会等（最多 `busy_timeout`，5 秒），读不受影响（WAL）。为了不让这段时间里的抓取撞上超时而白白记成失败，压缩开始后抓取循环不再取新的源，源保持到期，压缩结束后接着抓；已经在抓的那几个会等写锁。压缩结束后做一次 WAL 检查点，让文件真正变小。
- 界面里只显示进度状态，不阻塞：开始后每隔几秒重取 `GET /api/storage`，直到 `state` 不再是 `running`。
