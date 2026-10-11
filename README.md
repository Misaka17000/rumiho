# Rumiho

一个人的 Docker mihomo 代理：登录、订阅、国内分流、节点选择、实时连接和带认证的接入链接。网页与 HTTP 代理共用一个对外端口，每个容器保存独立配置。

## 启动

直接使用公开镜像部署：[`misakaxie/rumiho:0.1.3`](https://hub.docker.com/r/misakaxie/rumiho)（Linux amd64）。使用 [compose.hub.yaml](compose.hub.yaml) 作为服务器的 `compose.yaml`，无需下载源码或在服务器构建，配置步骤见 [部署说明](docs/DEPLOY.md)。

从源码构建：

```bash
cp -n config.example.yaml config.yaml
# 编辑 config.yaml，填写用户名和至少 8 个字符的密码
docker compose up -d --build
```

打开 `http://服务器IP:18080`，使用 `config.yaml` 中的账号密码登录。当前部署目录可写挂载到容器 `/config`，页面保存直接写回该文件。不需要配置 UID/GID，容器启动时自动处理目录权限，再以普通用户运行应用。`.env` 是可选项，仅用于修改端口或绑定地址；应用不需要数据库。

登录后直接进入定制 zashboard。在「订阅与接入」填写上游 Clash/mihomo YAML 订阅；「HTTP 代理」中填写局域网可访问的接入地址，例如 `http://192.168.1.10:18080`，保存并复制带认证的代理链接。点击「重置链接」可更换代理密码。此页只管理订阅和 HTTP 代理接入，不提供客户端订阅下载或手动配置面板。规则/全局/直连模式在「代理」页设置。

支持含 `proxies` 的完整 YAML 或直接的节点数组。完整订阅会保留 `proxy-groups`、`rules`、HTTP/inline 规则集以及 Geo 数据设置，代理页可以分别选择每个分组的节点。没有分组时生成 `PROXY` / `AUTO`，没有规则时使用内置国内与局域网直连规则；没有节点时直连。全局模式使用 `GLOBAL` 组所选出口。

订阅不能覆盖实例的监听端口、管理接口、认证、DNS 或 TUN。暂不支持 Base64 节点链接、`proxy-providers`、本地文件规则集和订阅转换。含 GEOSITE/GEOIP 的订阅首次导入需要下载对应数据，下载失败或配置无效时保留之前的可用配置。旧版本升级后点击「更新订阅」，即可恢复原来被忽略的分组和规则。

订阅流程参考 Clash Verge Rev 的完整配置导入与网络回退，以及 mihomo 的校验和缓存处理。具体源码版本、采用的行为及兼容边界见 [订阅实现说明](docs/SUBSCRIPTIONS.md)。

## 实例与数据

只有一个管理员。HTTP 代理用户名使用 `admin.username`，与登录用户名一致；登录密码和代理密码独立，均按明文保存在同一份 YAML。页面「修改登录密码」会写回文件、立即生效并退出所有登录会话；「重置链接」只更换代理密码并断开现有代理连接。修改用户名后，设备需要重新复制代理链接。

- `config.yaml`：唯一应用配置，保存账号密码、代理密码、订阅地址、接入地址、模式、刷新周期、节点、分组和规则。
- `cache/`：mihomo 自动生成的 `mihomo.yaml`、节点选择缓存、Geo 数据与下载规则集，不是另一份应用配置。

挂载目录而非单个文件，是为了能先写临时文件、再原子替换 `config.yaml`，避免保存中断留下半份配置。手动编辑时先停止容器，修改后再启动；平时直接在页面保存。

旧版本升级时保留原有配置内容，将文件放到与 Compose 同级的 `config.yaml`；旧 `config/cache/` 可以复制为同级 `cache/`。具体步骤见 [部署说明](docs/DEPLOY.md)。

`docker compose down` 保留宿主机配置目录；更新镜像后运行 `docker compose up -d --build`。备份 `config.yaml` 与 `cache/` 即可。内置规则随镜像更新，订阅规则随订阅更新；远程规则集与 Geo 数据遵循订阅的更新设置。

修改对外地址、更新间隔，或刷新后节点与路由配置没有变化时，只保存设置，不重新校验、加载内核配置。修改节点、分组、规则、模式或代理凭据时仍先校验，失败则恢复原配置。

多人分别部署时使用不同部署目录、项目名和宿主机端口：

```bash
# 每个部署目录各有一份 compose.yaml 和 config.yaml
docker compose -f alice/compose.yaml -p alice up -d
PORT=18081 docker compose -f bob/compose.yaml -p bob up -d
```

一个实例可以给自己的多台设备共用，所有连接应用同一套服务器规则。

## 端口和协议

默认对外发布 `18080`，映射到容器入口 `8080`。mihomo 的 HTTP 代理 `7890`、控制接口 `9090` 只监听容器内回环地址。入口识别普通网页请求、HTTP 正向代理及 CONNECT，网页会话不会作为代理认证凭据。

支持 HTTP 代理和通过 CONNECT 访问 HTTPS 站点，不支持 SOCKS5、UDP、TUN 或透明代理。HTTP 入口适用于可信局域网，网页登录和代理认证到服务器这一段不加密。需要加密时，入口服务原生支持 `TLS_CERT` / `TLS_KEY`，将证书只读挂载进容器并设置这两个环境变量，仍使用同一端口；「对外接入地址」改成 `https://主机:端口`，客户端需支持 HTTPS 代理且信任证书。普通网站反向代理不能直接替代这里的正向代理入口。

当前默认部署使用 IPv4，暂不支持仅提供 IPv6 地址的上游节点。节点名带有 IPv6 字样并不一定表示仅支持 IPv6；实际取决于节点服务器地址和 DNS 解析。

控制器只允许节点选择、模式切换和连接管理；内核更新、配置文件替换及监听/TUN 设置由 Rumiho 管理。经入口转发后的 mihomo 连接来源显示容器回环地址，不提供局域网设备身份统计。

## 检查与开发

```bash
docker compose ps
docker compose logs --tail=100
go test -race ./...
go vet ./...
# 使用真实内核运行完整集成测试（认证代理、WebSocket、订阅导入与重启）
MIHOMO_TEST_BINARY=/path/to/mihomo go test -race -v ./...
```

本地开发需要 Go 1.25+、mihomo，以及构建前端用的 Node 24 / pnpm 11.20.0。`frontend/` 是直接修改的 zashboard Vue 源码，登录和订阅管理都使用同一个应用路由，不再嵌套 iframe。先构建前端：

```bash
cd frontend
corepack pnpm install --frozen-lockfile --ignore-scripts
corepack pnpm type-check
corepack pnpm build
cd ..
```

环境变量：

```bash
CONFIG_FILE=./config.yaml DATA_DIR=./cache \
MIHOMO_BINARY=/path/to/mihomo DASHBOARD_DIR=./frontend/dist \
RULES_DIR=./third_party/rules go run ./cmd/rumiho
```

`LISTEN_ADDR` 默认 `:8080`。应用只使用一份 YAML 持久化，不需要数据库、Node.js 运行服务或 Docker socket。Docker 会在独立构建阶段编译前端，只将静态文件复制进最终镜像；运行时不包含 Node。`scripts/vendor_assets.py` 用于校验与恢复国内规则，常规构建不需要运行它。前端开发说明见 [frontend/README.md](frontend/README.md)。

本地镜像约 77 MiB（未压缩）。早期仅导入 12 个节点、使用内置规则时，Docker 统计内存约 26–30 MiB；完整订阅中的 Geo 数据和规则会增加内存与缓存目录占用，不能沿用纯节点测试的数值。部署预算建议从每实例 128 MiB 起，根据规则、节点数和连接负载调整；短时测试不代表最大容量。详细结果见 [LOCAL_TEST_REPORT.md](LOCAL_TEST_REPORT.md)。

实现范围见 [PLAN.md](PLAN.md)。第三方说明见 [third_party/NOTICE.md](third_party/NOTICE.md)。
