# 日常运维

## 状态

```bash
scripts/vpnctl server status
scripts/vpnctl peer list
```

`peer list` 只查询公钥和最近握手时间，不从服务器读取 PSK。

## 新增和导出

```bash
scripts/vpnctl peer add android
scripts/vpnctl peer export android --qr
```

`--file` 会写入 Git 忽略的 `exports/`。`--qr` 只显示在终端。

首次上线推荐直接执行：

```bash
scripts/vpnctl server bootstrap
```

它会创建缺少的四端 peer、跳过已经完整存在的 peer，并重新导出配置。完整的 `pending-add` 会复用原密钥继续提交；待轮换或残缺状态会停止，不会自动轮换密钥。

## 密钥轮换

```bash
scripts/vpnctl peer rotate prepare ios
scripts/vpnctl peer export ios --qr
scripts/vpnctl peer rotate activate ios --yes
```

存在待轮换材料时，`peer export` 导出新配置。先在设备中导入新配置，再执行 activate；激活会立即使旧配置失效。

## 撤销丢失设备

```bash
scripts/vpnctl peer revoke ios --yes
```

撤销先创建加密备份，然后同时删除服务端 peer 和本地活动秘密。其他设备不应中断。

## 备份

```bash
scripts/vpnctl backup create
scripts/vpnctl backup verify backups/<timestamp>
```

每次 peer 变更前 CLI 自动备份。每月至少手工验证一次最新备份。

自动生成的 `secrets/backup-age-identity.txt` 必须有离线副本。备份只收录 `state/`、peer secrets 和本地配置，不收录 age identity、GitHub deploy key 或其他无关秘密。

## 更新

- 每周检查握手、系统更新、磁盘、云流量和异常 SSH 登录。
- 每月完成四端 IPv4、IPv6、DNS 泄漏检查。
- 每季度演练轮换、撤销和临时服务器恢复。
- Ubuntu 或 WireGuard 更新需要重启时，先创建备份并确认云控制台可用。

## 网站共存

VPN 不监听 TCP 80/443 或 UDP 443。部署前已有的网站监听会在取消回滚前重新验证。

标准 Docker 使用 `DOCKER-USER` 与 VPN 共存。项目规则都带 `personal-vpn:*` 注释且只匹配 `wg0`，不会删除 Docker 网络规则。Docker 重启或升级后应运行：

```bash
sudo systemctl reload personal-vpn-firewall.service
scripts/vpnctl server status
```

改用 Podman、服务器面板，或出现 Docker/本项目之外的 nftables 表后，`server preflight` 会停止；不要为了绕过预检而清空业务规则。
