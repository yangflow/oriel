# Oriel

[![CI](https://github.com/yangflow/oriel/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yangflow/oriel/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

[English](README.md) | 中文

个人混合信息流阅读器：订阅 RSS 和 RSSHub，纯 Go 编写，原生界面。

文章、视频、社交动态、图片和社区帖子来自不同的地方。Oriel 把这五种内容放进同一条时间线，数据留在你自己的机器上，界面是原生应用，里面没有浏览器内核。一个 Go 程序既是应用，也是它的服务端：在电脑上，它打开窗口，只为自己提供服务；在家里的服务器或 VPS 上，它不开窗口运行，其他 Oriel 客户端连过来。数据就是你自己的一个 SQLite 文件。没有账号体系，没有推荐，自己也不带社交功能。

```text
┌──────────────────── oriel ────────────────────┐
│  原生窗口（MyGo）      ──HTTP──▶    服务端    │
│                                      │        │
│                       抓取循环 ──▶ SQLite     │
└───────────────────────────────────────────────┘
          oriel serve：同一个服务端，不开窗口
```

## 功能

### 阅读

- **一条时间线，五种形态**：文章行、视频封面、带图的社交动态、图片瀑布流、社区帖子三栏；每种形态还能换成网格或紧凑的行。
- **层层缩小范围**：形态、订阅和文件夹，「未读」「收藏」，时间范围，以及支持短语、排除、`from:`、`kind:`、`after:`、`before:` 的检索。
- **键盘优先**：按键移动、打开、收藏和标记条目；N 跳到下一条未读，? 列出所有按键，命令面板按名称找操作。全部标为已读可以撤销。
- **原生阅读器**：抽到全文就显示全文，抽不到就显示订阅里带的内容。可以在文章里查找、导出为 Markdown、看更新后改了什么、沉浸阅读，以及选米黄或雾灰的底色和行宽。
- **安静的时间线**：新条目先显示「N 条新内容」按钮；几个订阅带同一篇文章时只显示一次；条目可以「稍后提醒我」；何时算已读由你选。

### 订阅和抓取

- **几乎什么都能订**：订阅地址、RSSHub 路由、网站地址（自动找到订阅地址或路由），或者 YouTube、哔哩哔哩、X、GitHub、Telegram、微博的链接。
- **定时抓取**：支持 RSS、Atom、JSON Feed 和 RSSHub，带条件请求、失败退避、同一主机限流、WebSub 推送，一个 RSSHub 实例出错时换下一个。检查间隔按订阅的更新频率自动算。
- **每个订阅单独设置**：检查间隔、保留期限、节奏、每日上限、屏蔽词、正文抽取规则、请求头和代理。订阅健康会指出失效、失败、已搬家和太吵的订阅。
- **规则**：给命中的新条目加星标或标为已读；屏蔽词隐藏命中的条目，但不删除。

### AI

- **摘要和翻译**：走你自己的 OpenAI 兼容接口，例如 OpenAI、Ollama 或 LM Studio。可以在阅读器里按需生成，也可以让订阅自动为新条目生成；外语订阅还能翻译标题。API key 只存在服务端。

### 你的数据

- **一个 SQLite 文件**，带全文检索，驱动是纯 Go 的。
- **收藏的本地副本**：收藏的条目保留全文和图片，原页面失效后仍能看。
- **导入导出**：OPML 导入和导出；从 Miniflux 和 FreshRSS 导入订阅、星标和已读状态。
- **备份**：在设置里或用 `oriel backup` 存成一个 `.oriel-backup` 文件，从设置或 `oriel restore` 恢复，也可以定时备份到文件夹。登录会话和 AI key 不在文件里。
- **诊断文件**：不含密钥，用于报告问题。

### 运行方式

- **桌面应用**只在本机回环地址上提供服务；**`oriel serve`** 无界面运行，支持口令登录和按设备管理会话；桌面应用也可以连接远程服务端。
- **桌面集成**：登录时打开，菜单栏或托盘以及 Dock 上显示未读数，关掉窗口后继续更新。
- **英文和简体中文**界面。

计划中的工作见 [milestone](https://github.com/yangflow/oriel/milestones)。

## 项目状态

早期开发中。发布版见 [Releases 页面](https://github.com/yangflow/oriel/releases)，装好的应用在你确认后自动更新。macOS 应用经过签名和公证；Windows 安装包暂时没有签名，第一次运行时 SmartScreen 会提示。

目标平台：macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 版本需要 glibc，无界面运行也一样。

## 从源码构建

需要 `.go-version` 里的 Go 版本和 Python 3：

```bash
git clone https://github.com/yangflow/oriel
cd oriel
go tool mygo dev           # 运行桌面应用
go tool mygo build         # 打包到 build/
```

## 无界面运行

```bash
ORIEL_PASSWORD=<口令> go run . serve -data ./data -port 7470
```

- 不设 `ORIEL_PASSWORD` 时，服务端只监听 `127.0.0.1`。口令只从环境变量读取，不会出现在进程列表里。
- `-data` 默认是用户数据目录。
- 放在反向代理后面时，加 `-trusted-proxy <IP或CIDR,...>`，来自这些地址的请求才按 `X-Forwarded-For` 给登录限速。只写你自己的代理；`/0` 会被拒绝。

### Docker 或 systemd

文件在 [`deploy/`](deploy/)，数据目录、端口、反向代理和 HTTPS、健康检查、备份、升级的细节在 [docs/product.md](docs/product.md)「部署」一节。镜像还没有发布到注册表，先从源码目录构建，支持 `linux/amd64` 和 `linux/arm64`：

```bash
echo 'ORIEL_PASSWORD=<口令>' > deploy/.env && chmod 600 deploy/.env
docker compose -f deploy/compose.yaml up -d --build
```

- 示例只把端口发布在宿主机的 `127.0.0.1` 上；要让其他设备连，改 `ports`，最好放在 HTTPS 反向代理后面。
- 容器里必须设口令：不设时服务端只在容器内部监听。
- 镜像以非 root 用户运行，数据放在卷 `/data` 里，用 `oriel healthcheck` 回答 Docker 的健康检查。
- systemd 的话，安装 `deploy/oriel.service`，口令放在权限 `0600` 的环境文件里；服务以动态用户运行，数据在 `/var/lib/oriel`。

## 开发

```bash
scripts/check.sh                  # 拉取请求跑的检查：gofmt、go mod tidy、vet、测试，竞态检测只覆盖有并发的包
scripts/check.sh --cross --race   # 全部，CI 在 main 每次推送和每天各跑一遍：全部包的竞态检测，以及六个目标平台
```

| 内容 | 位置 |
|---|---|
| 产品行为、数据模型、接口、抓取和登录 | [docs/product.md](docs/product.md) |
| 所有界面共用的设计规则（英文） | [docs/design.md](docs/design.md) |
| 界面画板、设计系统和设计令牌 | [design/README.md](design/README.md) |
| 架构决定（英文） | [docs/adr/](docs/adr/README.md) |
| 发版（英文） | [docs/release.md](docs/release.md) |
| 人和代理修改仓库的规则（英文） | [AGENTS.md](AGENTS.md) |

## 参与贡献

欢迎用中文或英文提 issue 和 PR，见 [CONTRIBUTING.md](CONTRIBUTING.md)（英文）。漏洞请按 [SECURITY.md](SECURITY.md) 私下报告，不要开公开 issue。

## 许可证

[MIT](LICENSE)。依赖的许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。
