# 四端接入与验收

客户端只从 [WireGuard 官方安装页](https://www.wireguard.com/install/)指向的渠道获取。

`server bootstrap` 已经创建全部四个 peer 并把文件配置写入 `exports/`，不要再次运行 `peer add`。需要重新导出文件或临时二维码时使用下面的命令。

## Windows

```bash
scripts/vpnctl peer export windows --file
```

把 `exports/windows.conf` 安全传到 Windows 并导入。配置中的 IPv4 和 IPv6 `/0` 路由会启用官方客户端的全隧道阻断行为。导入后删除传输过程中的临时副本。

## macOS

```bash
scripts/vpnctl peer export macos --file
```

从 WireGuard App 导入，在应用中配置 On-Demand。非受监管 Apple 设备不等同于 MDM Always-On VPN，因此必须实测睡眠、唤醒和 Wi-Fi 切换。

## iOS

```bash
scripts/vpnctl peer export ios --qr
```

使用 WireGuard App 扫描终端二维码，配置同时覆盖 Wi-Fi 和蜂窝网络的 On-Demand。二维码包含私钥和 PSK，扫描后立即关闭终端内容。

## Android

```bash
scripts/vpnctl peer export android --qr
```

导入后，在 Android 系统 VPN 设置中启用“始终开启 VPN”和“阻止无 VPN 的连接”。

## 每台设备的验收

连接后检查：

1. 公网 IPv4 等于新加坡服务器 IPv4。
2. 公网 IPv6 等于新加坡服务器 IPv6。
3. DNS 检测只看到配置的公共解析器，不出现当前 Wi-Fi 或运营商 DNS。
4. 无法访问其他 peer 的 `10.66.0.x` 和 `fd66:66:66::x`。
5. 视频、下载、上传和大网页正常，未出现 MTU 症状。
6. 睡眠和唤醒后恢复。
7. Wi-Fi、热点、4G/5G 切换后恢复。
8. 断开 VPN 时符合平台的阻断或自动重连预期。

四端全部上线后，同时运行并检查：

```bash
scripts/vpnctl peer list
scripts/vpnctl server status
```

Windows 与 iOS 先稳定运行24小时，再加入 macOS 和 Android。
