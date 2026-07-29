package asia.tnestai.vpn

enum class ConfigDocumentType {
    ENROLLMENT,
    WIREGUARD
}

data class ConfigDocument(
    val type: ConfigDocumentType,
    val text: String
)

object ConfigDocumentParser {
    private const val MAX_DOCUMENT_BYTES = 64 * 1024

    fun parse(source: String): ConfigDocument {
        val text = source.removePrefix("\uFEFF").trim()
        require(text.isNotEmpty()) { "配置内容为空" }
        require(text.toByteArray(Charsets.UTF_8).size <= MAX_DOCUMENT_BYTES) {
            "配置文件过大"
        }
        val type = when {
            text.startsWith("{") -> ConfigDocumentType.ENROLLMENT
            text.lineSequence().any { it.trim().equals("[Interface]", ignoreCase = true) } ->
                ConfigDocumentType.WIREGUARD
            else -> throw IllegalArgumentException(
                "不支持的二维码或文件，只能导入 .tnestvpn 或 WireGuard .conf"
            )
        }
        return ConfigDocument(type, text)
    }
}
