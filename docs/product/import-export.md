# 导入导出和备份

OPML、备份与恢复，以及从别的阅读器导入。索引见 [docs/product.md](../product.md)。

## OPML 接口

都要会话（设了口令时）。

| 路由 | 作用 |
|---|---|
| `POST /api/opml/import` | 请求体是 OPML 文本，逐条添加订阅，做完才返回 `{ "added": n, "existing": n, "folders": n, "failed": [...] }`，`folders` 是新建的文件夹数。给脚本和不要进度的调用方用。有导入任务（或另一个同步导入）在进行时返回 409 `opml_import_busy`。 |
| `POST /api/opml/import/start` | 请求体同上，开始一个导入任务就返回 202 和状态（下面「导入任务」）。文件的校验和上一条一样、当场做：413 `opml_too_large`、400 `invalid_opml`，都不开任务。已有任务在进行时返回 409 `opml_import_busy`。界面用这一条。 |
| `GET /api/opml/import` | 返回当前任务的状态；没有任务时 `phase` 为 `idle`。 |
| `GET /api/opml/export` | 所有源导出成一个 OPML 2.0 文件，`Content-Type: text/x-opml`，文件名 `oriel.opml`。 |

OPML 没有形态和 RSSHub 路径，Oriel 用 `outline` 上的自定义属性保留它们：`orielKind`（五种形态之一）和 `orielRsshubPath`；点开时的设置放在 `orielOpenTarget`（`follow`、`reader`、`browser`，导出时是默认的 `follow` 就不写）；开了「不在全部里显示」的源再多一个 `orielHideFromAll`，开了「翻译标题」的再多一个 `orielTranslateTitles`，设了「每天最多」的再多一个 `orielDailyCap`。别的阅读器不认这些属性，照常读 `xmlUrl`。导入的文件可以不是 UTF-8：按 XML 声明里的 `encoding`（GBK、ISO-8859-1 等）解码。

导入：

- 有 `xmlUrl` 或 `orielRsshubPath` 的 `outline` 是订阅，其余是分组。分组可以嵌套，逐层往下找。
- 分组变成文件夹（见[「文件夹」](data.md#文件夹)）：文件夹只有一层，嵌套的分组压平，订阅归到最外层分组的名字（`title`，没有就取 `text`，去掉首尾空白，超过 60 个字符截断）下；最外层不是分组、直接在 `body` 下的订阅不放进文件夹；名字是空的分组当不存在，订阅归到再外一层的分组，都没有就不放进文件夹。同名（不分大小写）的文件夹已有就沿用，没有才新建，所以没有订阅的分组不会建出空文件夹。新加的订阅放进这个文件夹；已经订阅了的，没有文件夹时放进去，已经在别的文件夹里的不动。
- 有 `orielRsshubPath` 时按 RSSHub 源加，不看 `xmlUrl`；否则按 `xmlUrl` 加。没有 `orielKind` 时形态是 `article`。`orielOpenTarget` 没有时是 `follow`；写了别的值，这一条按 `invalid_open_target` 算作失败，不加。`orielHideFromAll` 为 `1` 时新加的订阅开着「不在全部里显示」，其他值或没有就是关；已经订阅了的不改。`orielTranslateTitles` 为 `1` 时新加的订阅开着「翻译标题」，规则同它。`orielDailyCap` 是 1 到 1000 的整数时新加的订阅设上「每天最多」，其他值或没有就是关；已经订阅了的不改。
- 标题取 `title`，没有就取 `text`，去掉首尾空白；和订阅地址一样时当没给，留给抓取补上。
- 每条按「订阅接口」添加的规则校验，抓取间隔用默认值（自动）。已经订阅了的（按「源」一节的规则比较，包括同一文件里重复出现的、写法不同但是同一个订阅的）计入 `existing`，不算失败，也不会为它们另加订阅。
- 新增的源不是马上都到期：第一次抓取按「抓取」里的规则摊开，每个源随机延后 `[0, min(10 分钟, 2 秒 × 本次导入的订阅条数))`。
- 不合规则的条目不影响别的条目，放进 `failed`，每项是 `{ "title", "address", "error" }`，`error` 是添加时的错误码，比如 `invalid_url`、`invalid_kind`。
- 文件最大 1 MiB，超了返回 413 `opml_too_large`；不是 XML、根元素不是 `opml` 时返回 400 `invalid_opml`，什么都不加。

### 导入任务

导入进度由服务端报告，不由客户端分批发：导入是服务端做的一连串本地写入，不去抓取，文件最大 1 MiB，几秒内结束；客户端自己拆文件分几次发，每次的随机延后只看那一批的条数，第一次抓取就摊不开了，所以不这样做。`POST /api/opml/import/start` 和 `POST /api/opml/import` 走同一个函数，也共用同一把「一次一个」的锁：任务在进行时，同步的 `POST /api/opml/import` 同样返回 409 `opml_import_busy`，不会和任务交错写入；同步的那条自己进行时，开始任务或再来一个同步请求也是 409。两条路由的校验、规整、去重和第一次抓取的随机延后都一样，区别只是开始任务这条在后台做并报告进度。

- 状态是 `{ "id", "phase", "progress": { "done", "total" }, "result", "error" }`。`id` 是任务编号，每次开始换一个，客户端只认自己开始的那个：状态里的编号不是它的，就说明别的设备开了新任务，按「连接断了」处理。`phase` 是 `running`、`done`、`failed` 或 `idle`。
- `total` 是文件里订阅条目的个数（有 `xmlUrl` 或 `orielRsshubPath` 的 `outline`，分组里的也算，数法和摊开第一次抓取用的是同一个），开始时就知道，所以进度是确切的。`done` 是已处理的条数：新增、已有和不合规则的都算处理过，每处理一条加一；到 `total` 时 `phase` 是 `done`。没有订阅条目的文件，`total` 为 0，立刻 `done`。
- `result` 在 `done` 和 `failed` 时有，就是 `POST /api/opml/import` 的返回：`{ "added", "existing", "folders", "failed": [...] }`；`failed` 时是停下之前的计数。`error` 只在 `failed` 时有，是 `import_failed`（写库出错）。已经写进去的订阅不回滚，和同步的那一条一样，重新导入同一个文件，已有的计入 `existing`，不会重复。
- 任务只在内存里，一次一个，重启就没有了。结束后状态留着，直到下一个任务开始；客户端读到结果后不用清。任务用自己的上下文，开始它的请求断了不会中止；服务端关闭时中止。
- 没有取消：写入几秒内结束，取消留下的是同一份半截的列表，和等它结束没有区别。

导出：

- 每个文件夹一个分组 `outline`（只有 `text` 和 `title`，名字是文件夹的名字），里面是它的订阅，只有这一层；没有文件夹的订阅在 `body` 下，排在分组之后。没有订阅的文件夹不导出。每个源一个 `outline`，带 `text`、`title`、`type="rss"`、`xmlUrl`、`htmlUrl` 和 `orielKind`，点开时不是 `follow` 的再带 `orielOpenTarget`。标题为空时 `text` 用地址。
- 开着「不在全部里显示」的源另带 `orielHideFromAll="1"`，没开的没有这个属性；开着「翻译标题」的源同样另带 `orielTranslateTitles="1"`；设了「每天最多」的源另带 `orielDailyCap="N"`。
- RSSHub 源另带 `orielRsshubPath`。设了 RSSHub 根地址时，`xmlUrl` 是主实例（列表里第一个）的根地址加路径，别的阅读器能直接用；没设时没有 `xmlUrl`。
- 导出的文件再导入，得到同样的订阅：地址、RSSHub 路径、形态、点开时、标题、文件夹、「不在全部里显示」「翻译标题」和「每天最多」都一样。别的字段（抓取间隔、prompt、屏蔽词、暂停）不导出。

## 备份与恢复

把全部数据装进一个文件，换电脑、重装或出事时能原样找回。只有一种完整格式，没有「精简」版本。

### 备份文件

扩展名 `.oriel-backup`，本身是一个 zip 文件，里面两项：

| 文件 | 内容 |
|---|---|
| `manifest.json` | `{ "format": 1, "app": "oriel", "version": "<做备份的应用版本>", "schema": <数据库的 user_version>, "createdAt": "<UTC 时间，RFC 3339>" }`。`format` 是文件布局的版本，现在是 1。 |
| `oriel.sqlite` | 数据库的快照，用 `VACUUM INTO` 取得，所以是一致的，应用同时在抓取、写入也没关系。 |

- 快照里包括：订阅（含每个源的全部设置，`open_target` 也在内）、条目、已读和加星标状态、阅读进度、AI 结果、抓取记录、WebSub 状态、设置。
- 快照里不包括：会话（备份文件不能当登录凭据用）、API key（`ai_api_key`，密钥不进任何导出）、代理的用户名和密码（设置 `fetch_proxy_user`、`fetch_proxy_password` 和各源的 `proxy_user`、`proxy_password`，同样是密钥）、全文索引（恢复时重建，也让文件更小）、还在撤销期内的已删除订阅（连同它的条目，见[「删除订阅与撤销」](data.md#删除订阅与撤销)）。会话、API key、代理的用户名和密码和这些订阅是从快照这份副本里删掉的，正在用的数据库不动。恢复后 AI 要重新填 API key，代理要认证的要重新填用户名和密码，除非这台服务端本来就配着（恢复时保留这台服务端自己的 API key 和全局代理的用户名和密码；各源的代理用户名和密码随源恢复，所以恢复后是空的）。
- 代理图片的缓存是可再生的，不进备份。每台设备上的 `preferences.json`（窗口位置、界面语言等）和 `remote.json` 在客户端，服务端没有，也不进备份。
- 文件名默认 `Oriel-YYYY-MM-DD.oriel-backup`。macOS 的保存对话框靠应用声明的导出类型（`mygo.json` 的 `macos.infoPlist` 里的 `UTExportedTypeDeclarations`，扩展名 `oriel-backup`）认得这个扩展名，不会再补一个。

### 接口

都要会话（设了口令时）。

| 路由 | 作用 |
|---|---|
| `GET /api/backup` | 流式返回备份文件，`Content-Type: application/zip`，`Content-Disposition` 是 `attachment`，文件名同上。取快照出错时在写出任何字节之前返回 500 `internal`。 |
| `POST /api/restore` | 请求体就是备份文件（不是 JSON），最大 4 GiB。成功返回 `{ "sources": n, "entries": n }`，是恢复后订阅和条目的数量。 |

`POST /api/restore` 的错误：

| 状态 | 代码 | 何时 |
|---|---|---|
| 400 | `invalid_backup` | 不是 zip，缺 `manifest.json` 或 `oriel.sqlite`，`app` 不是 `oriel`，或数据库不是 Oriel 的 |
| 413 | `backup_too_large` | 超过 4 GiB |
| 422 | `corrupt_backup` | 数据库没通过 `PRAGMA integrity_check`，或打不开 |
| 422 | `backup_newer` | 备份来自更新的版本：`schema` 比这个版本的迁移多，或 `format` 比 1 大。请先升级 Oriel |
| 409 | `restore_running` | 另一个恢复正在进行 |
| 409 | `vacuum_running` | 正在压缩数据库，稍后再试 |

读写都是长请求，这两个路由的读写超时放宽到 30 分钟，不受其他路由 1 分钟的限制。

### 恢复的步骤

客户端只通过上面的接口恢复（[ADR 0002](../adr/0002-standalone-client-and-server.md)），不碰存储。服务端依次做：

1. 把请求体写进数据目录下的临时目录，超过上限就中止（413）。
2. 检查：zip 和 `manifest.json`；把快照解出来，`PRAGMA integrity_check` 必须是 `ok`；`user_version` 比本版本的迁移多就拒绝（`backup_newer`），少就在这份临时副本上按迁移逐个升级。到这一步为止正在用的数据一点没动。
3. 暂停抓取循环和 AI 后台：正在进行的抓取中止，正在进行的 AI 任务留在队列里不记失败。
4. 在一个事务里替换：清空订阅、条目、AI 结果、抓取记录、WebSub 状态和设置（保留这台服务端的 `ai_api_key`），再把备份里的行按外键的先后次序写进去，重建全文索引，检查外键，提交。会话表不动，所以当前设备和其他设备不会被登出。
5. 恢复后重新启动抓取循环和 AI 后台。每个源按各自的 `next_fetch_at` 继续抓取。

任何一步失败都回滚（或在提交之前就中止），现有数据保持原样，临时文件删除。数据库连接从头到尾不关闭，恢复期间进来的请求要么看到全部旧数据，要么看到全部新数据，不会用到关掉的数据库。「保留旧库直到成功」靠事务回滚做到，不需要再留一份旧文件。

### 命令行

- `oriel backup [-data 目录] -o 文件`：直接读数据目录里的数据库，应用在不在运行都可以，写出同样的备份文件。文件已存在时不覆盖，报错退出。
- `oriel restore [-data 目录] [-force] 文件`：打开数据目录（别的 Oriel 进程占着时报错，请先退出），做上面的检查和替换。目录里已经有订阅或条目时，不加 `-force` 就拒绝，什么都不改。成功时打印恢复了多少订阅和条目。
- 失败时退出码非零，错误写到标准错误；口令、API key 不出现在输出里。

### 自动备份

按设定的频率把同样的备份文件写进一个文件夹，免得靠人记着去备份。由服务端做（桌面应用内嵌的服务端或 `oriel serve`），不在界面里，所以没有窗口的服务端也有；界面只是改设置、看状态。设置和状态的字段见[「设置接口」](api.md#设置接口)。

- 设置：开关（默认关）、文件夹、频率（每天 `daily` 或每周 `weekly`）、保留份数（1 到 100，默认 7）。文件夹是**服务端那台机器**上的路径：连着远程服务端时，在客户端的电脑上选不到它，界面只能让人把路径打出来（见[「设置界面」](screens.md#设置界面)的「导入和导出」里的「自动备份」）。
- 文件名 `Oriel-YYYY-MM-DD-HHMMSS.oriel-backup`，时间是服务端的本地时间。先写进同一个文件夹里的临时文件（`.oriel-backup-*.tmp`），写完并落盘再改名，所以半截的文件不会有完成的名字。进程崩溃留下的、超过一小时的临时文件，下次自动备份时删掉。
- 调度：服务端启动时和之后每小时检查一次。开着、而且从没成功过或最近一次成功已是一个频率之前（每天 24 小时，每周 7 天）就备份一次。改了设置也立刻检查一次，所以刚打开开关就会备份。检查在后台做，不挡请求。恢复或压缩数据库进行中不备份，恢复开始时进行中的自动备份中止（临时文件删掉），恢复完再检查。
- 保留：写完新的一份之后，文件夹里文件名完全符合 `Oriel-YYYY-MM-DD-HHMMSS.oriel-backup` 的普通文件按名字（也就是按时间）排序，超出保留份数的最旧的删掉。别的文件，包括手动备份的 `Oriel-YYYY-MM-DD.oriel-backup`，一概不动。
- 失败（文件夹没了或不可写、磁盘满……）不影响应用的其他功能：记下时间和原因代码（`autoBackupFailedAt`、`autoBackupError`），写一条日志，下一次每小时的检查再试，不连续重试。再成功时失败记录清掉。成功只记录在备份文件写完、改名之后；清理旧文件失败只写日志，不算备份失败。
- 状态存在设置里，所以会跟着备份和恢复走：恢复之后设置是备份里的，里面的文件夹可能在这台机器上不存在，那样下一次检查就会如实报失败。

## 导入其他阅读器接口

从别的阅读器的服务读出订阅、已加星标的条目和已读状态，写进 Oriel。支持 Miniflux 和 FreshRSS；代码里每个服务实现同一个 `Importer` 接口（取回一份快照）。都要会话（设了口令时）。

| 路由 | 作用 |
|---|---|
| `POST /api/import/start` | 请求体 `{ "service", "baseUrl", "username", "credential" }`，开始一个导入任务，进入预览阶段，返回 202 和状态。已有任务在进行（不是空闲、已完成、失败或已取消）时返回 409 `import_busy`。 |
| `GET /api/import` | 返回当前任务的状态；没有任务时 `phase` 为 `idle`。 |
| `POST /api/import/confirm` | 预览完成（`ready`）后确认，开始写入，返回状态。别的阶段返回 409 `import_not_ready`。 |
| `POST /api/import/cancel` | 立即停止抓取或写入，丢掉预览；任务已结束时清回 `idle`。 |

- `service` 是 `miniflux` 或 `freshrss`，别的值返回 400 `invalid_service`。`baseUrl` 必须是 `http` 或 `https` 地址，有主机名，不带用户名口令、查询串和片段，否则 400 `invalid_base_url`。末尾的 `/` 去掉；FreshRSS 末尾的 `/api/greader.php` 也去掉（填哪一种都行）。`credential` 不能为空（`missing_credential`）。`username` 只有 FreshRSS 要用（它的登录名），Miniflux 忽略。
- 任务只在内存里，一次一个，重启就没有了。状态是 `{ "phase", "service", "progress": { "stage", "done", "total" }, "counts": { "sources", "folders", "starred", "read" }, "result", "error" }`。`phase` 依次是 `fetching`（取数据）、`ready`（预览完成，等确认）、`writing`、`done`，另有 `failed`、`canceled`、`idle`。`progress.stage` 是 `subscriptions`、`starred`、`read` 或 `writing`。`counts` 在预览阶段就有；`error` 是下面的错误码，只在 `failed` 时有。
- `result`（`done` 时）是 `{ "addedSources", "existingSources", "starredAdded", "starredExisting", "readMarked", "unmatchedRead", "foldersCreated", "droppedLabels": [{ "title", "address", "kept", "dropped": [...] }], "droppedLabelsTotal", "skipped": [{ "kind", "title", "address", "error" }], "skippedTotal" }`。`skipped` 和 `droppedLabels` 最多各 100 条，`skippedTotal`、`droppedLabelsTotal` 是实际总数；`droppedLabels` 只有 FreshRSS 会有，见[「匹配和写入」](#匹配和写入)；`kind` 是 `source` 或 `starred`；`error` 对订阅是和 OPML 导入一样的校验码（`invalid_url` 等），对星标条目是 `unknown_source`（找不到所属订阅）或 `invalid_link`（没有可用的链接）。

### 凭据

- 凭据（Miniflux 的 API token、FreshRSS 的 API 密码，以及 FreshRSS 登录后换到的 `Auth` 令牌）只存在内存里，只活到取数据结束：取完得到快照，写入只用快照，不再需要凭据，快照取完就把凭据丢掉。
- 不落盘、不进数据库、不写日志、不放进错误信息、不在任何响应里回显（状态里没有 `credential`，也没有 `baseUrl` 以外的连接信息）。请求体照常受 64 KiB 的限制。
- 取消、失败和完成后任务里都不留凭据。凭据只通过请求头发给用户填的那个主机；重定向到别的主机时不跟随，直接失败（`redirected`），不会把头带过去。
- 测试里有一条检查：凭据字符串不出现在日志、错误、库里的行和任何响应中。

### 请求方式

- 用普通的 `http.Client`，不用抓取条目链接那个 `guardedClient`：`baseUrl` 是用户自己填的，常常是内网或本机的地址（局域网里的 Miniflux），`guardedClient` 会拒绝这些地址。代价是服务端会向用户指定的地址发请求，所以只做下面列出的调用，只用 GET（FreshRSS 登录那一个 POST 除外），任务一次一个，且要会话。
- 每个请求 30 秒超时，响应最多读 16 MiB（超了 `too_large`）。连续两个请求之间至少隔 200 毫秒。收到 429 时按 `Retry-After`（秒数或日期，最多等 60 秒）等待后重试，最多 3 次，仍是 429 就以 `rate_limited` 失败。
- 重定向最多 3 次，只跟随同一主机名，不从 https 降到 http。等待和请求都跟着任务的取消停下。
- 错误码：`invalid_base_url`、`unauthorized`（401、403）、`rate_limited`、`unreachable`（连不上、超时）、`bad_response`（其他非 2xx 状态，或读不出预期的 JSON）、`too_large`、`redirected`。原始的传输错误不放进错误信息，因为它可能带请求地址。

### 调用

Miniflux（`<baseUrl>/v1/...`，请求头 `X-Auth-Token: <API token>`）：

- `GET /v1/feeds`：订阅（`feed_url`、`site_url`、`title`、`category.title`）。分组数是不同的 `category.title` 个数。
- `GET /v1/entries?starred=true&limit=100&offset=N`：加星标的条目，翻页到 `total` 为止。
- `GET /v1/entries?status=read&limit=100&offset=N`：已读的条目，同样翻页。
- 条目取 `url`、`title`、`author`、`published_at`、`content`、`starred`、`status` 和 `feed.feed_url`；`status` 为 `removed` 的忽略。不认得的字段忽略。

FreshRSS（Google Reader 兼容接口，`<baseUrl>/api/greader.php`）：

- `POST accounts/ClientLogin`，表单 `Email`（登录名）和 `Passwd`（API 密码），回应里的 `Auth=` 行是令牌；之后每个请求带 `Authorization: GoogleLogin auth=<令牌>`。
- `GET reader/api/0/subscription/list?output=json`：订阅（`id`、`title`、`url`、`htmlUrl`、`categories[].label`）。
- `GET reader/api/0/stream/contents/user/-/state/com.google/starred?n=1000&output=json&c=<continuation>`：加星标的条目，按 `continuation` 翻页。条目链接取 `canonical[0].href`，没有就取 `alternate[0].href`；所属订阅取 `origin.streamId`，对应订阅列表里的 `id`。
- 已读状态：`reader/api/0/stream/items/ids` 只给条目 ID，没有链接，没法和 Oriel 里的条目对上，所以读 `stream/contents/user/-/state/com.google/read`（同样翻页）取链接。只要仍在订阅里的条目，已读流里别的订阅的不要。
- 星标和已读各最多读 20 000 条，再多的不读，结果里记一条 `limit`。

### 匹配和写入

- 订阅：和 OPML 导入走同一条路（同样的地址校验、默认抓取间隔、第一次抓取的随机延后）。按规整后的地址和已有订阅匹配，已经订阅的保留不动，计入 `existingSources`。导入时不去抓取，抓不了的地址之后在订阅健康里显示失败，和 OPML 一样，不会因此暂停。地址不合规则的订阅放进 `skipped`（`kind` 为 `source`）。
- 分组和标签：导入成文件夹（见[「文件夹」](data.md#文件夹)，一个订阅最多一个文件夹，文件夹不嵌套）。Miniflux 的每个分类一对一变成同名文件夹，没有分类的订阅不放进文件夹。FreshRSS 的订阅可以有多个标签，只取第一个（`categories` 里的第一个标签）放进同名文件夹，其余的不导入，把订阅的标题、地址、留下的标签 `kept` 和丢掉的标签 `dropped` 记进结果的 `droppedLabels`，界面据此告诉用户哪些标签没有变成文件夹。文件夹名按 OPML 导入的规则规整（去首尾空白，超过 60 个字符截断，空名当没有），同名（不分大小写）的已有文件夹沿用；`foldersCreated` 是新建的个数，预览里的 `counts.folders` 是将要用到的不同文件夹名的个数（FreshRSS 只数第一个标签）。已经订阅了的订阅，没有文件夹时放进去，已经在别的文件夹里的不动。
- 星标条目：找到所属的订阅（没有就按上面加上）。条目抓不到所属订阅也照样存：插入的条目挂在这个订阅下，不暂停订阅。条目按链接匹配：先用规整后的链接当 GUID 对 `UNIQUE(source_id, guid)`，再用 `linkKey` 对该订阅已有条目的链接。已有的条目只加星标，不改别的；没有的就插入，带标题、作者、发布时间、摘要或正文（Miniflux 给的 `content`），`created_at` 取发布时间（晚于现在就取现在，没有就取现在），不排 AI 任务；正文抽取状态按形态是 `pending` 或 `skipped`。后来的抓取遇到真实 GUID 时沿用「条目」里的规则，接管这条条目并保留它的星标和已读。
- 已读状态：只用在匹配到的、已有的条目上，只往已读方向改，不把已读改回未读，不创建只读不星标的条目（这类计入 `unmatchedRead`）。刚插入的星标条目如果在源上已读，也标已读。
- 刚新增的订阅还没有条目，所以已读状态主要作用在已经订阅过的订阅上；新订阅里已读的条目等抓取后也不会自动标已读，计入 `unmatchedRead`。
- 同一份快照导入两次，第二次什么都不加：订阅计入 `existingSources`，条目计入 `starredExisting`，已读不变。
- 写入按订阅分批，一个订阅一个事务。取消在两批之间生效，已写的不回滚。写完后时间线重新读取订阅和条目。
