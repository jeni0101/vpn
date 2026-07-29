package asia.tnestai.vpn

import android.util.Base64
import com.wireguard.crypto.Key
import com.wireguard.crypto.KeyPair
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.net.URLEncoder
import java.security.KeyFactory
import java.security.SecureRandom
import java.security.Signature
import java.security.spec.X509EncodedKeySpec
import java.time.Instant

class EnrollmentClient {
    fun enroll(
        document: String,
        migrationConfig: String? = null
    ): MultiRegionProfile {
        val invite = JSONObject(document)
        require(invite.keys().asSequence().toSet() == setOf(
            "version", "type", "management_url", "token", "expires_at",
            "device_name", "catalog_signing_key", "purpose"
        ))
        require(invite.getInt("version") == 2)
        require(invite.getString("type") == "tnest-vpn-enrollment")
        val origin = URI(invite.getString("management_url"))
        require(origin.scheme == "https" && origin.host == BuildConfig.TNEST_MANAGEMENT_HOST &&
                origin.rawPath.orEmpty() in listOf("", "/") && origin.rawQuery == null)
        require(Instant.parse(invite.getString("expires_at")).isAfter(Instant.now()))
        val token = invite.getString("token")
        require(token.length >= 43)
        val signingKey = invite.getString("catalog_signing_key")
        val purpose = invite.getString("purpose")
        require(purpose == "enroll" || purpose == "migrate")
        if (purpose == "migrate") {
            requireNotNull(migrationConfig) {
                "迁移现有设备需要本机原有的新加坡配置"
            }
        }

        val catalogEnvelope = requestJson(
            URL("${origin.scheme}://${origin.host}/api/v2/client/catalog"), "GET", null
        )
        val catalog = verifiedCatalog(catalogEnvelope, signingKey)
        require(Instant.parse(catalog.getString("expires_at")).isAfter(Instant.now()))
        val generated = mutableMapOf<String, Generated>()
        val credentials = JSONArray()
        val catalogRegions = catalog.getJSONArray("regions")
        for (index in 0 until catalogRegions.length()) {
            val region = catalogRegions.getJSONObject(index)
            if (region.getJSONArray("nodes").length() == 0) continue
            val generatedRegion =
                if (purpose == "migrate" && region.getString("code") == "SG") {
                    migrationMaterial(requireNotNull(migrationConfig))
                } else {
                    val pair = KeyPair()
                    val pskBytes = ByteArray(32).also(SecureRandom()::nextBytes)
                    Generated(
                        pair.privateKey.toBase64(), pair.publicKey.toBase64(),
                        Base64.encodeToString(pskBytes, Base64.NO_WRAP)
                    ).also { pskBytes.fill(0) }
                }
            generated[region.getString("code")] = generatedRegion
            credentials.put(JSONObject()
                .put("region_code", region.getString("code"))
                .put("public_key", generatedRegion.publicKey)
                .put("preshared_key", generatedRegion.presharedKey))
        }
        val claim = JSONObject().put("token", token).put("credentials", credentials)
        val response = requestJson(
            URL("${origin.scheme}://${origin.host}/api/v2/enrollments/claim"),
            "POST", claim
        )
        val signedResponseCatalog = verifiedCatalog(response.getJSONObject("catalog"), signingKey)
        val configurations = response.getJSONArray("regions")
        val byCode = (0 until configurations.length()).associate {
            val value = configurations.getJSONObject(it)
            value.getString("region_code") to value
        }
        val names = (0 until signedResponseCatalog.getJSONArray("regions").length()).associate {
            val value = signedResponseCatalog.getJSONArray("regions").getJSONObject(it)
            value.getString("code") to value.getString("display_name")
        }
        val regions = generated.map { (code, secret) ->
            val value = requireNotNull(byCode[code])
            val nodes = value.getJSONArray("nodes")
            RegionConfiguration(
                code, names[code] ?: code,
                value.getJSONArray("address").strings(),
                value.getJSONArray("dns").strings(), value.getInt("mtu"),
                value.getInt("config_version"),
                (0 until nodes.length()).map { CatalogNode.fromJson(nodes.getJSONObject(it)) },
                secret.privateKey, secret.presharedKey
            )
        }.sortedBy { it.code }
        val deviceToken = response.getString("device_token")
        require(deviceToken.length >= 32)
        return MultiRegionProfile(
            invite.getString("device_name"), origin.toString(), deviceToken, signingKey,
            signedResponseCatalog.getLong("catalog_version"),
            signedResponseCatalog.getString("expires_at"), regions
        )
    }

    fun sync(profile: MultiRegionProfile): MultiRegionProfile {
        val origin = URI(profile.managementUrl)
        val envelope = requestJson(
            URL("${origin.scheme}://${origin.host}/api/v2/client/catalog"), "GET", null
        )
        val catalog = verifiedCatalog(envelope, profile.catalogSigningKey)
        require(Instant.parse(catalog.getString("expires_at")).isAfter(Instant.now()))
        val current = requestJson(
            URL("${origin.scheme}://${origin.host}/api/v2/client/regions"),
            "GET", null, profile.deviceToken
        ).getJSONArray("regions")
        val currentByCode = (0 until current.length()).associate {
            val value = current.getJSONObject(it)
            value.getString("region_code") to value
        }
        val catalogRegions = catalog.getJSONArray("regions")
        val names = (0 until catalogRegions.length()).associate {
            val value = catalogRegions.getJSONObject(it)
            value.getString("code") to value.getString("display_name")
        }
        val result = profile.regions.map { existing ->
            currentByCode[existing.code]?.let { configuration ->
                existing.copy(
                    addresses = configuration.getJSONArray("address").strings(),
                    dns = configuration.getJSONArray("dns").strings(),
                    mtu = configuration.getInt("mtu"),
                    configVersion = configuration.getInt("config_version"),
                    nodes = configuration.getJSONArray("nodes").let { nodes ->
                        (0 until nodes.length()).map {
                            CatalogNode.fromJson(nodes.getJSONObject(it))
                        }
                    }
                )
            } ?: existing
        }.toMutableList()
        for (index in 0 until catalogRegions.length()) {
            val region = catalogRegions.getJSONObject(index)
            val code = region.getString("code")
            if (result.any { it.code.equals(code, ignoreCase = true) } ||
                region.getJSONArray("nodes").length() == 0) continue
            val pair = KeyPair()
            val pskBytes = ByteArray(32).also(SecureRandom()::nextBytes)
            val psk = Base64.encodeToString(pskBytes, Base64.NO_WRAP)
            pskBytes.fill(0)
            val body = JSONObject()
                .put("region_code", code)
                .put("public_key", pair.publicKey.toBase64())
                .put("preshared_key", psk)
            val configuration = requestJson(
                URL("${origin.scheme}://${origin.host}/api/v2/client/regions/$code/enroll"),
                "POST", body, profile.deviceToken
            ).getJSONObject("configuration")
            result += RegionConfiguration(
                code, names[code] ?: code,
                configuration.getJSONArray("address").strings(),
                configuration.getJSONArray("dns").strings(),
                configuration.getInt("mtu"), configuration.getInt("config_version"),
                configuration.getJSONArray("nodes").let { nodes ->
                    (0 until nodes.length()).map { CatalogNode.fromJson(nodes.getJSONObject(it)) }
                },
                pair.privateKey.toBase64(), psk
            )
        }
        return profile.copy(
            catalogVersion = catalog.getLong("catalog_version"),
            catalogExpiresAt = catalog.getString("expires_at"),
            regions = result
        )
    }

    fun usage(
        profile: MultiRegionProfile,
        regionCode: String,
        range: String
    ): UsageSummary {
        require(range in setOf("24h", "7d", "30d"))
        val origin = URI(profile.managementUrl)
        val region = URLEncoder.encode(regionCode, Charsets.UTF_8.name())
        val response = requestJson(
            URL("${origin.scheme}://${origin.host}/api/v2/client/usage" +
                "?range=$range&region=$region"),
            "GET", null, profile.deviceToken
        )
        val points = response.getJSONArray("points")
        var upload = 0L
        var download = 0L
        for (index in 0 until points.length()) {
            val point = points.getJSONObject(index)
            upload += point.getLong("upload_bytes")
            download += point.getLong("download_bytes")
        }
        return UsageSummary(
            upload, download, response.optString("synced_at")
        )
    }

    private fun requestJson(
        url: URL,
        method: String,
        body: JSONObject?,
        bearer: String? = null
    ): JSONObject {
        val connection = url.openConnection() as HttpURLConnection
        try {
            connection.requestMethod = method
            connection.connectTimeout = 10_000
            connection.readTimeout = 20_000
            connection.useCaches = false
            if (bearer != null) {
                connection.setRequestProperty("Authorization", "Bearer $bearer")
            }
            if (body != null) {
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json")
                connection.outputStream.use { it.write(body.toString().toByteArray()) }
            }
            require(connection.responseCode in 200..299) {
                "注册失败（HTTP ${connection.responseCode}）"
            }
            return connection.inputStream.bufferedReader().use { JSONObject(it.readText()) }
        } finally {
            connection.disconnect()
        }
    }

    private fun verifiedCatalog(envelope: JSONObject, encodedKey: String): JSONObject {
        val payload = Base64.decode(envelope.getString("signed_payload"), Base64.NO_WRAP)
        val signature = Base64.decode(envelope.getString("signature"), Base64.NO_WRAP)
        val rawKey = Base64.decode(encodedKey, Base64.NO_WRAP)
        val x509Prefix = byteArrayOf(
            0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00
        )
        val publicKey = KeyFactory.getInstance("Ed25519").generatePublic(
            X509EncodedKeySpec(x509Prefix + rawKey)
        )
        val valid = Signature.getInstance("Ed25519").run {
            initVerify(publicKey)
            update(payload)
            verify(signature)
        }
        rawKey.fill(0)
        signature.fill(0)
        require(valid) { "地区清单签名无效" }
        return JSONObject(payload.toString(Charsets.UTF_8)).also {
            payload.fill(0)
            require(it.optString("signature").isEmpty())
            require(it.optString("signed_payload").isEmpty())
        }
    }

    private fun migrationMaterial(text: String): Generated {
        SafeConfig.parse(text)
        var section = ""
        var privateKey = ""
        var presharedKey = ""
        text.lineSequence().forEach { source ->
            val line = source.substringBefore('#').substringBefore(';').trim()
            if (line.startsWith("[") && line.endsWith("]")) {
                section = line.lowercase()
            } else if (line.contains('=')) {
                val name = line.substringBefore('=').trim().lowercase()
                val value = line.substringAfter('=').trim()
                if (section == "[interface]" && name == "privatekey") {
                    privateKey = value
                }
                if (section == "[peer]" && name == "presharedkey") {
                    presharedKey = value
                }
            }
        }
        require(privateKey.isNotEmpty() && presharedKey.isNotEmpty())
        val pair = KeyPair(Key.fromBase64(privateKey))
        return Generated(
            privateKey, pair.publicKey.toBase64(), presharedKey
        )
    }

    private data class Generated(
        val privateKey: String,
        val publicKey: String,
        val presharedKey: String
    )
}

data class UsageSummary(
    val uploadBytes: Long,
    val downloadBytes: Long,
    val syncedAt: String
)

private fun JSONArray.strings() = (0 until length()).map(::getString)
