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

- **定时抓取**：每 15 秒抓取到期的订阅，带条件请求（ETag、Last-Modified），失败后退避重试。支持 RSS、Atom、JSON Feed 和 RSSHub 路由。
- **文章全文**：用 Readability 抽取文章正文；Discourse 论坛只取链接指向的那一楼。订阅里的链接访问不到本机和内网地址。
- **保留期限**：每小时清理超过设定天数且未收藏的条目；设为 0 则永久保留。
- **订阅接口**：通过 HTTP 添加、列出、修改、暂停、删除和「更新」订阅，带字段校验和重复检查。
- **时间线接口**：从新到旧翻页浏览所有条目，按源、形态、未读或收藏筛选，全文检索，标记已读和收藏；按源设屏蔽词，隐藏命中的条目但不删除。
- **时间线界面**：所有订阅混在一个列表里从新到旧排，每种形态有自己的排版：文章行带缩略图，视频封面带时长，社交动态最多显示四张图，图片是瀑布流。形态标签和订阅列表缩小范围，「未读」「收藏」和检索再筛一层。可以用按键移动、打开、收藏和标记条目；新条目到了先显示「N 条新内容」按钮，列表不动。视频、社交动态和图片在浏览器里打开。
- **文章阅读器**：文章在列表的位置打开，原生绘制标题、段落、列表、引用、代码、链接和图片。抽到全文就显示全文，抽不到就显示订阅里带的内容，并标明不是全文、给出原文链接。可以用按键切到下一篇、收藏、标记已读和回到列表。
- **订阅界面**：填订阅地址、RSSHub 路由，或者 YouTube、哔哩哔哩、X、GitHub、Telegram、微博的链接都能添加，链接会转成对应的订阅地址或路由；形态和检查间隔在同一个对话框里选。在侧栏右键菜单或工具栏的铅笔按钮里编辑、暂停、更新和删除订阅；删除前会先确认，因为它的条目也会一起删掉。
- **设置**：从服务端栏旁边的齿轮或「设置…」（Cmd/Ctrl+,）修改 RSSHub 实例和条目保留多久；设置存在服务端，连到它的设备都一样。连的是设了口令的服务端时，能看到登录过的每台设备，吊销不再用的。
- **AI 摘要和翻译**：走你自己的 OpenAI 兼容接口（例如 OpenAI 或本机的 Ollama），在设置里配好并测试连接；API key 存在服务端，存好后不再显示。阅读器里按需为当前文章生成摘要或翻译，可以取消，结果留着下次直接用；订阅可以自动为新条目生成摘要或翻译，并用自己的 prompt。
- **OPML 导入导出**：从别的阅读器导入订阅（支持嵌套分组），看到哪些没能添加、为什么；导出所有订阅，文件里保留形态和 RSSHub 路径。
- **两种运行模式**：桌面应用只在本机回环地址上提供服务；`oriel serve` 用于无界面的主机，支持口令登录和按设备管理会话。
- **连接远程服务端**：桌面应用可以从应用菜单的「连接服务端…」改连另一台电脑上的 `oriel serve`。只保存会话 token，只有你能读；口令从不保存。
- **一个存储文件**：带全文检索的 SQLite，驱动是纯 Go 的，生产代码不用 cgo。窗口只通过 HTTP API 读写数据，所以桌面应用和无界面服务器的行为一致。
- **两种语言**：界面提供英文和简体中文。

按 [milestone](https://github.com/yangflow/oriel/milestones) 的顺序计划中的（规则见[路线图](docs/roadmap.md)）：第一个正式版本 1.0。

项目状态：早期开发中，版本 0.1.0，还没有发布。窗口里有连接界面、时间线、订阅对话框、文章阅读器和设置。目标平台：macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 版本需要 glibc，无界面运行也一样。

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
