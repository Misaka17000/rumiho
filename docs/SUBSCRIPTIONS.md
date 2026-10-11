# 订阅处理依据与边界

## 参考源码

核查日期：2026-10-07。参考行为设计，未引入这些项目的桌面框架、脚本系统或新运行服务。

- Clash Verge Rev，提交 `ad2baf254a7d35ecc1bb2776582c7bc20f34498d`：
  - [`PrfItem::from_url`](https://github.com/clash-verge-rev/clash-verge-rev/blob/ad2baf254a7d35ecc1bb2776582c7bc20f34498d/src-tauri/src/config/prfitem.rs)：下载、HTTP 状态检查、BOM 兼容、YAML 检查，保留整份配置；还支持流量/到期元信息。
  - [`perform_profile_update`](https://github.com/clash-verge-rev/clash-verge-rev/blob/ad2baf254a7d35ecc1bb2776582c7bc20f34498d/src-tauri/src/feat/profile.rs)：按网络策略重试完整的下载与解析过程，下载成功后的保存失败不会触发再次下载。
  - [`enhance`](https://github.com/clash-verge-rev/clash-verge-rev/blob/ad2baf254a7d35ecc1bb2776582c7bc20f34498d/src-tauri/src/enhance/mod.rs)：在订阅配置基础上合并应用管理的配置，保留节点、分组和规则之间的关系。
- mihomo，与运行内核相同的 `v1.19.17`：
  - [`resource.Fetcher`](https://github.com/MetaCubeX/mihomo/blob/v1.19.17/component/resource/fetcher.go)：缓存优先、先解析再写入、内容相同时不重新应用。
  - [`ProxySetProvider`](https://github.com/MetaCubeX/mihomo/blob/v1.19.17/adapter/provider/provider.go)：这是节点提供器，解析 `proxies`；不能拿这种语义代替完整订阅配置导入。

## 本项目采用的流程

1. 使用 Clash 兼容 User-Agent 下载，保留 TLS 校验，限制单次请求为 25 秒、响应体为 8 MiB；HTTP 重定向采用 Go 标准库上限。
2. 下载或 YAML 解析失败时，若已有可用节点，再尝试经当前代理下载。HTTP 200 的错误页面、不完整正文也会进入回退。
3. 保留 `proxies`、`proxy-groups`、`rules`、`rule-providers` 和 Geo 数据设置。分组嵌套、`include-all`、筛选条件、规则顺序交由 mihomo 解释；不在网关实现另一套分流引擎。
4. 实例掌管监听地址、端口、认证和管理接口；不导入上游 DNS/TUN/额外监听器。HTTP 规则集缓存路径由实例生成，不能使用订阅指定的任意本地路径。
5. 生成候选配置，运行本实例的 `mihomo -t` 校验后才应用并保存。失败时保留或恢复最后可用配置。启动使用持久化配置，不强制在线获取订阅。
6. 相同配置仅更新时间，不重载内核；只有缺少分组/规则的节点订阅才补充对应的默认配置。管理员选择的刷新周期不被订阅响应头覆盖。

暂不支持 `proxy-providers`、Base64/URI 节点转换、本地文件规则集或脚本扩展；明确报错，不悄悄抽取部分节点。流量与到期元信息尚未增加到界面。单用户实例不需要多订阅管理服务或订阅转换服务。

## 回归验证

`internal/rumiho/subscription_test.go` 覆盖完整分组与规则保留、运行设置保护、缓存路径隔离、BOM、错误页面/截断下载回退、双路径失败保留缓存，以及真实 mihomo 的嵌套分组分流、自动组、错误引用拒绝、刷新、重启与清空订阅。

用户真实订阅复验：12 个节点、4 个上游分组、565 条规则；持久化分组和规则与原始订阅一致。`GLOBAL` 是 mihomo 自带的全局选择组，因此控制器总共显示 5 个组。测试私有数据留在 `.test-data/`，文档不包含订阅 token 或节点凭据。
