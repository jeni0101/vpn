package asia.tnestai.vpn

import com.wireguard.config.Config
import java.io.ByteArrayInputStream

object SafeConfig {
    private val interfaceFields = setOf("privatekey", "address", "dns", "mtu")
    private val peerFields = setOf(
        "publickey", "presharedkey", "endpoint", "allowedips", "persistentkeepalive"
    )

    fun parse(text: String): Config {
        require(text.toByteArray().size <= 64 * 1024) { "配置文件过大" }
        var section = ""
        var peers = 0
        var allowedRoutes: Set<String>? = null
        text.lineSequence().forEach { source ->
            val line = source.substringBefore('#').substringBefore(';').trim()
            if (line.isEmpty()) return@forEach
            if (line.startsWith("[") && line.endsWith("]")) {
                section = line.lowercase()
                require(section == "[interface]" || section == "[peer]") { "配置包含未知区域" }
                if (section == "[peer]") peers++
                return@forEach
            }
            val key = line.substringBefore('=', "").trim().lowercase()
            val value = line.substringAfter('=', "").trim()
            require(key.isNotEmpty()) { "配置行格式错误" }
            val allowed = if (section == "[interface]") interfaceFields else peerFields
            require(key in allowed) { "拒绝危险或不支持的字段：$key" }
            if (section == "[peer]" && key == "allowedips") {
                allowedRoutes = value.split(',').map(String::trim).toSet()
            }
        }
        require(peers == 1) { "TNest VPN 只接受一个 Peer" }
        require(allowedRoutes == setOf("0.0.0.0/0", "::/0")) { "必须启用 IPv4/IPv6 全局路由" }
        val config = Config.parse(ByteArrayInputStream(text.toByteArray(Charsets.UTF_8)))
        return config
    }
}
