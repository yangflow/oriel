# 治理

## 角色

- **维护者**（[@yangflow](https://github.com/yangflow)）：决定 Oriel 做什么，审阅并合并每个 PR。只有维护者合并进 `main`。
- **AI 协作者**：替维护者干活的编码代理（见 [AGENTS.md](AGENTS.md)）。它们起草 issue、代码和 PR。维护者对它们的产出负责。
- **贡献者**：提过 issue 或 PR 的任何人（见 [CONTRIBUTING.md](CONTRIBUTING.md)）。

## 决策

产品范围以 [docs/product.md](docs/product.md) 为准，范围外的需求会被关掉。

日常决定在 issue 和 PR 里做。需要人拍板的 issue 加 `needs-decision` 标签；定了以后，在 issue 里记下结论，再去掉标签。影响架构的决定另写一份 ADR，放在 [docs/adr/](docs/adr/README.md)。

## 流程

先开 issue，再开 PR，PR 用 `Closes #<号>` 关联，squash 合并。细节在 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 发布

还没有公开发布。签名、更新源和发布流程配好之前，`package` 工作流只出开发用的未签名包。

## 行为

[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) 适用于整个项目，由维护者执行。
