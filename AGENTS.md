# 开发约束

本文件约束人和代理怎么改这个仓库，不重复产品说明。

代理读到的文件名是 `AGENTS.md`。`CLAUDE.md` 只有一行 `@AGENTS.md`，不要往里写别的，也不要另起一份。

## 基线

- `docs/product.md`：产品行为、数据模型、流程。
- `docs/design-system.md`：颜色、字体、间距、组件。
- `design/`：每个界面的画板和状态，台账在 `design/README.md`。

产品行为以 `docs/product.md` 为准。新功能的规则先写进本仓库的文档，再写代码。

架构见 `docs/adr/0002-standalone-client-and-server.md`：客户端只通过 HTTP API 读写数据，不直接碰存储；服务端未设鉴权时只听 `127.0.0.1`。

文档和代码冲突时，先判断是哪边错了：文档已经改了决定就改代码；文档仍有效而代码悄悄退让也改代码；文档本身会让人做错事才改文档。

## 工作流程

1. 先开 issue。标题 `[类型] 动词开头的一句话`，类型用 `ui`、`bug`、`feat`、`documentation`；正文分「What's wrong」「Fix direction」「Done when」三段。要人拍板的加 `needs-decision` 标签，定了以后在 issue 里记下结论，再去掉标签。
2. 分支 `<类型>/<issue 号>-<短名>`，例如 `feat/12-timeline`。
3. 提交信息第一行 `#<issue 号> · [类型] <和 issue 标题同义的一句话>`。每个提交用 `git commit -s` 签名（DCO 检查会拦）。作者邮箱用 GitHub 的 noreply 地址 `269347247+yangflow@users.noreply.github.com`：克隆后先 `git config user.email` 设好，不用个人邮箱和本机的全局配置。
4. PR 标题同上，正文写 `Closes #<号>`，squash 合并，合并后删分支。
5. 改视觉或交互：先改画板和 `docs/design-system.md`，再改代码，放在同一个 PR。改产品行为：先改 `docs/product.md`。
6. 合并前 `scripts/check.sh --cross` 要过。用户能感知的改动写进 `CHANGELOG.md` 和 `CHANGELOG.zh.md` 的 `[Unreleased]`。
7. `README.md`（英文）和 `README.zh-CN.md`（中文）内容保持一致，改一份就同时改另一份。功能状态只写已经合并的，计划中的写明是计划。

## 目录

- `main.go`：分派桌面模式和无窗口模式（`oriel serve`）；生命周期、菜单、窗口、原生更新插件。
- `backend.go`：打开数据库、启动和关闭 HTTP 服务端，两种模式共用。
- `internal/store/`：SQLite 和全部查询。迁移在 `internal/store/migrations/`，只加新文件，不改已发布的。
- `internal/fetch/`：抓取循环、订阅解析、失败退避、正文抽取和过期清理，由 `backend.go` 启动。条目链接一律走 `guardedClient`，不要换成普通的 `http.Client`。
- `internal/server/`：HTTP API 和鉴权。客户端只走这层，不直接 import `internal/store`。
- `internal/ui/<界面>/`：一个界面一个包，放状态、文案目录和无头交互测试。目录名和 `design/screens/<界面>.html` 对应。
- `internal/ui/locale/`：系统语言到界面语言（简体中文、英文）的映射，所有文案目录共用。
- `internal/ui/startup/`：桌面模式启动失败时原生错误框的文案。
- `resources/`：随包分发的许可证和产品资源。
- `scripts/`：检查、版本号、打包。
- `mygo.json`：名称、identifier、版本号、打包配置。版本号只写在这里，用 `python3 scripts/bump.py <版本>` 改，它会同时给两份 changelog 定日期。

## 代码

- 只用 MyGo 原生视图和原生更新插件。不引入 WebView、HTML 前端、cgo 生产依赖或前端工具链。
- 绘制路径里不做磁盘和网络操作。耗时工作放到后台，结果回到 UI 线程再改状态。
- 界面文案放在各界面包的 `copy.json`，只有 `en` 和 `zh-Hans`，两种一起改。不加其他语言。
- 行为变了，就改该区域已有的测试。无头测试通过不等于真机验收。
- 不因为文件长就拆。不留临时脚本、调试输出和注释掉的代码。
- MyGo 库和命令行工具是同一个模块，在 `go.mod` 里一起升级。依赖变了同步改 `THIRD_PARTY.md` 和 `resources/ThirdPartyLicenses/`。

## 红线

- 口令、token、API key、签名私钥不出现在代码、PR、issue、日志和截图里。`scripts/scan_secrets.py` 在 CI 里扫全部历史。
- 签名、发布、往外部服务发数据，先问维护者。
- 提交前看清暂存的文件列表，不用 `git add -A`。
- 个人邮箱、本机用户名和本机路径不进提交信息、文件和日志。
