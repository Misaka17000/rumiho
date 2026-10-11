# Docker Hub 部署

公开镜像：[`misakaxie/rumiho:0.1.3`](https://hub.docker.com/r/misakaxie/rumiho)，同时提供 `latest`，支持 **Linux amd64 / x86_64**。

## 两个文件即可启动

把发布包解压到服务器。在解压后的 `rumiho` 目录中，将 `config.example.yaml` 复制为 **与 compose.yaml 同级的 config.yaml**：

```bash
cp -n config.example.yaml config.yaml
```

目录结构：

```text
rumiho/
├── compose.yaml
├── config.yaml
└── cache/          # 首次启动自动生成
```

不需要创建 config 目录，不需要 `.env`、UID/GID 或手动调整权限。

编辑 `config.yaml`，填写自己的账号密码：

```yaml
admin:
  username: admin
  password: "请替换成你自己的密码，至少8个字符"

settings:
  subscription_url: ""
  mode: rule
  update_hours: 24
  public_url: ""
```

然后启动：

```bash
docker compose pull
docker compose up -d --wait
```

打开 `http://服务器IP:18080`，登录后添加订阅。网页与 HTTP 代理共用 **18080 TCP**，防火墙/安全组只需向接入设备开放该端口。需要更换端口时，可以使用可选的 `.env.example`，或执行 `PORT=18081 docker compose up -d --wait`。

`public_url` 留空时使用浏览器访问地址生成代理链接，也可以在「订阅与接入」改成设备可达的地址。HTTP 代理用户名与登录用户名一致，代理密码独立生成；点击「重置链接」会更换代理密码。

## 配置保存

账号、订阅、节点、分组和规则都保存在同级 `config.yaml`，页面修改直接写回。`cache/` 仅用于内核运行文件、节点选择缓存和下载数据。备份 `config.yaml` 与 `cache/` 即可；手工编辑 YAML 时先停止容器。

Compose 挂载当前部署目录，保证保存配置时能原子替换文件。启动脚本仅处理配置文件、缓存和父目录本身，不递归修改部署目录中的其他文件；随后以普通用户运行应用。

## 从旧版本升级

用新版 Compose 替换旧文件，保留已有配置内容。新版不再读取 `.env` 中的 `CONFIG_DIR`、`PUID` 或 `PGID`；这些行可以删除。若旧 `.env` 设置了 `PORT=8080`，请改成 `18080` 或删除该行。

先停止旧实例：

```bash
docker compose down
```

如果你原来创建的是无后缀的 **config 文件**，直接改名，不用重写：

```bash
mv -n config config.yaml
```

如果原来用的是 **config 目录**，保留它作为备份，把配置和缓存复制到同级：

```bash
cp -n config/config.yaml config.yaml
cp -an config/cache .  # 如果原来有缓存目录
```

随后运行 `docker compose pull` 和 `docker compose up -d --wait`。后续升级只需要更换镜像标签并重新拉取启动，挂载文件会保留。

`docker compose logs --tail=100 rumiho` 可查看启动错误。HTTP 入口适用于可信局域网或 VPN；普通网站反向代理不能直接替代 HTTP 正向代理。

镜像只包含程序、前端与内置规则，没有预设账号或订阅。第三方许可和来源见镜像 `/app/licenses/` 与前端许可文件。
