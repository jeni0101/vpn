# 离线签名发布

服务器和 GitHub Actions 只生成未签名产物。首次发布在断网或专用离线机生成 Ed25519 密钥：

```bash
personal-vpn-release-tool keygen \
  --private release-ed25519.private \
  --public release-ed25519.public
```

私钥移入离线介质，禁止放在服务器、GitHub 或普通 VPN 备份。把公钥编译进客户端。

准备 `payload.json`，每个平台至少包含版本、下载 URL、包名/产品 ID、SHA-256、最低系统版本和是否允许降级（必须为 false）。离线签名：

```bash
personal-vpn-release-tool sign \
  --private release-ed25519.private \
  --payload payload.json \
  --out releases.json
personal-vpn-release-tool verify \
  --public release-ed25519.public \
  --file releases.json
```

核对后把 `releases.json` 安装到服务器
`/var/lib/personal-vpn-web/releases.json`。`/api/v1/releases` 只负责原样提供签名信封；没有签名清单时返回空发布列表，不提供不可信更新。

Windows MSI 还必须使用 Windows 代码签名证书签名；Android APK 必须使用固定 Android keystore 签名。客户端更新逻辑需要同时核对 Ed25519 签名、SHA-256、产品标识、包名和版本单调递增，安装始终需要用户确认。
