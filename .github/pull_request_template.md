## What

<!-- 一两句话。标题写成 `#<issue 号> · [类型] 一句话`。 -->

Closes #

## Why

## How to verify

- [ ] `scripts/check.sh --cross`
- [ ] `go test -race ./...`
- [ ] 界面有变化：截图贴在这个 PR 正文里（浅色和深色、每个状态一张），没有提交进仓库
- [ ] 其他你手动验证过的步骤：

## Checklist

- [ ] 正文有 `Closes #<号>`
- [ ] 每个提交都用 `git commit -s` 签了名
- [ ] 视觉或交互有变化：`design/` 画板、`docs/design-system.md` 和代码在同一个 PR 里
- [ ] 界面文案中英文都改了（各界面包的 `copy.json`）
- [ ] 产品行为有变化：先改了 `docs/product.md`
- [ ] 用户能感知的改动写进了 `CHANGELOG.md` 和 `CHANGELOG.zh.md`
- [ ] 用了 AI 工具的话，在这里写明哪个工具、做了什么
