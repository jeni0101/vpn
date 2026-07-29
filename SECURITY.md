# Security policy

这是个人自用项目，不接受公开提交运行时密钥或配置。发现问题时请只提供复现步骤、受影响版本和已脱敏日志。

绝不能提交：

- WireGuard 私钥、PSK、客户端 `.conf`、二维码；
- `config/local.env`、SQLite 运行库、age identity；
- Windows 代码签名证书、Android keystore；
- Ed25519 发布私钥、TOTP secret、恢复码或管理员密码。

仓库公开时按秘密已经泄露处理：立即撤销/轮换对应 peer 或签名材料，不只依赖删除 Git 提交。
