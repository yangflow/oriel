# 0003 用纯 Go 的 SQLite

- 状态：已采纳
- 日期：2026-10-12
- 相关：[docs/product.md](../product.md)「存储」

## 背景

服务端要一个单文件、带全文检索的嵌入式数据库，单用户用 SQLite 足够。技术基线不允许生产代码用 cgo，否则交叉编译六个目标就要六套 C 工具链。

## 决定

用 `modernc.org/sqlite`。它是 SQLite 的 C 源码机器翻译成的 Go，带 FTS5 和 trigram 分词，六个目标都能用 `CGO_ENABLED=0` 编译。通过 `database/sql` 使用，驱动名 `sqlite`。

## 代价

比 cgo 版慢，程序也更大。多了一串间接依赖（`modernc.org/libc` 等），每次升级都要同步 `THIRD_PARTY.md` 和许可证文件。SQLite 的版本跟着驱动走，不能单独升。

## 什么时候重新考虑

真机上测出查询或写入速度影响交互，或者驱动停止维护。
