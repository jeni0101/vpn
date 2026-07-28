# 服务器部署

## 1. 云服务器要求

- Ubuntu 24.04 LTS。
- 固定公网 IPv4。
- 公网 IPv6 地址和 IPv6 默认路由。
- 可以使用云控制台或救援模式。
- SSH 用户支持密钥登录和无交互 `sudo -n`。

云安全组至少配置：

- 从当前管理地址允许 SSH。
- 从任意客户端来源允许 `51999/udp` 的 IPv4 入站。
- 允许必要 ICMP 和 ICMPv6。
- 网站部署时再开放 TCP 80/443 和 UDP 443。

先创建云服务器快照，再把 `config/local.env` 中的：

```text
CLOUD_FIREWALL_CONFIRMED="yes"
CLOUD_RECOVERY_CONFIRMED="yes"
```

设为 `yes`。这两个值代表人工确认，不是自动探测结果。

## 2. 本地 age 密钥

建议把 age identity 放在本项目之外并离线备份：

```bash
age-keygen -o /safe/path/personal-vpn-age-key.txt
age-keygen -y /safe/path/personal-vpn-age-key.txt
```

第二条输出填入 `AGE_RECIPIENT`，identity 路径填入 `AGE_IDENTITY_FILE`。identity 文件不得提交到 Git 或发到服务器。

## 3. 本地配置

如果 `config/local.env` 不存在：

```bash
scripts/vpnctl config init
```

填写：

- `SSH_HOST`
- `SSH_USER`
- `SSH_PORT`
- `SSH_IDENTITY_FILE`
- `VPN_ENDPOINT_IPV4`
- `WAN_INTERFACE`，不确定时留空自动探测
- `AGE_RECIPIENT`
- `AGE_IDENTITY_FILE`

端口、隧道网段和地址是固定设计，CLI 会拒绝改变它们。

## 4. 预检

```bash
scripts/vpnctl server preflight
```

预检会实际探测公网 IPv6。失败时不要跳过；先修复云网络、IPv6 路由、sudo、端口或防火墙冲突。

为避免覆盖未知安全策略，检测到活动 UFW、firewalld 或其他 nftables 表时会停止。应先人工迁移现有规则，再重试。

## 5. 部署

```bash
scripts/vpnctl server deploy
scripts/vpnctl server status
```

部署会安装软件包、生成服务端密钥、应用双栈转发与 NAT，并启用开机启动。防火墙切换后必须在两分钟内通过新 SSH 会话验证；CLI 会自动完成验证和取消回滚。

如果部署过程中连接断开，不要立即重复执行。等待至少两分钟，让自动回滚恢复旧配置，再通过云控制台检查。

## 6. 第一台设备

peer 变更要求 age 备份已配置：

```bash
scripts/vpnctl peer add windows
scripts/vpnctl peer export windows --file
```

先完成 Windows 验收，再添加 iOS：

```bash
scripts/vpnctl peer add ios
scripts/vpnctl peer export ios --qr
```
