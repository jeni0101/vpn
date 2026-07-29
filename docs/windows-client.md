# TNest VPN Windows 客户端

客户端位于 `clients/windows/`，面向 Windows 10/11 x64，使用 .NET 8 WPF。UI 以普通用户运行；只有安装/更新隧道服务时触发 UAC。隧道服务调用 WireGuard 官方 `tunnel.dll`，并依赖 WireGuardNT 的 `wireguard.dll`。

## 构建

在 Windows 11、Visual Studio Build Tools 2022、.NET 8 SDK、Go 和 WiX v4 环境运行：

```powershell
clients\windows\build.ps1
```

脚本把 WireGuard Windows 固定到提交
`4e6726c23ae9c5cb58e0c9910f3b7515621d133d`，构建 x64 `tunnel.dll`，再生成自包含应用和未签名 MSI。GitHub Actions 只生成未签名产物。

WiX 中每个版本化 EXE/DLL 使用独立组件，以便稳定生成组件 GUID。构建后的
`SHA256SUMS.txt` 递归列出 `publish/` 下的文件，不会尝试对语言资源目录执行
`Get-FileHash`。

正式侧载必须在离线发布机上：

1. 校验 Git 提交和 `SHA256SUMS.txt`。
2. 使用自用 Windows 代码签名证书签名 EXE、DLL、MSI。
3. 生成依赖/SBOM 和 SHA-256。
4. 使用离线 Ed25519 发布密钥签名版本清单。

证书和发布私钥不得进入仓库、GitHub Actions 或服务器。

## 使用

1. 安装已签名/自行确认来源的 MSI。
2. 从 Web 下载一次性 `.tnestvpn`，或导入标准 `.conf`。
3. 把文件拖入窗口或点击“导入配置”。
4. `.tnestvpn` 会在 Windows 本机生成 Curve25519 私钥和 PSK，再经 HTTPS 注册。
5. 点击“连接”，首次安装隧道服务时确认 UAC。

客户端只接受 `PrivateKey`、`Address`、`DNS`、`MTU`、`PublicKey`、`PresharedKey`、`Endpoint`、`AllowedIPs`、`PersistentKeepalive`。出现 `PreUp/PostUp`、脚本或多 peer 会拒绝导入。

本地保存使用 DPAPI LocalMachine；隧道服务需要的临时明文配置只允许 SYSTEM、管理员和该服务 SID 读取。全局 `0.0.0.0/0, ::/0` 触发官方 WireGuard Windows 的阻断语义。

## 验收

连接后在 PowerShell 运行：

```powershell
curl.exe -4 https://icanhazip.com
curl.exe -6 https://icanhazip.com
Resolve-DnsName example.com
```

IPv4/IPv6 应分别显示新加坡服务器出口。断开服务器或屏蔽 UDP 51999 后，客户端不得静默回到本地直连。
