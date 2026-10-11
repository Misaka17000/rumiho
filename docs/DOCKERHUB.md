# Rumiho

单管理员的轻量 mihomo 代理容器，内置定制 zashboard。网页和带认证的 HTTP 代理共用 18080 端口；支持订阅导入、分组选择、规则和实时连接。

版本 **0.1.3** / `latest`，平台 **Linux amd64 / x86_64**。无需数据库、UID/GID 或 `.env`。只需要同级的 `compose.yaml` 和 `config.yaml` 两个文件。

## compose.yaml

```yaml
services:
  rumiho:
    image: misakaxie/rumiho:0.1.3
    platform: linux/amd64
    restart: unless-stopped
    init: true
    ports:
      - "${BIND_ADDRESS:-0.0.0.0}:${PORT:-18080}:8080"
    volumes:
      - type: bind
        source: .
        target: /config
        bind:
          create_host_path: false
    cap_drop:
      - ALL
    cap_add:
      - CHOWN
      - DAC_OVERRIDE
      - SETUID
      - SETGID
    security_opt:
      - no-new-privileges:true
    stop_grace_period: 15s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

## config.yaml

与 Compose 放在同一目录，填写你自己的账号密码：

```yaml
# 复制为与 compose.yaml 同级的 config.yaml。页面修改直接写回此文件。
# 手动编辑时先停止容器，修改后再启动。
admin:
  username: admin
  # 填写至少 8 个字符的密码；按明文保存，不需要生成哈希。
  password: ""

settings:
  subscription_url: ""
  mode: rule
  update_hours: 24
  public_url: ""

# 代理用户名直接使用 admin.username。
# 以下留空则在首次启动生成。代理密码与登录密码独立。
proxy_password: ""
core_secret: ""
# 订阅更新后自动保存节点、分组和规则。
nodes: []
```

## 启动

```bash
docker compose pull
docker compose up -d --wait
```

打开 `http://服务器IP:18080`，登录后导入 Clash/mihomo YAML 订阅。在「代理」选择节点与分组；「订阅与接入」可修改接入地址并复制带认证的 HTTP 代理链接。需要换端口时使用 `PORT=18081 docker compose up -d --wait`。

账号、订阅、节点与规则保存在同级 `config.yaml`，页面修改会写回。同级 `cache/` 自动生成，仅用于内核运行文件和下载缓存。配置目录权限自动处理，应用随后以普通用户运行。

HTTP 代理用户名与登录用户名一致，代理密码独立生成。「重置链接」会轮换代理密码；修改登录密码会退出所有网页登录会话。

从旧版升级时替换 Compose 文件；将原 config 文件改名为 config.yaml，或者把原 config/config.yaml 和 config/cache 分别复制到同级的 config.yaml 和 cache。旧的 CONFIG_DIR/PUID/PGID 不再使用。保留原有配置内容，不要用示例覆盖。备份 config.yaml 和 cache 即可，手动编辑 YAML 前先停止容器。

只需向接入设备开放入口 TCP 端口。HTTP 入口适用于可信局域网或 VPN；普通网站反向代理不能直接代替 HTTP 正向代理。

## 组件与许可

mihomo v1.19.17（GPL-3.0），来自固定摘要的官方镜像；源码：https://github.com/MetaCubeX/mihomo/archive/refs/tags/v1.19.17.tar.gz 。zashboard v3.29.1（MIT）来自 https://github.com/Zephyruso/zashboard 并经过界面和认证集成定制。许可与组件说明随镜像提供于 `/app/licenses/` 和前端许可文件。
