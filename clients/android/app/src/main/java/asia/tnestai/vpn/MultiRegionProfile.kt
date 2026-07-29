package asia.tnestai.vpn

import org.json.JSONArray
import org.json.JSONObject

data class CatalogNode(
    val id: String,
    val endpoint: String,
    val probeUrl: String,
    val serverPublicKey: String,
    val priority: Int
) {
    fun toJson() = JSONObject()
        .put("id", id).put("endpoint", endpoint).put("probe_url", probeUrl)
        .put("server_public_key", serverPublicKey).put("priority", priority)

    companion object {
        fun fromJson(value: JSONObject) = CatalogNode(
            value.getString("id"), value.getString("endpoint"),
            value.getString("probe_url"), value.getString("server_public_key"),
            value.getInt("priority")
        )
    }
}

data class RegionConfiguration(
    val code: String,
    val displayName: String,
    val addresses: List<String>,
    val dns: List<String>,
    val mtu: Int,
    val configVersion: Int,
    val nodes: List<CatalogNode>,
    val privateKey: String,
    val presharedKey: String
) {
    fun render(node: CatalogNode): String {
        require(nodes.any { it.id == node.id })
        return """
            [Interface]
            PrivateKey = $privateKey
            Address = ${addresses.joinToString(", ")}
            DNS = ${dns.joinToString(", ")}
            MTU = $mtu

            [Peer]
            PublicKey = ${node.serverPublicKey}
            PresharedKey = $presharedKey
            Endpoint = ${node.endpoint}
            AllowedIPs = 0.0.0.0/0, ::/0
            PersistentKeepalive = 25
        """.trimIndent() + "\n"
    }

    fun toJson() = JSONObject()
        .put("code", code).put("display_name", displayName)
        .put("address", JSONArray(addresses)).put("dns", JSONArray(dns))
        .put("mtu", mtu).put("config_version", configVersion)
        .put("nodes", JSONArray(nodes.map(CatalogNode::toJson)))
        .put("private_key", privateKey).put("preshared_key", presharedKey)

    companion object {
        fun fromJson(value: JSONObject) = RegionConfiguration(
            value.getString("code"), value.getString("display_name"),
            value.getJSONArray("address").strings(),
            value.getJSONArray("dns").strings(), value.getInt("mtu"),
            value.getInt("config_version"),
            value.getJSONArray("nodes").objects().map(CatalogNode::fromJson),
            value.getString("private_key"), value.getString("preshared_key")
        )
    }
}

data class MultiRegionProfile(
    val deviceName: String,
    val managementUrl: String,
    val deviceToken: String,
    val catalogSigningKey: String,
    val catalogVersion: Long,
    val catalogExpiresAt: String,
    val regions: List<RegionConfiguration>
) {
    fun region(code: String) = regions.first { it.code.equals(code, ignoreCase = true) }

    fun toJson() = JSONObject()
        .put("version", 2).put("device_name", deviceName)
        .put("management_url", managementUrl).put("device_token", deviceToken)
        .put("catalog_signing_key", catalogSigningKey)
        .put("catalog_version", catalogVersion).put("catalog_expires_at", catalogExpiresAt)
        .put("regions", JSONArray(regions.map(RegionConfiguration::toJson))).toString()

    companion object {
        fun fromJson(text: String): MultiRegionProfile {
            val value = JSONObject(text)
            require(value.getInt("version") == 2)
            return MultiRegionProfile(
                value.getString("device_name"), value.getString("management_url"),
                value.getString("device_token"), value.getString("catalog_signing_key"),
                value.getLong("catalog_version"), value.getString("catalog_expires_at"),
                value.getJSONArray("regions").objects().map(RegionConfiguration::fromJson)
            )
        }
    }
}

private fun JSONArray.strings() = (0 until length()).map(::getString)
private fun JSONArray.objects() = (0 until length()).map(::getJSONObject)
