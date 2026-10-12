# 0001 纯 Go 原生 UI

- 状态：已采纳
- 日期：2026-10-12
- 相关：[docs/product.md](../product.md)「技术基线」

## 背景

用 Electron 套网页做桌面应用，体积和启动都重。Oriel 要一种语言、可复现的构建、真正的原生窗口。参考 [MyGo 原生 UI 文档](https://mygo.egoist.dev/docs/ui) 和[固定版本的源码](https://github.com/egoist/mygo/tree/v0.3.6)。

## 决定

用 MyGo 原生 UI，一个 Go 模块，命令行工具在 `go.mod` 里用 `tool` 固定版本。更新用原生更新插件。

## 代价

控件是 GPU 绘制的，无障碍和各平台表现要在真机上测。没有 WebView，视频和社交内容不能内嵌官方播放器。MyGo 版本还早，每次升级要看 API 和打包的变化。

## 什么时候重新考虑

某个必须的平台能力或无障碍要求，原生 UI 做不到。
