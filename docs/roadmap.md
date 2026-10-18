# 路线图

规划和进度都在 GitHub：

- 一个 [milestone](https://github.com/yangflow/oriel/milestones) 是一个主题，按版本号顺序做，一次一个。
- 每项工作是一个 issue，归到所属 milestone。
- 新想法先开 issue，不插进正在做的 milestone。
- 服务端和对应的 HTTP API 先于界面。
- milestone 里的 issue 全部关闭，这个主题就算完成。

## 已完成

- 0.1 仓库基线：MyGo 项目初始化、MIT 许可、开源社区文件、CI。
- 0.2 定架构：独立应用，同一个程序既是服务端又是客户端，见 [ADR 0002](adr/0002-standalone-client-and-server.md)。
- 0.3 服务端基线：SQLite 存储和数据模型、带鉴权的 HTTP API 骨架；桌面模式和无窗口模式的启动。
- 0.4 抓取：订阅抓取、正文抽取和 SSRF 防护、按保留期限清理。
- 0.5 订阅接口：添加、列出、修改、删除订阅和「更新」的 HTTP API，见 [product.md](product.md)「订阅接口」。
- 0.6 时间线接口：按游标分页的时间线、按源 / 形态 / 未读 / 收藏筛选、全文检索、标记已读和收藏的 HTTP API，见 [product.md](product.md)「时间线接口」。

## 进行中和之后

- [0.7 服务端补全](https://github.com/yangflow/oriel/milestone/3)
- [0.8 第一批界面](https://github.com/yangflow/oriel/milestone/4)
- [0.9 设置与导入导出界面](https://github.com/yangflow/oriel/milestone/5)
- [0.10 AI 摘要和翻译](https://github.com/yangflow/oriel/milestone/6)
- [1.0 发布](https://github.com/yangflow/oriel/milestone/7)
