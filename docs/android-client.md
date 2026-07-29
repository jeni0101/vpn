# TNest VPN Android 客户端

客户端位于 `clients/android/`，包名 `asia.tnestai.vpn`，minSdk 26、target/compile SDK 36。隧道使用 Maven Central 当前已发布的官方 `com.wireguard.android:tunnel:1.0.20260102` 与 `GoBackend`。

## 构建

准备 JDK 17、Android SDK 36 和 Gradle 8.13：

```bash
gradle -p clients/android \
  :app:lintDebug \
  :app:testDebugUnitTest \
  :app:assembleDebug
```

生成 release APK 时，Gradle 工程不内置签名配置。必须在离线发布机用自用 Android keystore 签名，随后执行 `apksigner verify --verbose --print-certs`。keystore、口令和 Ed25519 发布私钥不得上传。

手动运行 GitHub `unsigned-release` 工作流会生成两个不同产物：

- `tnest-vpn-android-debug-installable`：已用 CI Debug 证书签名，可直接安装，仅供测试。
- `tnest-vpn-android-release-unsigned`：未签名 Release APK，只能交给离线发布机签名，不能直接安装。

Android 安装未签名 Release APK 时可能显示错误码 33 或
`packageInfo is null`。这不是应用包名错误，而是安装包没有有效签名。Debug
构建与正式 Release 证书不同，而且不同 CI 运行生成的 Debug 证书也可能不同。覆盖安装
失败时应先卸载旧测试版；改装正式版前也应先卸载 Debug 版，除非两者使用同一固定
keystore。

## 使用

1. 安装自己核对并签名的 APK。
2. 选择 `.conf`/`.tnestvpn`，或扫描 Web 显示的一次性二维码。
3. 首次连接时接受 Android `VpnService` 系统确认。
4. 进入系统 VPN 设置，对 TNest VPN 启用“始终开启 VPN”和“无 VPN 时阻止连接”。

`.tnestvpn` 的私钥和 PSK 在手机本地生成。完整配置使用 Android Keystore AES-GCM 加密后保存，应用禁用系统备份和明文网络。

应用每 5 秒通过 `Backend.getStatistics()` 读取当前隧道计数。显示方向以设备为准：RX 为下载，TX 为上传。

## 验收

- Wi-Fi 与蜂窝切换后自动恢复。
- 重启、锁屏、唤醒后恢复。
- IPv4/IPv6 出口均为新加坡服务器。
- 关闭服务器或 UDP 51999 后，Lockdown 阻止直连。
- Web 与手机 24 小时累计差异不超过 2%。
