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

服务端已经能用的：

- **定时抓取**：每 15 秒抓取到期的订阅，带条件请求（ETag、Last-Modified），失败后退避重试。支持 RSS、Atom、JSON Feed 和 RSSHub 路由。
- **文章全文**：用 Readability 抽取文章正文；Discourse 论坛只取链接指向的那一楼。订阅里的链接访问不到本机和内网地址。
- **保留期限**：每小时清理超过设定天数且未收藏的条目；设为 0 则永久保留。
- **两种运行模式**：桌面应用只在本机回环地址上提供服务；`oriel serve` 用于无界面的主机，支持口令登录和按设备管理会话。
- **一个存储文件**：带全文检索的 SQLite，驱动是纯 Go 的，生产代码不用 cgo。窗口只通过 HTTP API 读写数据，所以桌面应用和无界面服务器的行为一致。
- **两种语言**：界面提供英文和简体中文。

按[路线图](docs/roadmap.md)的顺序计划中的：添加和编辑订阅、按四种形态排版的时间线、文章阅读器、通过你自己的 OpenAI 兼容接口按需生成摘要和翻译、设置、OPML 导入导出。

项目状态：早期开发中，版本 0.1.0，还没有发布。窗口里仍是模板自带的计数示例；订阅和设置还没有接口和界面。目标平台：macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 版本需要 glibc，无界面运行也一样。

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
| 颜色、字体、间距、组件 | [docs/design-system.md](docs/design-system.md) |
| 界面画板 | [design/README.md](design/README.md) |
| 开发顺序 | [docs/roadmap.md](docs/roadmap.md) |
| 架构决定 | [docs/adr/](docs/adr/README.md) |
| 人和代理修改仓库的规则（英文） | [AGENTS.md](AGENTS.md) |

## 参与贡献

欢迎用中文或英文提 issue 和 PR，见 [CONTRIBUTING.md](CONTRIBUTING.md)（英文）。提交信息和 PR 标题用 [Conventional Commits](https://www.conventionalcommits.org/)，不需要 DCO 签名。漏洞请按 [SECURITY.md](SECURITY.md) 私下报告，不要开公开 issue。

## 许可证

[MIT](LICENSE)。依赖的许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。
