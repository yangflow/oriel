<h1 align="center">Oriel</h1>

<p align="center">
  个人混合信息流阅读器：订阅 RSS 和 RSSHub，纯 Go 编写，原生界面。
</p>

<p align="center">
  <a href="https://github.com/yangflow/oriel/actions/workflows/ci.yml"><img src="https://github.com/yangflow/oriel/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/yangflow/oriel" alt="License: MIT"></a>
</p>

<p align="center">
  <a href="README.md">English</a> · <b>简体中文</b>
</p>

## 为什么做 Oriel

文章、视频、社交动态和图片来自不同的地方。多数阅读器要么把它们压成一列链接，要么要你注册云端账号。Oriel 把这四种内容放进同一条时间线，数据留在你自己的机器上，界面是原生应用，里面没有浏览器内核。

一个 Go 程序既是应用，也是它的服务端。在电脑上，它打开窗口，只为自己提供服务；在家里的服务器或 VPS 上，它不开窗口运行，其他 Oriel 客户端连过来。数据在你自己的一个 SQLite 文件里。没有账号体系，没有推荐，没有社区。

## 功能

服务端已经能用的：

- **定时抓取**：每 15 秒抓取到期的订阅，带条件请求（ETag、Last-Modified），失败后退避重试。支持 RSS、Atom、JSON Feed 和 RSSHub 路由。
- **文章全文**：用 Readability 抽取文章正文；Discourse 论坛只取链接指向的那一楼。订阅里的链接访问不到本机和内网地址。
- **保留期限**：每小时清理超过设定天数且未收藏的条目；设为 0 则永久保留。
- **两种运行模式**：桌面应用只在本机回环地址上提供服务；`oriel serve` 用于无界面的主机，支持口令登录和按设备管理会话。

按[路线图](docs/roadmap.md)的顺序计划中的：添加和编辑订阅、按四种形态排版的时间线、文章阅读器、通过你自己的 OpenAI 兼容接口按需生成摘要和翻译、设置、OPML 导入导出。

## 工作原理

```text
┌──────────────────── oriel ────────────────────┐
│  原生窗口（MyGo）      ──HTTP──▶    服务端    │
│                                      │        │
│                       抓取循环 ──▶ SQLite     │
└───────────────────────────────────────────────┘
          oriel serve：同一个服务端，不开窗口
```

- 窗口只通过 HTTP API 读写数据，所以桌面应用和无界面服务器的行为一致。
- 存储是一个带全文检索的 SQLite 文件，驱动是纯 Go 的，生产代码不用 cgo。
- 界面提供英文和简体中文。

## 项目状态

早期开发中，版本 0.1.0，还没有发布。上面列出的服务端部分已经可用，也有测试；窗口里仍是模板自带的计数示例。订阅和设置还没有接口和界面。

目标平台：macOS 12+、Windows 10/11、带 GTK 3 的 Linux，amd64 和 arm64。Linux 版本需要 glibc，无界面运行也一样。

## 快速开始

从源码构建，需要 Go 1.27.1（见 `.go-version`）和 Python 3：

```bash
git clone https://github.com/yangflow/oriel
cd oriel
scripts/check.sh --cross   # 格式、vet、测试，以及六个目标平台的交叉编译
go tool mygo dev           # 运行桌面应用
go tool mygo build         # 打包到 build/
```

不开窗口运行：

```bash
ORIEL_PASSWORD=<口令> go run . serve -data ./data -port 7470
```

不设 `ORIEL_PASSWORD` 时，服务端只监听 `127.0.0.1`。口令只从环境变量读取，不会出现在进程列表里。

## 安全

漏洞请按 [SECURITY.md](SECURITY.md) 私下报告，不要开公开 issue。

## 文档

| 内容 | 位置 |
|---|---|
| 产品行为、数据模型、抓取、HTTP 和登录 | [docs/product.md](docs/product.md) |
| 颜色、字体、间距、组件 | [docs/design-system.md](docs/design-system.md) |
| 各个界面和它们的状态 | [design/README.md](design/README.md) |
| 开发顺序 | [docs/roadmap.md](docs/roadmap.md) |
| 架构决定 | [docs/adr/](docs/adr/README.md) |
| 修改这个仓库的规则 | [AGENTS.md](AGENTS.md) |

## 参与贡献

欢迎提 issue 和 PR，动手前先读 [CONTRIBUTING.md](CONTRIBUTING.md)。每个提交都要用 `git commit -s` 签名（Developer Certificate of Origin）。

## 获取帮助

见 [SUPPORT.md](SUPPORT.md)。问题统一走 issue；没有开 Discussions，也没有付费支持。

## 许可证

[MIT](LICENSE)。依赖的许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。
