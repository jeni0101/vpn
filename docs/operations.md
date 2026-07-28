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

## 更新

- 每周检查握手、系统更新、磁盘、云流量和异常 SSH 登录。
- 每月完成四端 IPv4、IPv6、DNS 泄漏检查。
- 每季度演练轮换、撤销和临时服务器恢复。
- Ubuntu 或 WireGuard 更新需要重启时，先创建备份并确认云控制台可用。

## 网站共存

VPN 不监听 TCP 80/443 或 UDP 443。网站或容器引入新的 nftables 表后，后续 `server preflight` 会要求人工复核；不要为了绕过预检而清空网站规则。
