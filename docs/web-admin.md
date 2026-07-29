# TNest VPN Web 管理后台

## 部署前 DNS

在 DNS 控制台创建：

```text
vpn.example.com  A     203.0.113.10
vpn.example.com  AAAA  2001:db8::10
```

必须等待公网解析生效。部署程序会从本机和服务器同时核对 A/AAAA；不一致时停止，因此不会申请错误证书或修改现有站点。

## 无损部署

```bash
scripts/vpnctl web preflight
scripts/vpnctl web build
scripts/vpnctl web deploy
scripts/vpnctl web init-admin
```

`web deploy` 的固定顺序：

1. 创建现有 CLI 与服务端的 age 加密备份。
2. 安装 managerd，但设为只读。
3. 把本地四个旧 peer 导入 SQLite；私钥仍只在原设备/本机。
4. 对照内核 `wg0` 核验 peer 数量、公钥、PSK、IPv4 `/32` 和 IPv6 `/128`。
5. 只有全部一致才启用 manager 写入。
6. 安装低权限 Web 服务、独立 Nginx server block，并为 `vpn.example.com` 申请证书。

管理面监听 `127.0.0.1:8787`，公网只经 Nginx HTTPS 访问。部署不改动 `example.com` 站点、Docker 或 `127.0.0.1:8088`。

## 初始化管理员

`web init-admin` 会在终端安全读取至少 14 位密码，然后只显示一次：

- TOTP provisioning URI；
- 10 个一次性恢复码。

把 URI 导入密码管理器或认证器，将恢复码离线保存。后台不提供邮件找回。

## 配置方式

- Windows/Android 自定义客户端：创建“注册文件”，下载 `.tnestvpn`。客户端本机生成私钥和 PSK，10 分钟内使用一次。
- 官方 WireGuard/iOS/macOS：创建“标准配置”或二维码。配置下载一次即销毁，最长保留 10 分钟。
- 旧设备显示“外部持有私钥”，无法重新下载旧配置；丢失时执行轮换。

新增、轮换、撤销及配置下载均要求当前 TOTP。设备撤销后地址隔离 7 天。

## 服务与数据

```text
/usr/local/bin/personal-vpn-managerd
/usr/local/bin/personal-vpn-web
/var/lib/personal-vpn/manager.db
/var/lib/personal-vpn-web/auth.db
/etc/personal-vpn/manager.key
/etc/personal-vpn-web/auth.key
```

manager 每 30 秒采集 WireGuard 计数：服务端 RX 是设备上传，服务端 TX 是设备下载。小时数据保留 400 天，日数据保留 5 年，月数据长期保留。

Web 的流量页提供 24 小时、7 天和 30 天范围，并可按设备筛选。图表只展示 manager 启用后的真实增量；空时间桶显示为零，不补造部署前数据。页面上的最近速率根据相邻两次 `stats_updated_at` 采样计算，是最近约 30 秒的平均值。

“网络测试”使用固定版本的 Cloudflare 浏览器测速组件，测试当前浏览器实际使用的网络路径。默认轻量配置最多约 40 MB、30 秒后自动停止，不进行 TURN 丢包测试，也不在本项目中保存结果。测速请求及完成结果会发送给 Cloudflare。

审计页记录成功登录、退出和设备管理动作，只保存时间、管理员、来源 IP、目标设备和操作类型，不记录密码、TOTP、密钥、PSK 或配置正文。

Web 启用后，旧 `vpnctl peer add/revoke/rotate` 会拒绝直接写入。旧 CLI 仍是恢复入口；只有明确停用 manager 后才能使用。

## 回滚

若部署在启用 manager 写入前失败，现有 WireGuard 完全未变。若启用后需要回滚：

```bash
scripts/vpnctl backup verify <备份目录>
scripts/vpnctl restore <备份目录> --yes
```

恢复包包含控制面数据库、认证库、服务文件和密钥，但不包含本地 age identity、Windows 证书、Android keystore 或发布签名私钥。
