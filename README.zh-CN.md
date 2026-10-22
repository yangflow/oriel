# Oriel

[![CI](https://github.com/yangflow/oriel/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yangflow/oriel/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

[English](README.md) | 中文

个人混合信息流阅读器：订阅 RSS 和 RSSHub，纯 Go 编写，原生界面。

文章、视频、社交动态和图片来自不同的地方。多数阅读器要么把它们压成一列链接，要么要你注册云端账号。Oriel 把这四种内容放进同一条时间线，数据留在你自己的机器上，界面是原生应用，里面没有浏览器内核。一个 Go 程序既是应用，也是它的服务端：在电脑上，它打开窗口，只为自己提供服务；在家里的服务器或 VPS 上，它不开窗口运行，其他 Oriel 客户端连过来。数据在你自己的一个 SQLite 文件里。没有账号体系，没有推荐，没有社区。

```text
┌──────────────────── oriel ────────────────────┐
│  原生窗口（MyGo）      ──HTTP──▶    服务端    │
│                                      │        │
│                       抓取循环 ──▶ SQLite     │
└───────────────────────────────────────────────┘
          oriel serve：同一个服务端，不开窗口
```

## 功能

已经能用的：

- **定时抓取**：几个工作线程不停地抓取到期的订阅，带条件请求（ETag、Last-Modified），失败后退避重试，同一主机限制并发请求。支持 RSS、Atom、JSON Feed 和 RSSHub 路由，一个 RSSHub 实例出错时换列表里的下一个。
- **文章全文**：用 Readability 抽取文章正文；Discourse 论坛只取链接指向的那一楼。订阅里的链接访问不到本机和内网地址。
- **保留期限**：每小时清理超过设定天数且未收藏的条目；设为 0 则永久保留。
- **收藏的本地副本**：收藏条目时，把它的全文和图片（最多 30 张，每张不超过 8 MiB）存进数据库，原页面失效后仍能看，数据库备份也会带上；取消收藏时删除副本。
- **订阅接口**：通过 HTTP 添加、列出、修改、暂停、删除和「更新」订阅（可以一个个更新，也可以全部更新），带字段校验和重复检查。
- **时间线接口**：从新到旧翻页浏览所有条目，按源、形态、未读或收藏筛选，全文检索，标记已读和收藏；按源设屏蔽词，隐藏命中的条目但不删除。
- **时间线界面**：所有订阅混在一个列表里从新到旧排，每种形态有自己的排版：文章行带缩略图，视频封面带时长，社交动态最多显示四张图，图片是瀑布流。形态列表和订阅列表缩小范围，「未读」「收藏」和检索再筛一层。可以用按键移动、打开、收藏和标记条目，按 N 跳到下一条未读，按 ? 列出所有按键；全部标为已读可以撤销。新条目到了先显示「N 条新内容」按钮，列表不动。几个订阅带同一篇文章时只显示一行，用一个标签列出其他来源（可在设置里关闭）。每个订阅显示它网站的图标。视频、社交动态和图片在浏览器里打开。
- **文章阅读器**：文章在列表的位置打开，原生绘制标题、段落、列表、引用、代码、链接和图片。抽到全文就显示全文，抽不到就显示订阅里带的内容，并标明不是全文、给出原文链接。可以用按键切到下一篇、收藏、标记已读和回到列表。
- **订阅界面**：填订阅地址、RSSHub 路由，或者 YouTube、哔哩哔哩、X、GitHub、Telegram、微博的链接都能添加，链接会转成对应的订阅地址或路由；形态和检查间隔（默认自动，按订阅的更新频率算）在同一个对话框里选。在侧栏右键菜单或工具栏的铅笔按钮里编辑、暂停、更新和删除订阅；删除前会先确认，因为它的条目也会一起删掉。「订阅 > 全部更新」（Cmd/Ctrl+R，命令面板里也有）立刻检查所有未暂停的订阅，进度显示在侧栏，查完后给出提示。
- **设置**：从服务端栏旁边的齿轮或「设置…」（Cmd/Ctrl+,）修改 RSSHub 实例（有序列表）和条目保留多久；设置存在服务端，连到它的设备都一样。连的是设了口令的服务端时，能看到登录过的每台设备，让不再用的退出登录。
- **AI 摘要和翻译**：走你自己的 OpenAI 兼容接口（例如 OpenAI 或本机的 Ollama），在设置里配好，有 Ollama 和 LM Studio 的预设，连接自检会指出问题在哪（地址、key、模型名、上下文大小）；API key 存在服务端，存好后不再显示。阅读器里按需为当前文章生成摘要或翻译，可以取消，结果留着下次直接用；订阅可以自动为新条目生成摘要或翻译，并用自己的 prompt。
- **OPML 导入导出**：从别的阅读器导入订阅（支持嵌套分组），看到哪些没能添加、为什么；导出所有订阅，文件里保留形态和 RSSHub 路径。
- **备份与恢复**：在设置里或用 `oriel backup` 把全部内容（订阅、条目、已读和星标、AI 结果、设置）存成一个 `.oriel-backup` 文件，再从设置、空的时间线或 `oriel restore` 恢复。登录会话和 AI 的 API key 不在文件里。
- **自动备份**：在设置里打开后，每天或每周把备份存进你选的文件夹；服务端只保留最新的几份（默认 7 份），只删它自己写的旧文件。
- **两种运行模式**：桌面应用只在本机回环地址上提供服务；`oriel serve` 用于无界面的主机，支持口令登录和按设备管理会话。
- **连接远程服务端**：桌面应用可以从应用菜单的「连接服务端…」改连另一台电脑上的 `oriel serve`。只保存会话 token，只有你能读；口令从不保存。
- **一个存储文件**：带全文检索的 SQLite，驱动是纯 Go 的，生产代码不用 cgo。窗口只通过 HTTP API 读写数据，所以桌面应用和无界面服务器的行为一致。
- **两种语言**：界面提供英文和简体中文。

按 [milestone](https://github.com/yangflow/oriel/milestones) 的顺序计划中的（规则见[路线图](docs/roadmap.md)）：0.5 打磨与修复、0.6 阅读体验、0.7 订阅管理、0.8 数据安全、0.9 优先级与自动化。

项目状态：早期开发中，发布版见 [Releases 页面](https://github.com/yangflow/oriel/releases)。窗口里有连接界面、时间线、订阅对话框、文章阅读器和设置。目标平台：macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 版本需要 glibc，无界面运行也一样。

发布版从版本 tag 构建并发布（见 [docs/release.md](docs/release.md)）。装好的应用会从发布版自动更新，安装前先在更新窗口里确认。macOS 应用经过签名和公证；Windows 安装包暂时没有签名，第一次运行时 SmartScreen 会提示。

## 快速开始

从源码构建，需要 `.go-version` 里的 Go 版本和 Python 3：

```bash
git clone https://github.com/yangflow/oriel
cd oriel
go tool mygo dev           # 运行桌面应用
go tool mygo build         # 打包到 build/
```

## 使用

不开窗口运行服务端：

```bash
ORIEL_PASSWORD=<口令> go run . serve -data ./data -port 7470
```

不设 `ORIEL_PASSWORD` 时，服务端只监听 `127.0.0.1`。口令只从环境变量读取，不会出现在进程列表里。`-data` 默认是用户数据目录。

## 开发

```bash
scripts/check.sh --cross   # gofmt、go mod tidy、vet、测试、脚本测试，以及六个目标平台的交叉编译
go test -race ./...
```

| 内容 | 位置 |
|---|---|
| 产品行为、数据模型、抓取、HTTP 和登录 | [docs/product.md](docs/product.md) |
| 设计语言：布局、颜色、字体、组件、无障碍（英文） | [docs/design.md](docs/design.md) |
| 界面画板 | [design/README.md](design/README.md) |
| 路线图规则和 milestone | [docs/roadmap.md](docs/roadmap.md) |
| 架构决定（英文） | [docs/adr/](docs/adr/README.md) |
| 人和代理修改仓库的规则（英文） | [AGENTS.md](AGENTS.md) |

## 参与贡献

欢迎用中文或英文提 issue 和 PR，见 [CONTRIBUTING.md](CONTRIBUTING.md)（英文）。提交信息和 PR 标题用 [Conventional Commits](https://www.conventionalcommits.org/)，不需要 DCO 签名。漏洞请按 [SECURITY.md](SECURITY.md) 私下报告，不要开公开 issue。

## 许可证

[MIT](LICENSE)。依赖的许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。
