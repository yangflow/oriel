# 0004 A WebView only for the official video players

- Status: accepted
- Date: 2026-10-23
- Related: [0001](0001-pure-go-native-ui.md), [docs/product.md](../product.md), [docs/design.md](../design.md),
  issue #392

## Context

[0001](0001-pure-go-native-ui.md) chose native UI and no WebView, at the cost that video entries cannot embed the
official players: opening one hands it to the system browser, and watching a video means leaving Oriel. Native UI
cannot play YouTube or bilibili itself, because the streams are only reachable through the platforms' private
interfaces, which docs/product.md rules out.

MyGo can place a system web view inside a window of native UI (`Window.NewWebView` and `ui.WebView`), still without
cgo: WKWebView on macOS, WebView2 on Windows, WebKitGTK loaded with dlopen on Linux. A trial on macOS (issue #392)
showed that the bilibili player plays when loaded directly, and that the YouTube player fails with error 153 unless
it sits in an iframe of a page with an https origin (`Page.LoadHTML` with an https base URL).

## Decision

The interface stays native. The one exception is a web view that shows the official embedded player of a video
entry, and nothing else:

- It loads only a page Oriel writes itself, an iframe of an allowed player address: YouTube through
  `www.youtube-nocookie.com/embed/<id>` and bilibili through `player.bilibili.com/player.html`. It never loads an
  entry's link or any other page.
- Any other navigation, and every new window the page asks for, goes to the system browser instead.
- The page cannot call Go. No sign-in, and nothing of it is kept between runs.
- When the entry has no allowed player, when no web view is available (for example Linux without WebKitGTK), or when
  the player cannot load, the entry opens in the system browser as before.

Everything else stays as 0001 decided: no HTML frontend, no frontend toolchain, no cgo.

## Cost

A web engine process runs while a video plays, with its memory. On Linux, playing in Oriel needs WebKitGTK, which
not every desktop has installed. Each platform's web view has to be tried on a real device, and the platforms can
change what their embeds require, as YouTube did with error 153.

## When to revisit

A platform stops allowing its player to be embedded this way, or the web view needs to show anything other than an
official player.
