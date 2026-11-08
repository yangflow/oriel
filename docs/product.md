# 产品

Oriel 的产品行为、数据模型和接口。界面长什么样只看设计稿（[design/README.md](../design/README.md)），这里只写行为。

## 定位

Oriel 是单用户的混合信息流阅读器：订阅原生 RSS 和 RSSHub，一条时间线混排文章、视频、社交、图片和社区帖子，摘要和翻译按需走用户自己的 OpenAI 兼容接口。它用 Go 和 MyGo 原生 UI，不用 WebView。

Oriel 只负责接收：内容只经订阅进来（RSS、Atom、JSON Feed 和 RSSHub），不登录原站账号，不调用平台的私有接口，也不往外发布内容。新功能先对照这一条，越过它的不做。

## 范围

做的范围：

- 时间线混排五种形态：`article`、`video`、`social`、`image`、`forum`（社区）。形态按源决定，条目跟随源。
- 添加订阅：博客 RSS、YouTube / 哔哩哔哩 / X / GitHub / Telegram / 微博链接，或 RSSHub 路径。
- 文章阅读器：全文抽取，抽不到就显示 RSS 正文并标明不是全文。
- 视频和社交条目显示封面和摘要，点开交给系统浏览器打开原站；社交和图片条目的图片可以在应用内的看图层里看（见[「时间线」](product/screens.md#时间线)的「看图」）。没有 WebView，所以不内嵌官方播放器。
- 按需 AI 摘要和翻译，prompt 可全局改，也可按源覆盖。
- 导入、导出 OPML。

不做：多用户、发现台、推荐、高亮笔记、插件、MCP、播客、邮件 inbox、自建播放器、登录原站账号。

## 架构

同一个程序既是服务端又是客户端（见 [ADR 0002](adr/0002-standalone-client-and-server.md)）：

- 服务端负责抓取、正文抽取、存储、AI 调用和清理，并提供 HTTP API。
- 客户端是原生界面，只通过 HTTP API 读写数据。
- 桌面模式在本机启动服务端、只监听 `127.0.0.1`，再打开窗口。无窗口模式只跑服务端，供其他设备上的 Oriel 客户端连接。
- 桌面模式没有首次设置，打开就进时间线。也可以改连另一台电脑上的无窗口模式：填服务端地址和口令登录，口令只在登录时发送，不保存。

## 文档

| 文件 | 内容 | 章节 |
|---|---|---|
| [运行和部署](product/running.md) | 桌面和无窗口两种运行方式、部署、登录、日志、诊断和更新。 | [运行模式](product/running.md#运行模式)、[桌面集成](product/running.md#桌面集成)、[连接远程服务端](product/running.md#连接远程服务端)、[部署](product/running.md#部署)、[容器](product/running.md#容器)、[系统服务](product/running.md#系统服务)、[发布镜像](product/running.md#发布镜像)、[鉴权](product/running.md#鉴权)、[日志文件](product/running.md#日志文件)、[诊断文件](product/running.md#诊断文件)、[更新](product/running.md#更新) |
| [数据](product/data.md) | 数据模型、存储、删除与撤销、保留期限和收藏的本地副本。 | [数据模型](product/data.md#数据模型)、[源](product/data.md#源)、[文件夹](product/data.md#文件夹)、[订阅健康](product/data.md#订阅健康)、[抓取记录](product/data.md#抓取记录)、[RSSHub 实例健康](product/data.md#rsshub-实例健康)、[条目](product/data.md#条目)、[条目的修订](product/data.md#条目的修订)、[同一篇文章只显示一次](product/data.md#同一篇文章只显示一次)、[规则](product/data.md#规则)、[语言检测](product/data.md#语言检测)、[阅读时间](product/data.md#阅读时间)、[时间范围](product/data.md#时间范围)、[AI 结果](product/data.md#ai-结果)、[设置和会话](product/data.md#设置和会话)、[存储](product/data.md#存储)、[删除订阅与撤销](product/data.md#删除订阅与撤销)、[保留期限](product/data.md#保留期限)、[数据库维护](product/data.md#数据库维护)、[收藏的本地副本](product/data.md#收藏的本地副本) |
| [抓取](product/fetching.md) | 抓取循环、订阅发现、RSSHub 路由、WebSub 和正文抽取。 | [抓取](product/fetching.md#抓取)、[自定义请求头](product/fetching.md#自定义请求头)、[代理](product/fetching.md#代理)、[订阅发现](product/fetching.md#订阅发现)、[RSSHub 路由查找](product/fetching.md#rsshub-路由查找)、[WebSub 推送](product/fetching.md#websub-推送)、[正文抽取](product/fetching.md#正文抽取) |
| [时间线规则](product/timeline.md) | 节奏、隐藏、文件夹时间线、每日上限、检索语法和规则。 | [节奏](product/timeline.md#节奏)、[不在全部里出现](product/timeline.md#不在全部里出现)、[文件夹时间线](product/timeline.md#文件夹时间线)、[每日上限](product/timeline.md#每日上限)、[检索语法](product/timeline.md#检索语法)、[规则](product/timeline.md#规则)、[规则接口](product/timeline.md#规则接口) |
| [接口](product/api.md) | HTTP 约定和订阅、时间线、设置、图片代理、活动和存储的接口。 | [HTTP 约定](product/api.md#http-约定)、[订阅接口](product/api.md#订阅接口)、[时间线接口](product/api.md#时间线接口)、[稍后提醒我](product/api.md#稍后提醒我)、[屏蔽词](product/api.md#屏蔽词)、[设置接口](product/api.md#设置接口)、[图片代理](product/api.md#图片代理)、[活动和存储接口](product/api.md#活动和存储接口) |
| [导入导出和备份](product/import-export.md) | OPML、备份与恢复，以及从别的阅读器导入。 | [OPML 接口](product/import-export.md#opml-接口)、[导入任务](product/import-export.md#导入任务)、[备份与恢复](product/import-export.md#备份与恢复)、[备份文件](product/import-export.md#备份文件)、[接口](product/import-export.md#接口)、[恢复的步骤](product/import-export.md#恢复的步骤)、[命令行](product/import-export.md#命令行)、[自动备份](product/import-export.md#自动备份)、[导入其他阅读器接口](product/import-export.md#导入其他阅读器接口)、[凭据](product/import-export.md#凭据)、[请求方式](product/import-export.md#请求方式)、[调用](product/import-export.md#调用)、[匹配和写入](product/import-export.md#匹配和写入) |
| [AI](product/ai.md) | 摘要和翻译，以及发送与费用。 | [AI 摘要和翻译](product/ai.md#ai-摘要和翻译)、[标题翻译](product/ai.md#标题翻译)、[发送与费用](product/ai.md#发送与费用) |
| [界面行为](product/screens.md) | 各个界面的行为：规则、状态、按键、保存和失败。样子看设计稿。 | [当前界面](product/screens.md#当前界面)、[时间线](product/screens.md#时间线)、[阅读器](product/screens.md#阅读器)、[订阅管理](product/screens.md#订阅管理)、[文件夹的管理](product/screens.md#文件夹的管理)、[设置 → 订阅](product/screens.md#设置--订阅)、[设置界面](product/screens.md#设置界面) |

## 技术基线

Go 1.27.3，MyGo v0.7.4，SQLite 用纯 Go 的 `modernc.org/sqlite`，生产代码不用 cgo，没有前端工具链。macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 程序需要 glibc（MyGo 动态加载系统库），无窗口模式也一样：可以跑在不带 GTK 的 Debian 等系统上，但不能跑在 Alpine 等 musl 系统上。界面只有简体中文和英文：默认跟随系统，中文系统（含繁体）显示简体中文，其他系统显示英文，也可以在「外观」里固定其中一种。没在真机上测过的性能不写进文档。
