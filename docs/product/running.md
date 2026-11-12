# 运行和部署

桌面和无窗口两种运行方式、部署、登录、日志、诊断和更新。索引见 [docs/product.md](../product.md)。

## 运行模式

| | 桌面模式 | 无窗口模式 |
|---|---|---|
| 启动 | 打开应用，或不带参数运行 `oriel` | `oriel serve [-data 目录] [-port 端口] [-trusted-proxy 地址段]` |
| 数据目录 | 系统的用户数据目录：macOS `~/Library/Application Support/Oriel`，Windows `%AppData%\Oriel`，Linux `~/.config/Oriel` | 同左，`-data` 可改 |
| 监听 | `127.0.0.1` 的随机空闲端口，不要口令 | 默认端口 7470。没设口令只听 `127.0.0.1`；设了口令听所有网卡 |
| 口令 | 不用 | 只从环境变量 `ORIEL_PASSWORD` 读，不接受命令行参数，免得出现在进程列表里 |
| 退出 | 关最后一个窗口或退出菜单；服务端随之关闭，最多 2 秒 | SIGINT 或 SIGTERM，最多等 10 秒让请求完成 |

### 桌面集成

登录时打开、菜单栏或托盘图标、Dock 未读角标，三者都是桌面模式的本机偏好（`openAtLogin`、`trayIcon`、`dockBadge`，默认全关，见[「设置界面」](screens.md#设置界面)的「桌面」）。不用 cgo：登录项、托盘和角标通过 MyGo，macOS 的角标权限通过已有的 purego 调用系统接口。

可行性，按系统：

| | macOS | Windows | Linux |
|---|---|---|---|
| 登录时打开 | 可以，要从 `.app` 包里运行 | 可以（当前用户的启动项） | 可以（`~/.config/autostart`） |
| 菜单栏或托盘图标 | 可以，菜单栏图标，模板图 | 可以，通知区域图标 | 可以，要 libayatana-appindicator3；没有点击事件，只有菜单 |
| 未读角标 | 可以，Dock 角标 | 不做：MyGo 在这个系统上不画角标 | 不做：只对部分桌面环境的启动器有效，不可靠 |

- 未读数：就是 `GET /api/sources` 的 `unreadTotal`（同一篇文章只算一次，屏蔽的不算，暂停的订阅也不算），和侧栏「全部」的数字一致，不另算。数字为 0 时角标清除；超过 999 显示 `999+`（托盘标题同）。
- macOS 角标权限：首次需要显示非零角标时，只申请应用图标标记权限，不申请横幅和声音，也不发送通知。拒绝后不显示角标，开关仍保留用户的偏好；需要在系统设置的通知中允许 Oriel 的应用图标标记。后续更新会重新检查系统授权。等待授权期间关闭开关或清空未读，授权完成后也不会恢复旧数字；未读数变化时使用最新值。原生权限接口只在 `.app` 中调用。
- 更新频率：时间线本来每 60 秒轮询一次，已读、未读变化时它也会即时更新，角标和托盘数字跟着它走。窗口隐藏时时间线不重画，所以由外壳自己每 60 秒调一次 `GET /api/sources` 取这个数；窗口显示时不重复请求。读取失败时保留上一个数字，不清零。
- 托盘菜单：未读数（不可点）、打开 Oriel（显示并聚焦窗口）、全部更新（和菜单栏里的一样，`POST /api/sources/refresh`）、退出。macOS 上图标旁显示未读数（标题），没有未读时只有图标；Windows 和 Linux 只用悬停提示写明未读数。macOS 点图标就是打开菜单；Windows 单击图标打开窗口，右键打开菜单。
- 关窗口：开着托盘或菜单栏图标时，关窗口只是隐藏窗口，应用继续在后台，服务端和抓取照常；要完全退出用托盘菜单的退出或应用菜单的退出（macOS 的 Dock 图标点一下也会重新显示窗口）。没开图标时，关最后一个窗口就退出。只开了 Dock 角标不改变关窗口的行为。
- 登录时打开：以登录项启动时（`WasOpenedAtLogin`）如果托盘图标开着，窗口不显示，只留图标；图标没开就正常显示窗口，免得应用起来了却找不到。已经在运行时再次启动，仍是打开并聚焦原来的窗口。
- 开关生效：切开关立即生效，不用重启。托盘图标开不起来（Linux 缺依赖）时，开关退回关并提示，不影响其他两项。
- 无窗口模式：`oriel serve` 不启动事件循环，没有窗口、托盘、角标和登录项，这三项偏好对它不起作用，也不写任何系统登录项。
- 托盘图标的图片：macOS 用单色模板图（`resources/trayTemplate@2x.png`，18 点高，黑色加透明），Windows 和 Linux 用应用图标的小图（`resources/trayIcon.png`，64 像素）。两张都由 `design/export-icon.sh` 生成：模板图来自 `design/tray.svg`，小图是应用图标缩小。MyGo 只收一张 PNG 并按 18 点高缩放，所以应用内嵌 `@2x`。

- 退出时整个关闭流程只有一个总期限：桌面模式 2 秒、无窗口模式 10 秒，到时放弃还没结束的工作、关掉数据库，进程随之结束。顺序是：先取消所有后台工作（抓取、AI、自动备份、星标图片副本）和图片下载（含图片代理正在进行的下载），等图片的请求因此立刻返回；再等请求完成。到时还没完成的请求（比如一次不回话的 AI 连接测试）被强行中断，请求的上下文随之取消，再最多等 0.5 秒让它们的处理函数返回（没返回会记一条警告）；然后等后台工作停下，到期还没停的记一条警告并放弃。关库前的 `PRAGMA optimize` 和 WAL 截断（见[「数据库维护」](data.md#数据库维护)）用总期限里剩下的时间，最多 3 秒，没时间就跳过。
- 桌面模式只开一个实例。再次打开时，已有的实例把窗口带到前面。
- 同一个数据目录同时只能有一个进程（见[「存储」](data.md#存储)）。桌面模式打不开数据时，用系统语言弹出原生错误框，然后退出。
- 无窗口模式不启动界面事件循环，不加载 GTK 等界面库，可以跑在没有图形环境的服务器上。
- 版本号只写在 `mygo.json`，打包和未打包的程序都从这里读，`/health` 返回它。
- 无窗口的备份和恢复命令 `oriel backup`、`oriel restore` 见[「备份与恢复」](import-export.md#备份与恢复)的「命令行」。它们不加载界面库，口令不参与，因为直接读写本机的数据目录。
- 健康检查命令 `oriel healthcheck` 见[「部署」](#部署)。

### 连接远程服务端

桌面应用默认读写本机的服务端。应用菜单的「连接服务端…」打开连接界面，填地址和口令，改连另一台电脑上的 `oriel serve`。

- 地址没写协议时补 `http://`；没写协议也没写端口时补端口 7470。
- 先请求 `GET /health`：连不上、不是 Oriel、数据库不可写各有一句提示。不要口令的服务端直接连上；要口令的再 `POST /auth/login`，口令为空或不对时提示在口令框下。
- 连上后，服务端地址和会话 token 写进数据目录的 `remote.json`，只有当前用户能读写（0600）。口令只在登录时发送，不写进任何文件。
- 再次打开应用时，用保存的 token 请求 `GET /api/settings` 检查会话。401 时打开连接界面，写明哪个服务端的登录失效了，地址已填好，焦点在口令框；连不上时也打开连接界面，显示连不上。
- 连接界面可以改回本机服务端，这时删掉 `remote.json`。
- 网络和磁盘操作都在后台做，界面不卡；连接进行中不会重复发请求。

## 部署

`oriel serve` 在家用服务器、NAS 或 VPS 上长期运行，有两种现成的做法。示例文件在 `deploy/`，下面的文字和它们逐字相同（测试会核对）。示例里没有口令：口令写在只有管理员能读的文件里，文件不进仓库（`deploy/.env` 在 `.gitignore` 里）。

- **口令是必须的。** 没设 `ORIEL_PASSWORD` 时服务端只听 `127.0.0.1`（见[「鉴权」](#鉴权)）。在容器里这是容器自己的回环地址，宿主机和其他设备都连不上，所以容器和系统服务都要设口令，口令只从环境变量读。
- **数据目录。** 一律用 `-data` 指到专门的目录：容器里是卷 `/data`，系统服务是 `/var/lib/oriel`。数据库、会话、API key、日志和图片缓存都在里面，只有运行服务的用户能读写。
- **端口。** 默认 7470。容器里固定监听 7470，用端口映射改对外的端口（示例只发布在 `127.0.0.1` 上）；系统服务改 `ExecStart` 里的 `-port`。
- **反向代理和 HTTPS。** 服务端自己只说 HTTP，口令和会话 token 在明文网络上会被看到，要在公网或不可信的网络上用就放在反向代理后面，由它终结 HTTPS。示例 compose 已经只把端口发布在宿主机的回环地址 `127.0.0.1` 上，给同机的代理用；要让局域网里的其他设备直接连，把映射改成 `"7470:7470"`（compose 里的注释写了），但口令会在网络上明文传输。代理在场时加 `-trusted-proxy`（见[「鉴权」](#鉴权)）。
- **健康检查。** `oriel healthcheck [-port 端口] [-timeout 时长]` 请求本机 `127.0.0.1` 上服务端的 `GET /health`：回答 200 且是 Oriel（`app` 为 `oriel`，数据库可写）就在标准输出写 `ok`、退出码 0；连不上、超时、状态码不是 200、回答的不是 Oriel 时在标准错误写原因、退出码 1；参数不对是 2。有没有口令都一样，因为 `/health` 不要会话，回环地址也总是听的。不加载界面库，镜像里不需要 curl。
- **备份和升级。** 备份用 `oriel backup`（见[「备份与恢复」](import-export.md#备份与恢复)）：容器里 `docker compose -f deploy/compose.yaml exec oriel /oriel backup -data /data -o /data/oriel.oriel-backup`，再用 `docker compose cp` 取出；系统服务以 root 运行 `oriel backup -data /var/lib/private/oriel -o 文件`（`DynamicUser` 的数据真正放在这里，`/var/lib/oriel` 是指向它的链接）。也可以在应用里导出，或开自动备份。自动备份写到的文件夹要放在卷里，或另外挂载进容器、加进 `ReadWritePaths`，因为容器的根文件系统是只读的，系统服务除 `StateDirectory` 外整个文件系统也是只读的。升级前先备份；之后换上新版本重启：容器 `git pull` 后 `docker compose -f deploy/compose.yaml up -d --build`，系统服务替换 `/usr/local/bin/oriel` 再 `systemctl restart oriel`。迁移前的副本和退回旧版本的办法见[「存储」](data.md#存储)。恢复要先停服务端（`docker compose stop`，或 `systemctl stop oriel`）。

### 容器

`Dockerfile` 做出同时支持 `linux/amd64` 和 `linux/arm64` 的镜像：在构建机上为目标平台交叉编译，不用模拟器。分两个阶段：Go 官方镜像（版本与 `.go-version` 相同）编译，`gcr.io/distroless/base-debian12:nonroot` 运行。程序需要 glibc（见[「技术基线」](../product.md#技术基线)），所以不用 Alpine 等 musl 镜像；这个基底带 glibc 和 HTTPS 用的 CA 证书，不带 shell，也没有 GTK，因为无窗口模式不加载界面库。

- 以用户 65532（`nonroot`）运行，入口是 `/oriel`，默认参数 `serve -data /data -port 7470`；`/data` 是卷，第一次挂载命名卷时属于这个用户。
- `HEALTHCHECK` 是 `/oriel healthcheck -port 7470`，间隔 30 秒、超时 5 秒、启动宽限 20 秒、连续 3 次失败算不健康。
- 示例用 compose 构建本地镜像：`docker compose -f deploy/compose.yaml up -d --build`，根文件系统只读，去掉所有 capability，禁止提权。口令来自 `deploy/.env` 里的 `ORIEL_PASSWORD`，没设时 compose 直接报错，不会悄悄起一个连不上的服务。

`deploy/compose.yaml`：

```yaml
# Oriel server. From the repository root:
#   echo 'ORIEL_PASSWORD=<your password>' > deploy/.env && chmod 600 deploy/.env
#   docker compose -f deploy/compose.yaml up -d --build
services:
  oriel:
    build:
      context: ..
    image: oriel:local
    restart: unless-stopped
    ports:
      # Reachable from this host only, for a reverse proxy on it. To reach it from other devices without a proxy,
      # publish on every interface, "7470:7470", and know that the password then crosses the network as plain text.
      - "127.0.0.1:7470:7470"
    environment:
      # Without a password the server listens on 127.0.0.1 inside the container and cannot be reached.
      ORIEL_PASSWORD: ${ORIEL_PASSWORD:?set ORIEL_PASSWORD in deploy/.env}
    volumes:
      - oriel-data:/data
    read_only: true
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true

volumes:
  oriel-data:
```

### 系统服务

`deploy/oriel.service` 放在 `/etc/systemd/system/`，程序放在 `/usr/local/bin/oriel`，口令在 `/etc/oriel/oriel.env`（权限 `0600`，内容 `ORIEL_PASSWORD=<口令>`）。单元用 `DynamicUser` 和 `StateDirectory`，不用另建用户；systemd 以 root 读 `EnvironmentFile`，服务用户读不到。失败时 5 秒后重启；停止时服务端最多等 10 秒（见[「运行模式」](#运行模式)），所以 `TimeoutStopSec` 是 20 秒。

```ini
# /etc/systemd/system/oriel.service
#   install -m 0755 oriel /usr/local/bin/oriel
#   install -d -m 0700 /etc/oriel
#   echo 'ORIEL_PASSWORD=<your password>' > /etc/oriel/oriel.env && chmod 600 /etc/oriel/oriel.env
#   systemctl enable --now oriel
[Unit]
Description=Oriel server
After=network-online.target
Wants=network-online.target

[Service]
# A user that exists only while the service runs; the data is in /var/lib/oriel.
DynamicUser=yes
StateDirectory=oriel
# systemd reads this file as root; the service user never needs to read it.
EnvironmentFile=/etc/oriel/oriel.env
ExecStart=/usr/local/bin/oriel serve -data %S/oriel -port 7470
Restart=on-failure
RestartSec=5
# The server waits up to 10 seconds for running requests when it is stopped.
TimeoutStopSec=20
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
StateDirectoryMode=0700
UMask=0077
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
CapabilityBoundingSet=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
```

### 发布镜像

镜像只在 CI 里构建和跑一遍健康检查，不推送到任何注册表：推送是向外部服务发东西，要维护者确认（见 [release.md](../release.md)）。

## 鉴权

- 没设口令时，服务端只听 `127.0.0.1`，不校验。桌面模式默认这样。
- 设了口令时听所有网卡，除 `GET /health`、`POST /auth/login` 和 WebSub 回调 `/websub/{key}` 外都要会话。回调只在设了 `websub_base_url` 时才有效，靠不可猜的 `key` 和每个源自己的签名密钥保护，见[「WebSub 推送」](fetching.md#websub-推送)。
- 口令不写进日志和数据库。启动时用 PBKDF2-SHA256（60 万轮、随机盐）派生一次，登录时同样派生后用常数时间比较。
- 登录请求的坏 JSON、或没声明 `Content-Type: application/json`，返回 401，不是字段校验错误。
- 服务端拒绝来自其他来源的浏览器请求：`POST`、`PUT`、`PATCH`、`DELETE` 带了 `Origin` 而它的主机和端口与请求的 `Host` 不同，或带 `Sec-Fetch-Site: cross-site`，返回 403、`error` 为 `forbidden_origin`。不带 `Origin` 的请求（原生客户端、命令行）不受影响。没设口令时，`Host` 不是本机的（`localhost`、`127.0.0.0/8`、`::1`，端口不限，主机名末尾的点忽略）也返回 403、`forbidden_host`；设了口令时不限 `Host`。WebSub 回调 `/websub/{key}` 不查 `Host`，因为 hub 经公开地址访问它。
- 读 JSON 请求体的接口要求 `Content-Type: application/json`（可带 `charset` 等参数），否则返回 415、`error` 为 `unsupported_media_type`。
- 登录限速，防止猜口令和拖垮服务：同一个客户端地址（默认是 `RemoteAddr`，不看 `X-Forwarded-For`）连续输错 5 次不受限，第 6 次错起锁 1 秒，之后每多错一次翻倍，最长 15 分钟；锁定期间的登录不再派生密钥，直接返回 429、`error` 为 `too_many_attempts`，`Retry-After` 给出剩余秒数。登录成功清掉该地址的记录，安静满 1 小时的记录也会清掉（不依赖有人输错，登录请求到来时至少每分钟清一次），内存里最多留 1 万条；满了再来新地址时，一次挤掉最久没动静的 100 条记录（优先挤没被锁的），新地址照样限速。
- 放在反向代理后面时，所有客户端的 `RemoteAddr` 都是代理的地址，几次输错就会把本人锁在外面。可选的 `-trusted-proxy`（逗号分隔的 IP 或 CIDR，例如 `127.0.0.1,10.0.0.0/8`）默认关闭；只有 `RemoteAddr` 在这些地址段里时，才读 `X-Forwarded-For`，从右往左跳过可信代理，取第一个不可信的地址作为客户端地址；头里的地址可以带端口或方括号，IPv6 的区域标识会去掉；头缺失、解析不了，或整条链都是可信代理时，仍用 `RemoteAddr`。不要把客户端所在的网段或 `0.0.0.0/0` 写进 `-trusted-proxy`，`/0` 会被拒绝。`RemoteAddr` 不在地址段里的请求，头一律忽略，防止直连的人伪造。
- 同时派生密钥的登录最多 2 个，其余不排队，直接返回 429 和 `Retry-After: 1`。连接界面把 429 说成尝试次数太多、请稍后再试，焦点在口令框。
- HTTP 服务设读头 10 秒、整个请求 1 分钟、写 150 秒（比最慢的 AI 测试调用 2 分钟略长）、空闲 2 分钟的超时。等 AI 接口回答的路由（`POST /api/ai/test`、`POST /api/ai/selection`）自己把读写期限放宽到 150 秒，不受整个请求 1 分钟的限制；备份和恢复放宽到 30 分钟。
- 会话放在 cookie `oriel_session`（HttpOnly，SameSite=Lax，30 天；HTTPS 或带 `X-Forwarded-Proto: https` 时加 Secure），或 `Authorization: Bearer <token>`。空闲满 30 天失效。最近使用时间最多一分钟写一次。
- 每次登录是一条设备会话，可以单独退出登录，也可以登出当前会话。

## 日志文件

Oriel 在数据目录里写一份滚动的日志文件，桌面应用和 `oriel serve` 都写。打包后的桌面应用没有终端，stderr 无处可看，进程崩了内存里的最近错误也随之丢失，「它自己退出了」无从查起。

- 位置：数据目录下的 `logs/oriel.log`（数据目录：macOS 是 `~/Library/Application Support/Oriel`，Windows 是 `%AppData%\Oriel`，Linux 是 `~/.config/Oriel`；`oriel serve -data dir` 时是 `dir`）。目录权限 `0700`，文件权限 `0600`。连着远程服务端时，日志在服务端那台电脑上。
- 格式：与 stderr 相同的 JSON 日志，一行一条记录，时间为 UTC；stderr 照常也写。Info 及以上的记录都写，Debug 不写。
- 大小：单个文件最多 5 MB，写满前滚动：`oriel.log` 改名为 `oriel.log.1`，原来的 `.1` 改名为 `.2`，原来的 `.2` 删除，再新建 `oriel.log`。一共留 3 个文件，磁盘上日志总量不超过 15 MB。启动时接着最新的文件往后写，不清空。滚动或写入出错不影响程序运行，日志只是少写；打不开日志文件时在 stderr 里警告一次。
- 脱敏：写入时就做，文件里没有未脱敏的版本。规则同「诊断文件」的脱敏规则（网址只留 `scheme://host`，主目录写成 `~`，名字像密钥的属性不写，`token=…`、`Bearer …` 的值抹掉），区别是单个值最多留 1000 个字符，堆栈最多 16 KB，而不是 300 个字符。口令、密钥和文章内容从不进日志。
- 崩溃：Oriel 自己启动的每个后台 goroutine（抓取、全文提取、AI 队列、图片下载、图片缓存淘汰、自动备份、阅读时间回填、压缩、导入）和主 goroutine 的最上层都捕获 panic，以 Error 级别写一条 `Panic` 记录：`where`（哪一项工作）、`panic`（panic 的值）、`stack`（调用栈，主目录写成 `~`，Windows 上的反斜杠和斜杠写法都算）。之后：
  - 一次抓取、提取、AI 任务或图片下载的 panic 只丢掉这一项，别的源和任务照常。抓取的 panic 记为一次失败（`last_error` 写「internal error while fetching」），按失败的重试时间再来，不会马上又被取到；AI 任务的 panic 记为一次失败的尝试（`ai_upstream`），受自动任务三次的限制；提取和图片下载没有记成成功，按原来的时间再来，等图片的请求得到的是失败。抓取线程池里的一个工作线程自己 panic 时（不在某次抓取里），等一分钟再重新启动。
  - 抓取、AI、副本、自动备份这些长期循环自己 panic 时，等一分钟再重新启动，避免坏数据让它空转。
  - 一个 HTTP 请求的处理 panic 时，记日志并像 `net/http` 一样断开这条连接，服务端继续。
  - 压缩和导入这类一次性的后台任务 panic 时，状态记为失败，界面照常显示失败。
  - 主 goroutine（桌面应用的界面线程、`oriel serve` 的主流程）panic 时，写完日志以退出码 2 退出。
  日志是同步写的，进程随后退出，记录不会丢。

## 诊断文件

用户报问题时，一个不含密钥的文本文件，由用户自己存下来、自己附到 issue 上。文件只写在本机，Oriel 不上传，也不自己发给任何人。

取得文件有三条路，内容相同：

- 设置的「活动与存储」页里导出：先弹出系统的存储对话框（默认文件名 `Oriel-diagnostics.txt`，类型是文本文件），再向服务端请求 `GET /api/diagnostics`，把响应写进选定的文件；之后写明结果：成功、取不到或写不进文件。取消对话框什么都不显示。连着远程服务端时导出的是那台服务端的诊断。
- 桌面应用的「帮助」菜单里的「导出诊断文件…」：打开设置的「活动与存储」页并开始同一个导出。
- 无界面的命令行：`oriel diagnostics [-data dir] -o file`，直接从数据目录读数据库写出同一个文件（`-o` 必填，`-` 写到标准输出；`-data` 默认同 `oriel serve`）。它没有运行中的服务端，所以「最近的错误」一节是空的，文件里写明这一点。数据库正被另一个 Oriel 进程使用时（桌面应用开着）打不开，提示先退出那个进程，或改用应用里的导出。

`GET /api/diagnostics` 要会话（设了口令时），返回 `text/plain; charset=utf-8`，不缓存。文件是 UTF-8 文本，用 `\n` 分行，分五块：

1. 头部：`Oriel diagnostics`、生成时间（UTC，RFC 3339）、版本、操作系统和架构（`darwin/arm64`）、模式（`desktop`、`headless`；从数据目录写出时是 `data directory`）。
2. 数据：数据库大小（字节）、订阅数、条目数。只有数字。
3. 最近的错误：服务端进程在内存里留着最近 200 条 Warn 和 Error 级别的日志，每条一行：时间、级别、消息、属性。Info 和 Debug 不留，因为 Info 里有地址和数据目录。留的是写入时已经脱敏的文字，原始记录不在内存里。服务端启动以来没有过错误时写「none」；从数据目录写出时写「not available: this file was written from a data directory, not from a running server」。
4. 订阅：每个订阅一行，`id`、`host`、`paused`、`consecutive_failures`、`last_success_at`（UTC，从未成功写 `never`）、`last_error`（脱敏后的文字，没有时省略）。`host` 只写主机名；RSSHub 路由的订阅写 `rsshub` 加路由的第一段（`rsshub:/github`），不写后面的参数。按 `id` 排序。
5. 日志文件：数据目录里最新那个日志文件（`logs/oriel.log`，见[「日志文件」](#日志文件)）的最后一段，最多 300 行、128 KB，每行是日志文件里的一行 JSON。读出来时再脱敏一遍，所以就算文件被人改过，写进诊断文件的也是脱敏后的文字。数据目录里没有日志文件（比如从没用桌面应用或 `oriel serve` 跑过）时写「none」。从数据目录写出的诊断文件也有这一块，因为日志在磁盘上，不依赖运行中的服务端。

脱敏规则（日志和 `last_error` 一视同仁，写入时就做，文件里没有未脱敏的版本）：

- 文字里出现的网址（`scheme://…`）只留 `scheme://host`，路径、查询串、片段、用户名和密码都去掉。主机名之后的端口保留。
- 用户主目录（里面有本机用户名）写成 `~`，比如文件错误里的 `/Users/<名字>/Library/…` 写成 `~/Library/…`。
- 日志属性的名字（不分大小写）含 `key`、`token`、`password`、`passwd`、`secret`、`credential`、`authorization`、`cookie`、`signature` 之一的，整个属性不写；属性组里按带点的完整名字判断。
- 单条属性值、消息和 `last_error` 各最多保留 300 个字符，超出的以 `…` 结尾。
- 任何情况下都不写入：口令、会话令牌、AI 的 key、提示词、条目的标题和正文、订阅的完整地址、标题、站点地址和 RSSHub 实例地址、代理的地址、用户名和密码、WebSub 的回调 key 和签名密钥。这些数据根本不进入文件的组装代码；脱敏只是第二道防线。
- 日志本来就不记它们（见[「AI 摘要和翻译」](ai.md#ai-摘要和翻译)和「鉴权」），脱敏防的是错误文字里夹带的网址，比如 `Get "https://host/feed?token=…": EOF`。
- 测试里有一条检查：口令、会话令牌、AI key、带 `?token=` 的订阅地址、带令牌网址的日志和名为 `authorization` 的属性都不出现在文件里。

## 更新

用 MyGo 的原生更新插件（`native.Plugin`）。更新源只有 `yangflow/oriel` 公开的 GitHub Releases，走 HTTPS；每个平台一份更新清单，安装包和增量包都用 Ed25519 签名，公钥写在 `mygo.json` 的 `updates.publicKey`，签名不对、换了密钥或不是 HTTPS 的更新一律不装，应用保持原样。

- 每天在后台检查一次，应用菜单「检查更新…」随时检查。
- 有新版本时弹原生更新窗口，显示这一版在 `CHANGELOG.md` 里的说明，用户选安装、跳过这一版或以后提醒；不确认就不下载、不安装。
- 窗口的语言跟随系统，不受「外观」里界面语言的影响：中文系统（含繁体）显示简体中文，其他系统显示英文。
- 开发构建（`go tool mygo dev`）和没有用发布密钥签过的构建不检查更新。`oriel serve` 不更新自己。

发布流程、密钥和每次发布的步骤见 [docs/release.md](../release.md)。
