# AI

摘要和翻译，以及发送与费用。索引见 [docs/product.md](../product.md)。

## AI 摘要和翻译

摘要和翻译走用户自己的 OpenAI 兼容接口（`aiBaseUrl` 和 `aiModel` 都设了才算配置好）。都要会话（设了口令时）。

| 路由 | 作用 |
|---|---|
| `GET /api/entries/{id}/ai` | 条目的两种结果和当前的目标语言：`{ "summary": ..., "translation": ..., "targetLanguage": "zh-Hans" }`，没有的结果为 `null`。 |
| `POST /api/entries/{id}/summary` | 要摘要，返回这一种结果。 |
| `POST /api/entries/{id}/translation` | 要翻译，返回这一种结果。 |
| `POST /api/ai/test` | 自检：用当前设置检查接口，成功返回 `{ "reachable": true, "model": "...", "modelFound": true \| null, "contextTokens": n \| null, "contextFits": true \| false \| null }`；没配置好时 409，失败时 502，带下面的错误码。见下面的「自检」。 |
| `POST /api/ai/selection` | 翻译或解释阅读器里选中的一段文字：`{ "task": "translate" \| "explain", "text": "...", "title": "..." }`，成功返回 `{ "text": "...", "model": "..." }`。见下面的「所选文字」。 |

一种结果是 `{ "status", "text", "error", "model", "updatedAt" }`，`status` 是 `pending`、`ok`、`failed`，`error` 只在 `failed` 时有值。

要结果（`POST`）：

- 没配置好时 409 `ai_not_configured`，条目不存在时 `not_found`。
- 已有 `ok` 的结果且指纹没变（模型、prompt、目标语言和原文都一样），直接返回，不调接口。
- 已经 `pending` 时直接返回，不重复排队。
- 其他情况（没有、失败过、指纹变了）排进队列，返回 202 和 `pending` 的结果；上一次成功的 `text` 留着，换成新结果前照样返回。客户端用 `GET /api/entries/{id}/ai` 轮询，建议每 2 秒一次。
- 用户要的不受自动重试的次数限制，每次 `POST` 都再试一次。

送去的原文：

- 第一行是条目标题，空一行，后面是正文的纯文本，段落之间空一行。正文依次取抽取的正文、订阅里的正文、订阅里的摘要，取第一个不为空的；都为空且没有标题时直接失败，`ai_empty`。
- 文章形态的条目还在等待抽取（`pending`）时先等抽取结束，再送。
- 摘要最多送 `aiSummaryLimit` 个字符（默认 24 000），翻译最多 60 000 个字符，多出的截掉。
- 翻译按段落切成每块不超过 `aiChunkLimit` 个字符（默认 6000），一块一个请求，按顺序翻完再拼起来。
- 翻完一块马上把它的译文存进 `entry_ai_chunks`（只存多于一块的翻译）。有一块失败，这一条翻译记为失败，但已经翻完的块留着；之后不论是自动重试还是用户点「重试」，都先按块的 `key` 找缓存，找到的直接用，不再发请求，只重发失败的和还没翻的块。送出内容变了（模型、prompt、目标语言或这一块的原文不同）`key` 就变，旧缓存用不上，也不会被误用。整条翻译成功时，这一条的缓存行全部删掉。

调用：

- `POST {aiBaseUrl}/chat/completions`，`stream: false`。system 消息是生效的 prompt，user 消息是原文；设了 key 时带 `Authorization: Bearer <key>`。
- 生效的 prompt：源的 `promptSummary` / `promptTranslate` 不为空就用源的，否则用全局的。prompt 里的 `{language}` 换成目标语言的名字（`简体中文`、`English`）。
- 内置的摘要 prompt：`Summarize the article in {language} in three to five sentences. Reply with the summary only.`；翻译 prompt：`Translate the text into {language}. Keep the paragraphs and their order, and the title on the first line. Reply with the translation only.`
- 每个请求超时 120 秒，响应最大 1 MiB。自检是例外：列模型和短请求共用一个 120 秒的总预算，最坏情况也是 120 秒，客户端等 130 秒。认 `HTTP_PROXY` / `HTTPS_PROXY`，不用「抓取」里的抓取代理。接口地址是用户自己填的，可以指向本机或内网，不走正文抽取的公网检查。
- 结果取第一个 `choices[].message.content`，去掉首尾空白，再去掉开头的一段 `<think>…</think>`（推理模型会把思考写进正文）；为空算失败。`finish_reason` 为 `length`（被接口的输出上限截断）的答复算失败，`ai_upstream`，不保存、不当作翻完的一块，照常走重试。

自检（`POST /api/ai/test`）在服务端做，界面只显示结果，从不在绘制时联网：

1. 连通：`GET {aiBaseUrl}/models`，带 key（设了的话）。连不上是 `ai_unreachable`，超时是 `ai_timeout`，401 或 403 是 `ai_unauthorized`。别的状态码、不是预期的 JSON，或这个接口不存在（有的服务没有 `/models`）都不算失败，安静地跳过后两项里依赖列表的部分：`modelFound` 和 `contextTokens` 为 `null`。
2. 模型存在：列表里有 `aiModel`（不分大小写，没写标签时也认 `:latest`，Ollama 的写法）时 `modelFound` 为 `true`；列表读到了却没有这个模型是 `ai_model_not_found`，不再发下一步。
3. 发一个很短的 chat 请求；成功时 `model` 是答复的模型。上游 404 也是 `ai_model_not_found`。
4. 上下文：模型在列表里报了上下文长度（依次认 `loaded_context_length`、`context_length`、`max_context_length`、`context_window`，单位 token）时 `contextTokens` 是它。限制放得下时 `contextFits` 为 `true`，放不下为 `false`，不知道为 `null`。放不下不是失败：HTTP 仍是 200，由界面提示调小限制。估算按每两个字符约一个 token，摘要要 `aiSummaryLimit ÷ 2 + 1000`，翻译每块要 `aiChunkLimit + 1000`（原文和译文各占一半，所以不除以 2），两个都放得下才算 `true`。

错误里不带 key，日志也不记 key。接口地址可以是本机，例如 `http://127.0.0.1:11434/v1`，同样走这条调用。

失败的错误码（写进结果的 `error`，`POST /api/ai/test` 直接返回）：

| 代码 | 原因 |
|---|---|
| `ai_not_configured` | 没设接口地址或模型。 |
| `ai_unreachable` | 连不上（DNS、拒绝连接、TLS）。 |
| `ai_timeout` | 超时。 |
| `ai_unauthorized` | 上游 401 或 403，多半是 key 不对。 |
| `ai_rate_limited` | 上游 429。 |
| `ai_model_not_found` | 接口的模型列表里没有这个模型，或聊天请求得到 404。 |
| `ai_upstream` | 上游其他非 2xx，或返回的不是预期的 JSON，或结果为空，或答复被输出上限截断（`finish_reason` 为 `length`）。 |
| `ai_empty` | 条目没有可送的文字。 |
| `ai_cap_reached` | 自动处理碰到了每天或每次抓取的上限，这一条没有处理。不会自动重试，用户点「摘要」「翻译」或「重试」会照做（见[「发送与费用」](#发送与费用)）。 |

所选文字（`POST /api/ai/selection`）：

- 同步做完再回答，不排队、不占上面处理器的名额，什么都不写进数据库。
- `task` 是 `translate` 或 `explain`，别的值 400 `invalid_task`。`text` 去掉首尾空白后为空 400 `invalid_text`，超过 2000 个字符 400 `text_too_long`。`title` 可不给，是所读文章的标题，只作为解释的背景，去掉首尾空白后最多取 200 个字符，多出的截掉，不会因它报错。
- 没配置好时 409 `ai_not_configured`；上游失败时 502，带上面的错误码（结果为空和条目的一样是 `ai_upstream`；`ai_empty` 只用于条目没有可送的文字，所选文字不会有这种情况）。
- 翻译用生效的翻译 prompt 里的全局那一份（所选文字不属于某个源），目标语言和条目翻译相同；送去的 user 消息就是所选文字。
- 解释的内置 prompt：`Explain the selected text in {language} in one to three plain sentences, so a reader who does not know the term or phrase can follow. Reply with the explanation only.`；有 `title` 时 user 消息是 `Article: <title>` 一行、空一行、`Selected text: <text>`，没有时只送所选文字。
- 调用、超时、响应上限、错误码和日志的规矩与上面相同；日志不记所选文字。

自动处理：

- 源开了 `autoSummary` / `autoTranslate` 时，抓取写入的新条目自动排队（`requested` 为 `0`）。只管打开开关之后新到的条目，不补旧条目。没配置好时不排。条目的语言已知且等于目标语言时不排自动翻译（见[「语言检测」](data.md#语言检测)）；文章的语言要等抽取到全文才可能得知，之前排下的翻译照做。
- 后台一个处理器，同时最多 2 个请求。先做用户要的（新的在前），再做自动的（新的在前）。`POST` 排队后马上唤醒它，此外每 15 秒看一次队列。
- 自动的失败了按「满 3 次不再自动重试」处理，每次之间至少隔 5 分钟；不影响抓取和抽取。跟条目有关的失败（`ai_upstream`、`ai_empty`，包括被截断的答复）计入这 3 次。接口本身的失败（`ai_unreachable`、`ai_unauthorized`、`ai_rate_limited`、`ai_model_not_found`）不计：结果照样记为失败并带上错误码，但 `attempts` 不加一。`ai_timeout` 要看接口当时的状况：接口此前一直正常（没有在退避）时，可能是这一条太长，计入 3 次；接口已在退避时算接口的，不计。
- 接口失败时处理器整体退避：等 1 分钟，再失败翻倍，最长 30 分钟，下一次成功就清零。429 带 `Retry-After`（秒数或日期）时至少等那么久，同样最长 30 分钟。退避期间不再发自动的请求，用户要的不受影响，照常发出并给出原因；用户自己点出来的请求失败了，不再把退避翻倍，只沿用当前的等待，也不会缩短已有的等待。用户点一个正排队的自动结果，它就转为用户要的，哪怕它正在处理中也不会被每天的上限叫停。自检或选中文字的翻译、解释成功，说明接口恢复了，退避马上清掉；用别的模型或 key 成功的不算。`ai_unauthorized` 和 `ai_model_not_found` 时一直停到接口地址、模型或 key 改了为止（或用户自己点出来的请求、自检、选中文字的请求成功）。换了其中任何一个，退避马上清掉。
- 自动处理受上限约束，见下面的「发送与费用」。
- 设置改了之后，已有的结果不重做；下次用户要时指纹变了才重做。

日志只记条目 id、任务、模型、错误码和耗时，不记 key、prompt、原文和结果。

### 标题翻译

外文订阅可以在时间线里直接显示翻译后的标题。它是每个订阅的开关 `translateTitles`，默认关闭，要接口配置好才用得上（没配置好时开关仍可改，和 `autoSummary` 一样，只是不会排任何东西）。

- 只管打开开关之后新到的条目，不补旧条目：不回填，已有的标题不会被翻译。关掉开关后再打开，也只管之后新到的。
- 抓取写入新条目时，把标题排进后台队列（条目的 `title_tr_status` 记为 `pending`）。标题为空的不排。条目的语言已知且等于目标语言时不排（和自动翻译共用「语言检测」的结果，判断是 `language != "" && language == 目标语言`）；语言要等抽取到全文才可能得知的文章，标题照排，处理这一批之前再判断一次，那时语言已经等于目标语言的就跳过，状态记为空。
- 标题不是一条一个请求，而是成批发：处理器每次取最多 40 条 `pending` 的标题（旧的在前），同一个目标语言的合成一个请求。批和摘要、翻译共用同一个处理器、名额、退避和每天的上限，一个请求算一个。
- 批的格式：system 消息是生效的翻译 prompt 之外的内置标题 prompt：`Translate each title in the JSON array into {language}. Reply with a JSON array of strings of the same length and in the same order, and nothing else. Keep a title that is already in {language} as it is.`；user 消息是标题的 JSON 数组（`["Title one","Title two"]`），每个标题最多取 300 个字符。答复去掉 `<think>…</think>` 和 Markdown 代码围栏后必须是同样长度的字符串数组，否则这一批算失败（`ai_upstream`）。调用、超时、响应上限和错误码与「调用」相同。
- 批失败了（`ai_upstream`，条目相关的失败）就对半拆开，两半各发一个请求，一半还失败就再对半拆，直到只剩一条。只剩一条还失败的，这一条的 `title_tr_attempts` 加一，状态记为 `failed`；满 3 次不再自动重试，每次之间至少隔 5 分钟，没满的下一轮重新排进批里。拆开后的请求同样计入每天的上限；批只有一条时不再拆。
- 接口本身的失败（`ai_unreachable`、`ai_unauthorized`、`ai_rate_limited`、`ai_model_not_found`、接口已在退避时的 `ai_timeout`）不拆也不计次数：整批仍是 `pending`，处理器按「自动处理」的规则退避，恢复后再发。
- 到了每天的上限，还在排队的标题记为 `failed` 且次数记满，不再自动重试，也不再发请求；标题不受「每次抓取上限」约束，只受每天的上限；第二天零点之后新到的条目照常处理，被停下的不补做。
- 存储：译文写在条目的 `translated_title`，状态是 `title_tr_status`（空、`pending`、`ok`、`failed`）。`ok` 的标题不再请求。模型把标题原样还回来（和原标题一样，不分首尾空白）时存空，不显示副标题。条目的标题被抓取改了（`title` 变了）就清掉译文并按开关重新排一次。这些字段是派生内容，写入它们不改 `updated_at`，也不算条目有更新。
- 搜索：`q` 同时匹配原标题和译文。译文用 LIKE 匹配，不进全文索引。
- 没配置好接口、接口出错、碰到上限时，静默显示原标题，界面不提示、不画占位。
- 开关关掉时，这个订阅所有条目的译文和 `pending` 状态一并清掉。
- 送去的内容只有标题，在「发送与费用」的表里。

## 发送与费用

摘要和翻译会把条目的文字发到用户在设置里填的接口（`aiBaseUrl`）。这一节定下所有会把内容发出去的功能共同守的规矩，新加的这类功能也要先守它才能做。

同意：

- 凡是自动发送或批量发送内容的功能，默认关闭，各有各的开关；打开一个不会带着打开另一个。开关是每个订阅的 `autoSummary`、`autoTranslate` 和 `translateTitles`，默认都是关；它们只管打开之后新到的条目。
- 用户自己点「摘要」「翻译」「翻译所选」「解释所选」是一次明确的请求，不需要开关。
- 没配置好接口（`aiBaseUrl` 和 `aiModel` 缺一个）时什么都不发。

每个功能发什么，写在这里，新增的功能先在这里加一行才能做：

| 功能 | 送去的内容 | 送到 |
|---|---|---|
| 摘要（手动或自动） | 条目标题加正文的纯文本，最多 `aiSummaryLimit` 个字符，加生效的摘要 prompt | `aiBaseUrl` |
| 翻译（手动或自动） | 条目标题加正文的纯文本，最多 60 000 个字符，按块发，加生效的翻译 prompt | `aiBaseUrl` |
| 标题翻译（自动） | 开了 `translateTitles` 的订阅里新条目的标题，每个最多 300 个字符，一批最多 40 个，加内置的标题 prompt | `aiBaseUrl` |
| 翻译所选、解释所选 | 所选的文字（最多 2000 个字符），解释还带文章标题（最多 200 个字符） | `aiBaseUrl` |

任何功能都不送：订阅地址和它的令牌、设置、口令、API key、阅读记录。送去的内容不含条目的链接和图片。

上限：

- 自动处理共用两个上限，所有自动和批量的功能算在一起，不是各算各的。上限数的是发给模型的请求个数，因为费用跟着请求走：一条摘要一个请求，一条翻译一块一个请求。
- 每天上限 `aiAutoDailyLimit`：服务端本地时间的一天里，自动处理最多发多少个请求，默认 200，1 到 10 000；每天零点重新数。已发出的请求计入 `ai_usage`，失败的请求也算（接口收到了就可能收费）。
- 每次抓取上限 `aiAutoFetchLimit`：一个订阅一次抓取里，最多给多少条新条目排自动任务，默认 10，1 到 100。新条目超过它时，给最新的排，其余的不排，这些条目的自动任务直接记为失败，错误码 `ai_cap_reached`。一条条目要排摘要和翻译两个任务时只占一条的名额。
- 用户自己点的请求不受上限约束，也不计入每天上限：上限是为了没人看着的时候别花钱，而用户点一下就是同意了这一次的花费。它们照样走受 `aiChunkLimit` 和 60 000 字符约束的原文大小，所以一次点击的费用是有界的。
- 到了每天上限，自动处理就停下：还在排队的自动任务记为失败，`ai_cap_reached`，之后新排的自动任务到处理时同样记为失败，不再发请求；一条翻译做到一半碰到上限，已翻完的块留在缓存里，这一条记为 `ai_cap_reached`。第二天零点之后新到的条目照常处理，被上限停下的条目不会补做，用户点「摘要」「翻译」或「重试」可以补上（已缓存的块不重发）。`ai_cap_reached` 不自动重试，也不占满 3 次的重试名额。
- 功能停下的时候必须说出来：条目的摘要或翻译卡片显示原因（见[「阅读器」](screens.md#阅读器)），设置的 AI 页显示今天用了多少个、是否已到上限（见[「设置界面」](screens.md#设置界面)）。

接口的位置：

- 设置的 AI 页直说内容会离开这台设备，发到配置的接口，并写明这个接口是否在服务端所在的这台电脑上。服务端用 `aiEndpointLocal` 回答：接口地址的主机是 `localhost`、以 `.localhost` 结尾、是 `127.0.0.0/8` 里的地址或 `::1` 时为 `true`，别的（含局域网地址和要解析的主机名）为 `false`。只看地址本身，不做 DNS 解析；没配置接口地址时为 `false`。
- 对批量的功能，建议用本机的模型（Ollama、LM Studio 等）。这只是建议，不强制：服务端不会因为接口不在本机就拒绝开关或请求。
