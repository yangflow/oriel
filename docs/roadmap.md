# 路线图

规划和进度都在 GitHub：

- 一个 [milestone](https://github.com/yangflow/oriel/milestones) 是一个主题，也是一次发版，标题里的版本号就是发版时的版本号；按版本号顺序做，一次一个。
- 每项工作是一个 issue，归到所属 milestone；没有归属的 issue 是待定的想法，不排期。
- 新想法先开 issue，不插进正在做的 milestone。
- 服务端和对应的 HTTP API 先于界面。
- milestone 里的 issue 全部关闭，这个主题就算完成，维护者合并发布 PR 发版（[release.md](release.md)）。

## 已完成

0.1.0 之前的 milestone 按主题编号，和发版版本号无关；从 0.5 起两者一致。

- 0.1.0：仓库基线和架构（独立应用，同一个程序既是服务端又是客户端，见 [ADR 0002](adr/0002-standalone-client-and-server.md)）；SQLite 存储、带鉴权的 HTTP API、抓取、正文抽取和 SSRF 防护；订阅、时间线、设置、OPML 和 AI 摘要翻译的接口与界面；图标、签名、公证、自动更新和发布流程。
- 0.2.0：形态列表、按范围全部标为已读并可撤销、快捷键表、下一条未读、站点图标。
- 0.3.0：纸墨配色和行底色渐变，隐藏标题栏并用 Liquid Glass 绘制窗口外框。
- 0.4.0：外观设置（主题、界面语言、字体、阅读字号）；墨色侧栏、石墨深色和无衬线正文的视觉改版。
- 0.5.0：状态提示、右键菜单里的批量标为已读、键盘可达的侧栏、宽窗口设置页、术语表、遵守 Retry-After、预计阅读时间。
- 0.6.0：按形态选默认视图、网格浏览、阅读字体与中西文排版、选中文字翻译解释、命令面板、阅读位置与上次看到哪里、讨论链接、看图层、导出 Markdown、列表密度和排序、侧栏过滤与收起、追踪图片过滤、条目语言。0.5 剩下的修复和小改动并入 0.7。

## 进行中和之后

- [0.7 订阅管理](https://github.com/yangflow/oriel/milestone/10)
- [0.8 数据安全](https://github.com/yangflow/oriel/milestone/11)
- [0.9 优先级与自动化](https://github.com/yangflow/oriel/milestone/12)
