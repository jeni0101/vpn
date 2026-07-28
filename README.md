# 个人自用 WireGuard VPN

这是一个在本机管理、部署到新加坡 Ubuntu 24.04 LTS 云服务器的双栈 WireGuard VPN。Windows、macOS、iOS、Android 使用官方 WireGuard 客户端，IPv4 和 IPv6 流量均从新加坡服务器出口。

## 已实现

- 原生 WireGuard，监听 `51999/udp`。
- IPv4 隧道 `10.66.0.0/24`，使用 NAT44。
- IPv6 隧道 `fd66:66:66::/64`，使用 NAT66。
- 四台设备使用固定地址、独立密钥对和独立 PSK。
- 本地 `vpnctl` 通过 SSH 部署和管理服务器。
- peer 新增、导出、撤销和两阶段轮换。
- nftables peer 隔离，并保留 TCP 80/443、UDP 443 给网站。
- 部署前硬性检查公网 IPv6、端口、sudo 和冲突防火墙。
- 原子 WireGuard 更新、部署自动回滚、`age` 加密备份与恢复。
- Bash、ShellCheck、Bats 和双栈 network namespace 集成测试。

项目不会自行实现 VPN 协议，也不会部署公网 Web 管理面。

## 快速开始

本项目已经生成 Git 忽略的 `config/local.env`。先填写真实的服务器、SSH 和 `age` 配置，然后执行：

```bash
scripts/vpnctl server preflight
scripts/vpnctl server deploy

scripts/vpnctl peer add windows
scripts/vpnctl peer export windows --file

scripts/vpnctl peer add ios
scripts/vpnctl peer export ios --qr
```

完整部署步骤见 [docs/deployment.md](./docs/deployment.md)，四端设置见 [docs/clients.md](./docs/clients.md)。

## 常用命令

```bash
scripts/vpnctl server status
scripts/vpnctl peer list
scripts/vpnctl peer rotate prepare ios
scripts/vpnctl peer export ios --qr
scripts/vpnctl peer rotate activate ios --yes
scripts/vpnctl peer revoke ios --yes
scripts/vpnctl backup create
```

所有会改变 peer 的操作都会先创建加密备份。撤销、轮换激活和恢复必须显式传入 `--yes`。

## 本地测试

```bash
tests/run.sh
scripts/vpnctl lab test
```

第一条运行语法、ShellCheck、秘密扫描、权限和 Bats 测试。第二条通过 `sudo` 创建临时 network namespace，验证 WireGuard 双栈、NAT44、NAT66、peer 隔离和撤销，结束后自动清理。

## 安全边界

- `config/local.env`、`state/`、`secrets/`、`exports/` 和 `backups/` 均不进入 Git。
- 客户端私钥只在本机生成；服务端私钥只在服务器生成。
- 二维码等同于客户端密码，只在终端临时显示。
- VPN 保护设备到新加坡服务器之间的流量；服务器之后仍应使用 HTTPS。
- 使用前必须确认云服务商条款及适用法律。

## 文档

- [PROJECT.md](./PROJECT.md)：技术设计和实现说明
- [docs/deployment.md](./docs/deployment.md)：服务器部署
- [docs/clients.md](./docs/clients.md)：四端接入和泄漏测试
- [docs/operations.md](./docs/operations.md)：日常运维和 peer 管理
- [docs/recovery.md](./docs/recovery.md)：备份、恢复和故障处理

WireGuard 四端客户端下载以[官方安装页](https://www.wireguard.com/install/)为准。
