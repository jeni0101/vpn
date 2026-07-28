# 个人自用 WireGuard VPN 技术说明

版本：2026-07-28

## 1. 架构

WireGuard 外层固定使用新加坡服务器公网 IPv4 与 `51999/udp`。隧道内提供完整双栈：

```text
Windows  10.66.0.10 / fd66:66:66::10 ─┐
macOS    10.66.0.11 / fd66:66:66::11 ─┤
iOS      10.66.0.12 / fd66:66:66::12 ─┼─ WireGuard ─> Singapore
Android  10.66.0.13 / fd66:66:66::13 ─┘                │
                                                       ├─ NAT44
                                                       └─ NAT66
```

服务端地址是 `10.66.0.1/24` 和 `fd66:66:66::1/64`。客户端使用 `0.0.0.0/0, ::/0` 全局路由、双栈公共 DNS、MTU 1420 和 25 秒 keepalive。

服务端每个 peer 的 `AllowedIPs` 只包含该设备的 IPv4 `/32` 和 IPv6 `/128`。nftables 明确拒绝 `wg0 -> wg0`，阻止设备互访。

## 2. 本地状态与秘密

| 位置 | 内容 | Git |
| --- | --- | --- |
| `config/local.env` | SSH、Endpoint、WAN、age 配置 | 忽略 |
| `state/peers/` | 活动 peer 的非秘密元数据 | 忽略 |
| `state/pending/` | 待激活轮换元数据 | 忽略 |
| `secrets/peers/` | 活动客户端私钥、PSK、配置 | 忽略 |
| `secrets/pending/` | 待激活客户端材料 | 忽略 |
| `exports/` | 临时导出的客户端配置 | 忽略 |
| `backups/` | age 加密备份 | 忽略 |

运行时目录权限为 `0700`，秘密文件权限为 `0600`。客户端密钥在本机使用 `wg` 生成，服务端密钥在服务器部署脚本中生成。

## 3. CLI

唯一公开入口是 `scripts/vpnctl`：

```text
vpnctl config init
vpnctl lab test

vpnctl server preflight
vpnctl server deploy
vpnctl server status

vpnctl peer add <windows|macos|ios|android>
vpnctl peer export <name> --file
vpnctl peer export <name> --qr
vpnctl peer list
vpnctl peer revoke <name> --yes
vpnctl peer rotate prepare <name>
vpnctl peer rotate activate <name> --yes

vpnctl backup create
vpnctl backup verify <archive-directory>
vpnctl restore <archive-directory> --yes
```

存在 `pending-rotation` 时，`peer export` 自动导出待轮换配置，并在终端明确提示；否则导出活动配置。

## 4. 远端部署

`server preflight` 要求：

- Ubuntu 24.04 LTS。
- SSH 用户具有无交互 sudo。
- 恰好一个 IPv4 默认路由网卡。
- 服务器具有全局 IPv6、IPv6 默认路由，并可通过 ICMPv6 访问公网。
- `51999/udp` 未被非本项目服务占用。
- UFW、firewalld 未启用。
- 不存在未经本项目管理的 nftables 表。
- 用户已经在配置中确认云安全组和云控制台/快照。

`server deploy` 在远端安装 WireGuard、nftables 和自动安全更新，启用 IPv4/IPv6 forwarding，并部署：

- `/etc/wireguard/wg0.conf`
- `/etc/nftables.d/personal-vpn.nft`
- `/etc/sysctl.d/70-personal-vpn.conf`
- `personal-vpn-firewall.service`

所有配置先在临时目录渲染。WireGuard 使用 `wg-quick strip` 校验；nftables 使用重命名后的临时表执行 `nft -c`。

部署会保存旧项目文件，并通过 transient systemd timer 安排两分钟自动回滚。只有本机成功建立新 SSH 会话并读取 `wg0` 状态后，才取消回滚。

## 5. 防火墙

项目管理三个专属表：

- `inet personal_vpn_filter`
- `ip personal_vpn_nat4`
- `ip6 personal_vpn_nat6`

INPUT 默认拒绝，但允许：

- loopback、已建立连接
- ICMP、ICMPv6
- DHCP、DHCPv6 回复
- 配置的 SSH 端口
- TCP 80/443
- UDP 443
- UDP 51999

FORWARD 只限制涉及 `wg0` 的流量，其他转发继续交给网站或容器系统。项目不清空全局 nftables ruleset。

## 6. peer 事务

- `peer add`：先备份，再在 pending 目录生成材料；远端同步成功后才转为 active。
- `peer revoke`：先备份，原子删除服务端 peer，成功后清理本地活动秘密。
- `rotate prepare`：只生成待轮换材料，不影响活动 peer。
- `rotate activate`：先备份，将新公钥和 PSK 原子替换到服务端，成功后切换本地状态。
- 服务端更新失败时恢复旧 `wg0.conf` 并重新执行 `wg syncconf`。

## 7. 备份

一次备份由一个目录组成：

```text
backups/<timestamp>/
├── local.tar.gz.age
├── server.tar.gz.age
├── SHA256SUMS
└── manifest
```

本地状态和服务端配置分别通过管道直接进入 `age`，不创建明文备份包。验证操作检查 SHA-256、age 解密及 tar 结构。恢复前自动再创建一份当前状态备份，服务端恢复也使用两分钟自动回滚。

## 8. 测试

- `tests/static.sh`：Bash、ShellCheck、秘密扫描、模板和权限。
- `tests/unit.bats`：地址映射、名称校验、CLI 及双栈配置渲染。
- `tests/lab.sh`：真实 Linux network namespace、WireGuard、NAT44、NAT66、隔离、幂等加载和撤销。

云端和四端的人工验收步骤见 [docs/clients.md](./docs/clients.md)。
