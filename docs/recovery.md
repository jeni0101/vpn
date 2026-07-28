# 备份、恢复与故障处理

## 验证备份

```bash
scripts/vpnctl backup verify backups/<timestamp>
```

验证包括 SHA-256、age 解密和 tar 内容检查，全程不把明文归档写入磁盘。

`secrets/backup-age-identity.txt` 不在备份中，必须使用部署时保存的离线副本。恢复前把它放回配置记录的路径并设为 `0600`。

## 完整恢复

恢复会覆盖当前项目的服务器配置和本地 peer 状态：

```bash
scripts/vpnctl restore backups/<timestamp> --yes
```

流程：

1. 验证目标备份。
2. 自动创建当前状态的加密备份。
3. 把服务器归档流式解密到远端临时目录。
4. 校验 WireGuard 密钥、配置和 nftables。
5. 保存当前远端项目文件并安排两分钟自动回滚。
6. 恢复服务并通过新 SSH 会话验证。
7. 取消回滚并恢复本地状态。

如果 SSH 验证失败，等待两分钟后使用云控制台检查自动回滚结果。

## 部署时失联

- 不要连续重试部署。
- 等待两分钟。
- 从云控制台检查 `personal-vpn-rollback-*` timer/service。
- 检查 `/var/backups/personal-vpn/`。
- 必要时手工运行：

```bash
sudo /usr/local/sbin/personal-vpn-rollback \
  /var/backups/personal-vpn/<deployment-timestamp>
```

## WireGuard 无握手

依次检查：

1. 云安全组是否允许 `51999/udp`。
2. 客户端 Endpoint 是否为正确公网 IPv4。
3. `scripts/vpnctl server status` 是否显示 `wg0`。
4. 服务端和客户端公钥、PSK 是否来自同一份配置。
5. 客户端系统时间是否正确。

## 有握手但不能上网

检查：

- `net.ipv4.ip_forward` 和 `net.ipv6.conf.all.forwarding` 是否为 1。
- 三个 `personal_vpn_*` nftables 表是否存在。
- WAN 网卡名是否仍与默认路由一致。
- IPv6 默认路由与公网 IPv6 是否有效。
- 路径 MTU；只有确认存在问题后，才把 1420 调整到 1380 或更低。
