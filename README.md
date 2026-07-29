# 个人自用 WireGuard VPN

这是一个在本机管理、部署到新加坡 Ubuntu 24.04 LTS 云服务器的双栈 WireGuard VPN。项目同时包含 TNest VPN Web 管理后台、Windows x64 客户端和 Android 8+ 客户端；iOS/macOS 继续使用官方 WireGuard。

## 已实现

- 原生 WireGuard，监听 `51999/udp`。
- IPv4 隧道 `10.66.0.0/24`，使用 NAT44。
- IPv6 隧道 `fd66:66:66::/64`，使用 NAT66。
- 四台设备使用固定地址、独立密钥对和独立 PSK。
- 本地 `vpnctl` 通过 SSH 部署和管理服务器。
- peer 新增、导出、撤销和两阶段轮换。
- nftables peer 隔离，并保留 TCP 80/443、UDP 443 给网站。
- 只需提供公网 IPv4/IPv6，即可生成本地配置、age 身份并一键部署。
- 部署前硬性核对实际双栈出口、网站监听、端口、sudo 和冲突防火墙。
- 原子 WireGuard 更新、部署自动回滚、`age` 加密备份与恢复。
- 自动创建并导出四端 peer，重复 bootstrap 不轮换已有密钥。
- 兼容标准 Docker `iptables-nft` 布局，只在 `DOCKER-USER` 加入带项目标记的 `wg0` 规则。
- Bash、ShellCheck、Bats、备份流和双栈 network namespace 集成测试。
- root 权限 `personal-vpn-managerd` 只通过 Unix Socket 暴露固定管理操作。
- 低权限 `personal-vpn-web`、Argon2id 密码、TOTP、恢复码、CSRF/CSP 和审计日志。
- 动态地址池 `.14～.254`、7 天地址隔离、一次性注册/标准配置和流量统计。
- Windows WPF 与 Android Compose 客户端均复用 WireGuard 官方嵌入方案，不自行实现协议。

## 快速开始

云安全组、快照和控制台恢复准备完成后，使用不带 CIDR 的公网地址初始化。命令固定使用 `vpnadmin@<IPv4>:22` 和本机的 `~/.ssh/vpn_server_ed25519`：

```bash
scripts/vpnctl config prepare <public-ipv4> <public-ipv6> \
  --cloud-firewall-ready \
  --recovery-ready
scripts/vpnctl server preflight
scripts/vpnctl server bootstrap
```

`bootstrap` 部署服务器、验证网站、创建四端 peer，并把四份 `0600` 配置写到 `exports/`。自动生成的 age identity 必须另存到离线安全位置，它不会进入由自身加密的备份。

原有四端稳定后，为 `vpn.example.com` 配置 A/AAAA，再部署管理面：

```bash
scripts/vpnctl web preflight
scripts/vpnctl web deploy
scripts/vpnctl web init-admin
```

部署先以只读模式导入并逐项核对现有 peer；公钥、PSK 或 AllowedIPs 任一不一致都会停止，不会重建现有客户端密钥。

完整部署步骤见 [docs/deployment.md](./docs/deployment.md)，四端设置见 [docs/clients.md](./docs/clients.md)。

## 常用命令

```bash
scripts/vpnctl server status
scripts/vpnctl peer list
scripts/vpnctl peer export ios --qr
scripts/vpnctl peer rotate prepare ios
scripts/vpnctl peer rotate activate ios --yes
scripts/vpnctl peer revoke ios --yes
scripts/vpnctl backup create
scripts/vpnctl web status
```

所有会改变 peer 的操作都会先创建加密备份。撤销、轮换激活和恢复必须显式传入 `--yes`。

## 本地测试

```bash
tests/run.sh
scripts/vpnctl lab test
```

第一条运行语法、ShellCheck、秘密扫描、配置、预检、备份、权限和 Bats 测试。第二条通过 `sudo` 创建临时 network namespace，验证 WireGuard 双栈、NAT44、NAT66、网站端口、peer 隔离和撤销，结束后自动清理。

## 安全边界

- `config/local.env`、`state/`、`secrets/`、`exports/` 和 `backups/` 均不进入 Git。
- VPN 备份只包含 peer 秘密，不包含 age identity、GitHub deploy key 或其他无关凭据。
- 不删除或重写 Docker 的 NAT/转发规则；未知容器或防火墙布局仍会阻止部署。
- 客户端私钥只在本机生成；服务端私钥只在服务器生成。
- Web 一次性配置只保留到首次下载或 10 分钟；注册 Token 仅保存 SHA-256。
- manager 每次变更前生成 age 加密的 `wg0.conf` 快照，完整备份会短暂停止控制面以取得一致的 SQLite 文件。
- 二维码等同于客户端密码，只在终端临时显示。
- VPN 保护设备到新加坡服务器之间的流量；服务器之后仍应使用 HTTPS。
- 使用前必须确认云服务商条款及适用法律。

## 文档

- [PROJECT.md](./PROJECT.md)：技术设计和实现说明
- [docs/deployment.md](./docs/deployment.md)：服务器部署
- [docs/clients.md](./docs/clients.md)：四端接入和泄漏测试
- [docs/operations.md](./docs/operations.md)：日常运维和 peer 管理
- [docs/recovery.md](./docs/recovery.md)：备份、恢复和故障处理
- [docs/web-admin.md](./docs/web-admin.md)：Web 管理面部署和迁移
- [docs/windows-client.md](./docs/windows-client.md)：Windows 客户端构建与使用
- [docs/android-client.md](./docs/android-client.md)：Android 客户端构建与使用
- [docs/releases.md](./docs/releases.md)：离线签名与更新清单

WireGuard 四端客户端下载以[官方安装页](https://www.wireguard.com/install/)为准。
