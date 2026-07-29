# Windows 与 iPhone 接入完整教程

本文使用本项目已经生成的 WireGuard 客户端配置，不手工创建密钥，也不修改服务端参数。

项目当前约定：

| 项目 | Windows | iPhone |
|---|---:|---:|
| peer 名称 | `windows` | `ios` |
| 隧道 IPv4 | `10.66.0.10` | `10.66.0.12` |
| 隧道 IPv6 | `fd66:66:66::10` | `fd66:66:66::12` |
| 服务端隧道地址 | `10.66.0.1` | `10.66.0.1` |
| 服务端端口 | `51999/udp` | `51999/udp` |
| 路由模式 | IPv4、IPv6 全部流量走 VPN | IPv4、IPv6 全部流量走 VPN |

配置里的 `PrivateKey` 和 `PresharedKey` 相当于设备的 VPN
密码。任何拿到配置文件或二维码的人都可以冒充该设备，不能通过微信、普通邮件、公开网盘或群聊传输，也不能把配置提交到 Git。

## 一、开始前确认

先按照[服务器部署教程](deployment.md)完成部署，并在项目根目录运行：

```bash
scripts/vpnctl server status
scripts/vpnctl peer list
ls -l exports/
```

必须满足：

1. `server status` 能连接服务器，WireGuard 接口、IPv4/IPv6
   转发和项目 nftables 表均正常。
2. `peer list` 中存在状态为 `active` 的 `windows` 和 `ios`。
3. `exports/windows.conf` 与 `exports/ios.conf` 存在，权限为
   `-rw-------`，也就是 `0600`。
4. 云安全组已经放行服务器公网 IPv4 的 `51999/udp`。
5. Windows 和 iPhone 的日期、时间及时区均使用自动设置。时间严重错误会造成握手失败。

`server bootstrap` 已经一次性创建四个 peer。不要为了重新下载配置再次运行
`peer add`，只需重新导出。

## 二、Windows 配置

### 1. 导出 Windows 配置

在本项目根目录运行：

```bash
scripts/vpnctl peer export windows --file
```

命令会重新写出：

```text
exports/windows.conf
```

它包含 Windows 专用私钥和 PSK。不要打开后复制到聊天窗口，也不要和其他设备共用。

### 2. 安装官方 WireGuard

1. 在 Windows 浏览器打开
   [WireGuard 官方安装页](https://www.wireguard.com/install/)。
2. 点击 **Download Windows Installer**。
3. 运行安装程序；Windows 弹出用户账户控制时，确认发布者和下载来源后允许安装。
4. 打开 **WireGuard**。不要安装名称相似的第三方“加速器”或来历不明的客户端。

官方安装器会自动选择 x64、ARM64 或 x86 版本。普通 Intel/AMD
Windows 10/11 电脑无需手工选择 MSI。

### 3. 安全地把配置传到 Windows

优先使用只由自己控制的 U 盘、端到端加密传输或本地受信任网络传输。
传输后不要让文件继续停留在下载目录、聊天记录或同步网盘中。

导入前可以核对文件名为 `windows.conf`。不要修改其中任何密钥，也不要把
`windows.conf` 改成 `ios.conf` 后给手机使用。

### 4. 导入并连接

1. 打开 WireGuard。
2. 点击 **Add Tunnel** 旁的下拉箭头。
3. 选择 **Import tunnel(s) from file**。
4. 选择 `windows.conf`。
5. 确认隧道名称显示为 `windows`，然后点击 **Activate**。
6. 如果 Windows 再次请求管理员权限，允许官方 WireGuard 修改网络设置。

导入后，应用中应能看到以下非机密字段：

```text
Address: 10.66.0.10/32, fd66:66:66::10/128
Endpoint: <新加坡服务器公网 IPv4>:51999
AllowedIPs: 0.0.0.0/0, ::/0
PersistentKeepalive: 25
MTU: 1420
```

`AllowedIPs` 中的两个 `/0` 表示 IPv4 和 IPv6 全部经过 VPN。不要只删掉
`::/0`，否则 IPv6 可能绕过 VPN；也不要随意关闭 DNS 项。

导入成功后，删除 Windows 上用于导入的 `windows.conf` 临时文件并清空回收站。
WireGuard 应用已经保存隧道，不再依赖原文件。管理机中的 `exports/windows.conf`
仍是受保护的主副本。

### 5. 设置开机和断线行为

保持隧道处于启用状态，然后重启一次 Windows，确认登录前后均能恢复连接。
官方客户端会为已启用的隧道使用独立 Windows 服务；不要照搬网上的注册表脚本，
直接以重启实测结果为准。

本配置的 IPv4、IPv6 `/0` 路由会触发官方 Windows 客户端的全隧道阻断行为。
服务器不可达时，网络可能表现为完全断开，这是预期的防泄漏行为。需要临时恢复普通
网络时，在 WireGuard 中明确点击 **Deactivate**。

### 6. Windows 验收

连接后打开 PowerShell：

```powershell
ping 10.66.0.1
curl.exe -4 https://icanhazip.com
curl.exe -6 https://icanhazip.com
```

预期结果：

1. `10.66.0.1` 可以到达。
2. `curl.exe -4` 显示新加坡服务器公网 IPv4。
3. `curl.exe -6` 显示新加坡服务器公网 IPv6。
4. 浏览器访问 DNS 泄漏检测网站时，只看到配置中的公共解析器，不出现当前
   Wi-Fi、公司或运营商 DNS。
5. Windows 无法访问其他 peer 的隧道地址，例如 iPhone 上线后，
   `ping 10.66.0.12` 仍应失败。这是项目的设备隔离规则，不是故障。

`ping` 可能被本机或服务器策略禁用，所以公网 IPv4/IPv6、DNS 和服务端握手记录
比单独的 ping 更重要。

## 三、iPhone 配置

### 1. 安装官方 App

1. 在 iPhone 打开 App Store。
2. 搜索 **WireGuard**。
3. 确认开发者为 **WireGuard Development Team**，再安装。
4. 如果当前 Apple ID 地区无法取得官方 App，不要安装非官方 IPA 或企业签名版本。

当前官方 App 支持从二维码、文件或压缩包导入隧道。本文优先使用一次性二维码，
避免在手机“文件”应用或云盘中留下明文配置。

### 2. 在可信屏幕显示一次性二维码

在管理电脑进入本项目根目录并运行：

```bash
scripts/vpnctl peer export ios --qr
```

终端会显示二维码。它包含 iPhone 的私钥和 PSK：

- 不截图；
- 不录屏；
- 不通过远程会议共享终端；
- 不让监控摄像头或他人看到；
- 扫描成功后立即关闭该终端标签页或窗口，清除滚动缓冲。

二维码太大或变形时，先把终端窗口拉宽、缩小终端字体，再重新运行导出命令。
不要把二维码复制到在线二维码转换网站。

### 3. 扫码导入

1. 打开 WireGuard App。
2. 点击右上角 **“+”**。
3. 选择 **Create from QR code**（从二维码创建）。
4. 允许 WireGuard 使用相机。
5. 扫描管理电脑终端中的二维码。
6. 名称填写 `ios`。
7. iOS 提示“添加 VPN 配置”时，点击允许，并使用 Face ID、Touch ID 或锁屏密码确认。
8. 回到 WireGuard，打开 `ios` 右侧的开关。

首次连接后，配置中应看到：

```text
Address: 10.66.0.12/32, fd66:66:66::12/128
Endpoint: <新加坡服务器公网 IPv4>:51999
Allowed IPs: 0.0.0.0/0, ::/0
Persistent keepalive: 25
MTU: 1420
```

私钥只应在 WireGuard App 内保留，不要截图备份。项目管理机已经保存了可重新导出的
受保护副本。

### 4. 配置 Wi-Fi 和蜂窝网络自动连接

iOS 的具体文字可能随系统语言和 App 版本略有不同：

1. 在 WireGuard 中点击 `ios` 隧道进入编辑。
2. 找到 **On-Demand Activation** 或“按需启用”。
3. 同时启用 **Wi-Fi** 和 **Cellular**。
4. 若界面有 SSID 排除项，个人全隧道模式下先不要排除任何 Wi-Fi。
5. 保存后，分别在 Wi-Fi 和蜂窝数据下验证一次。

非受监管的个人 iPhone 上，On-Demand 不等同于企业 MDM 的强制
Always-On VPN：用户仍可以手工关闭或删除配置。因此必须实测锁屏、睡眠、唤醒和
网络切换，而不是只看状态栏的 VPN 标志。

### 5. iPhone 验收

按以下顺序测试：

1. 连接 Wi-Fi，开启隧道，用 Safari 检查公网 IPv4 和 IPv6；两者都应是新加坡
   服务器地址。
2. 运行 DNS 泄漏检测，不能出现当前 Wi-Fi 的 DNS。
3. 锁屏一分钟后解锁，再次打开网页。
4. 关闭 Wi-Fi，切到 4G/5G，确认 VPN 自动恢复，公网 IPv4、IPv6 仍为服务器地址。
5. 再从蜂窝切回 Wi-Fi，重复检查。
6. 开启、关闭飞行模式后，确认隧道恢复。

如果 iPhone 状态栏不显示 VPN 图标，以 WireGuard App 的最新握手、收发流量和
公网出口检查为准。

## 四、服务端确认两台设备已经上线

Windows 和 iPhone 分别连接后，在项目根目录运行：

```bash
scripts/vpnctl peer list
scripts/vpnctl server status
```

检查：

1. `windows` 和 `ios` 都是 `active`。
2. 两者都有最近的握手时间，不是 `-`。
3. `wg show` 中两者的接收、发送字节会随上网操作增加。
4. IPv4 forwarding 和 IPv6 forwarding 都是 `1`。
5. 项目 nftables 表仍然存在。

建议先让 Windows 与 iPhone 稳定运行 24 小时，再接入 macOS 和 Android。

## 五、常见问题

### 连接后没有握手

按顺序检查：

1. 云安全组是否对公网 IPv4 放行 `51999/udp`，协议必须是 UDP。
2. 客户端 `Endpoint` 是否为正确的新加坡服务器公网 IPv4 和端口。
3. 服务器是否在线，`scripts/vpnctl server status` 是否成功。
4. 是否误把 Windows 配置导入 iPhone，或让两台设备同时使用同一配置。
5. 客户端和服务器时间是否正确。
6. 当前公司、酒店或校园网络是否阻断 UDP；切换手机热点交叉测试。

不要通过反复 `peer add` 修复握手，这会制造密钥状态混乱。

### 有握手，但打不开网页

先运行：

```bash
scripts/vpnctl server status
```

重点检查 IPv4/IPv6 转发和 nftables 表。然后在客户端分别测试公网 IPv4、
公网 IPv6 和 DNS：

- IPv4、IPv6 都失败：优先检查转发、NAT 和服务器出站网络。
- IP 能访问但域名打不开：优先检查 DNS。
- IPv4 成功、IPv6 失败：检查服务器真实公网 IPv6、IPv6 默认路由、NAT66
  和云安全组的 IPv6 出站。

### 能打开小网页，但视频、上传或部分网站卡住

这通常是路径 MTU 问题。项目默认 `MTU = 1420`，先完整记录故障网络和现象，
再只在故障客户端将 MTU 改为 `1380` 重试。不要在没有证据时同时修改服务端、
所有客户端和防火墙。更多步骤见[恢复与排障](recovery.md)。

### Windows 开启 VPN 后完全断网

全隧道阻断行为已经生效，但服务端不可达或不能转发。先点击
**Deactivate** 恢复普通网络，再检查 UDP 端口、服务端状态和握手。不要删除配置，
否则会失去现场信息。

### iPhone 在 Wi-Fi 正常，蜂窝网络失败

检查：

1. iOS“设置”中 WireGuard 是否允许使用蜂窝数据。
2. On-Demand 是否同时启用了 Cellular。
3. 低数据模式、内容过滤器或其他 VPN/安全 App 是否产生冲突。
4. 运营商网络是否阻断 UDP；用另一张 SIM、热点或 Wi-Fi 交叉测试。

同一时间只启用一个 VPN。关闭其他 VPN、代理和网络过滤 App 后再测试。

### 二维码无法扫描

拉宽终端、缩小字体、提高屏幕亮度并清洁摄像头。仍失败时可以运行：

```bash
scripts/vpnctl peer export ios --file
```

再通过自己控制的加密或有线方式把 `exports/ios.conf` 送到 iPhone，在 WireGuard
中选择 **Create from file or archive** 导入。导入后立即删除手机“文件”应用及
“最近删除”中的临时文件。

## 六、日常安全和撤销

- 每台设备必须使用独立 peer，不能复制同一个配置到两台设备。
- 配置文件、二维码、私钥和 PSK 都不能进入截图、备忘录、聊天、工单或 Git。
- 手机或电脑丢失后，立即撤销对应 peer：

```bash
scripts/vpnctl peer revoke windows --yes
# 或
scripts/vpnctl peer revoke ios --yes
```

- 撤销会让该设备的旧配置立即失效。设备找回后应创建并导入新 peer，不要恢复旧密钥。
- 正常轮换密钥时按照[日常运维](operations.md)执行两阶段轮换，不要直接编辑密钥文件。
- 每月至少检查一次 Windows 和 iPhone 的 IPv4、IPv6、DNS 泄漏、网络切换及最近握手。

## 七、官方资料

- [WireGuard 官方安装页](https://www.wireguard.com/install/)
- [WireGuard 官方快速入门](https://www.wireguard.com/quickstart/)
- [WireGuard 官方 iPhone/iPad App Store 页面](https://apps.apple.com/app/wireguard/id1441195209)
- [Ubuntu Server：WireGuard VPN](https://documentation.ubuntu.com/server/how-to/wireguard-vpn/)
