# 新加坡服务器部署

## 1. 固定前提

服务器必须满足：

- Ubuntu 24.04 LTS。
- 固定公网 IPv4、直接配置并可出站的公网 IPv6。
- 原生 Nginx 或 Caddy 至少监听 TCP 80/443 之一。
- 允许标准 Docker `iptables-nft` 布局；IPv4、IPv6 `FORWARD` 必须调用空闲或仅含本项目规则的 `DOCKER-USER`。
- 不使用 Podman、1Panel、宝塔、活动 UFW、firewalld 或其他自定义 nftables。
- `ubuntu` 用户已安装 `~/.ssh/vpn_server_ed25519.pub`，支持 `sudo -n`。
- 云控制台或救援模式可用，并已创建部署前快照。

云安全组由用户手工配置：

- SSH TCP 22 仅允许管理来源。
- TCP 80/443 和 UDP 443 保留给网站。
- IPv4 UDP 51999 允许 VPN 客户端来源；来源经常变化时允许公网。
- 允许必要 ICMP 和 ICMPv6。
- 允许 IPv4、IPv6 出站。

项目不修改网站目录、Nginx/Caddy、证书或云安全组。

## 2. 准备本地配置

提供不带 CIDR 前缀的真实公网地址：

```bash
scripts/vpnctl config prepare <public-ipv4> <public-ipv6> \
  --cloud-firewall-ready \
  --recovery-ready
```

两个确认参数表示云安全组、快照和控制台恢复已经由人工完成。命令会：

- 固定使用 `ubuntu@<public-ipv4>:22`。
- 固定使用 `/home/ubuntu/.ssh/vpn_server_ed25519`。
- 把 IPv4 同时设为 SSH 地址和 WireGuard Endpoint。
- 保存预期 IPv6，供远端地址和真实出口核对。
- 自动生成 `secrets/backup-age-identity.txt` 和对应 recipient。
- 原子写入权限为 `0600` 的 `config/local.env`。

同一组地址可重复执行。已有配置指向其他服务器，或已有状态与新目标冲突时，命令拒绝覆盖。

立即把 `secrets/backup-age-identity.txt` 复制到离线安全位置。该身份文件不会进入 VPN 备份；丢失本机和离线副本后，已有备份无法解密。

## 3. 部署前检查

```bash
scripts/vpnctl server preflight
```

预检通过必须同时满足：

- SSH、公钥登录、`sudo -n` 和 Ubuntu 版本正确。
- IPv4、IPv6 默认路由可用，真实出口与输入地址完全一致。
- 输入的 IPv6 确实配置在唯一 WAN 网卡。
- `51999/udp` 未被其他服务占用。
- 网站至少监听 TCP 80/443 之一，并记录 TCP 80/443、UDP 443 当前监听集合。
- Docker 启用时，IPv4/IPv6 `DOCKER-USER` 和 `FORWARD` 调用链完整。
- 不存在 Podman、服务器面板或 Docker/本项目之外的 nftables 表。

任何失败都不得绕过。公网出口通过两个独立查询端点进行探测；两者均不可用时预检停止，不使用未经验证的结果。

## 4. 一键部署和四端初始化

```bash
scripts/vpnctl server bootstrap
```

执行顺序为：

1. 再次检查全部本地和远端条件。
2. 记录从管理机能够直接访问的网站 TCP 端口。
3. 安装 WireGuard、nftables 和自动安全更新。
4. 生成只保留在服务器的 WireGuard 私钥。
5. 应用双栈转发、NAT44、NAT66、peer 隔离及网站保留规则。
6. Docker 启用时，只向 IPv4、IPv6 `DOCKER-USER` 加入带 `personal-vpn` 注释且匹配 `wg0` 的隔离、出口和返回流量规则。
7. 使用新 SSH 会话检查 WireGuard、三个项目 nftables 表、Docker 兼容规则和部署前的网站监听。
8. 再次检查部署前可从管理机访问的网站端口。
9. 全部通过后取消两分钟自动回滚。
10. 按 Windows、iOS、macOS、Android 创建独立 peer，并导出到 `exports/`。

若网站原本只允许 CDN 等特定来源访问，管理机外部探测可能不可达；此时仍会严格验证服务器上的原有监听。部署前能够从管理机访问的端口，部署后必须继续可访问。

`bootstrap` 可以安全重试。已完整创建的 peer 会保留原密钥并跳过；发现 pending 或不完整状态时停止，避免静默覆盖。

项目不修改 Docker 网络、容器或 NAT 规则。回滚和停止服务只按完整参数及 `personal-vpn:*` 注释删除项目自己的 `DOCKER-USER` 规则。

## 5. 部署后检查

```bash
scripts/vpnctl server status
scripts/vpnctl peer list
ls -l exports/
```

预期存在：

```text
exports/windows.conf
exports/ios.conf
exports/macos.conf
exports/android.conf
```

所有文件权限必须为 `0600`。移动端二维码只在需要时显示：

```bash
scripts/vpnctl peer export ios --qr
scripts/vpnctl peer export android --qr
```

如果部署期间 SSH 或网站验证失败，不要立即重试。等待至少两分钟，让自动回滚执行，再通过云控制台检查。
