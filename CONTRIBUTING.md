# 参与贡献

Oriel 是单用户的原生桌面阅读器，主要由维护者和 AI 编码代理一起写。欢迎任何人提 issue 和 PR，流程在这一页。仓库的完整开发约束在 [AGENTS.md](AGENTS.md)，动手前读一遍。

## 开工前

先开 issue，再写代码。

- 标题 `[类型] 动词开头的一句话`，类型用 `ui`、`bug`、`feat`、`documentation`。
- 正文分「What's wrong」「Fix direction」「Done when」三段，issue 表单已经按这个排好。
- 新功能先等维护者确认再动手。范围写在 [docs/product.md](docs/product.md)「范围」：没有多用户、插件、推荐这些。范围外的 PR 可能被直接关掉。需要拍板的 issue 会加 `needs-decision` 标签，决定记在 issue 里（见 [GOVERNANCE.md](GOVERNANCE.md)）。
- 使用问题看 [SUPPORT.md](SUPPORT.md)。
- 安全漏洞不要公开提，见 [SECURITY.md](SECURITY.md)。

## 怎么改

1. 从 `main` 切分支，名字 `<类型>/<issue 号>-<短名>`，例如 `feat/12-timeline`。
2. 一个 PR 只做一件事。
3. 提交信息第一行 `#<issue 号> · [类型] 和 issue 标题同义的一句话`。每个提交用 `git commit -s` 签名：`Signed-off-by` 表示你同意 [Developer Certificate of Origin](https://developercertificate.org/)，DCO 检查没有它会失败。不想公开邮箱的话，用 GitHub 设置 → Emails 里的 noreply 地址。
4. 本地跑检查，Go 版本见 `.go-version`：

   ```bash
   scripts/check.sh --cross
   go test -race ./...
   ```

   `check.sh` 包括 gofmt、`go mod tidy` 无差异、vet、测试和六个目标的交叉编译。合并前必须过。
5. PR 标题同提交信息第一行，正文写 `Closes #<号>`，按模板填。PR 用 squash 合并，合并后删分支。`pr-title` 检查会拦格式不对的标题。
6. 行为变了，就改该区域已有的测试。用户能感知的改动写进 `CHANGELOG.md` 和 `CHANGELOG.zh.md`。

界面文案在各界面包的 `copy.json`，只有简体中文和英文，两种一起改。改产品行为先改 [docs/product.md](docs/product.md)。

## 界面改动

- 先改 `design/` 里的画板和 [docs/design-system.md](docs/design-system.md)，再改代码，三者放在同一个 PR。台账见 [design/README.md](design/README.md)。
- 只用 MyGo 原生控件和主题色。缺组件时，先在文档里补上它的规格和全部状态。
- 截图贴在 PR 正文里，浅色和深色、每个状态一张。不要把截图提交进仓库，图片会一直留在 git 历史里。

## AI 辅助

可以用 AI 工具。在 PR 里写明用了哪个、做了什么。你提交的每一行都要自己读懂并跑过，作为作者你对这个改动负责。

## License

提交贡献即表示你同意它按 [MIT](LICENSE) 授权。
