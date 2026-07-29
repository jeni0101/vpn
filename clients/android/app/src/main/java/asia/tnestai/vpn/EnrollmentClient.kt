package asia.tnestai.vpn

import com.wireguard.crypto.Key
import com.wireguard.crypto.KeyPair
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.security.SecureRandom
import java.time.Instant

class EnrollmentClient {
    data class Result(val config: String, val deviceName: String)

    fun enroll(document: String): Result {
        val invite = JSONObject(document)
        require(invite.length() in 6..7)
        require(invite.getInt("version") == 1)
        require(invite.getString("type") == "tnest-vpn-enrollment")
        val origin = URI(invite.getString("management_url"))
        require(origin.scheme == "https" && origin.host == "vpn.example.com" &&
                origin.rawPath.orEmpty() in listOf("", "/") && origin.rawQuery == null)
        require(Instant.parse(invite.getString("expires_at")).isAfter(Instant.now()))
        val token = invite.getString("token")
        require(token.length >= 43)
        val pair = KeyPair()
        val pskBytes = ByteArray(32).also(SecureRandom()::nextBytes)
        val psk = Key.fromBytes(pskBytes)
        pskBytes.fill(0)

        val request = JSONObject()
            .put("token", token)
            .put("public_key", pair.publicKey.toBase64())
            .put("preshared_key", psk.toBase64())
        val connection = URL("${origin.scheme}://${origin.host}/api/v1/enrollments/claim")
            .openConnection() as HttpURLConnection
        connection.requestMethod = "POST"
        connection.connectTimeout = 10_000
        connection.readTimeout = 20_000
        connection.doOutput = true
        connection.setRequestProperty("Content-Type", "application/json")
        connection.outputStream.use { it.write(request.toString().toByteArray()) }
        require(connection.responseCode == 200) { "注册失败（HTTP ${connection.responseCode}）" }
        val response = connection.inputStream.bufferedReader().use { JSONObject(it.readText()) }
        val device = response.getJSONObject("device")
        val config = response.getJSONObject("configuration")
        val peer = config.getJSONObject("peer")
        val addresses = config.getJSONArray("address").let { "${it.getString(0)}, ${it.getString(1)}" }
        val dns = config.getJSONArray("dns").let { array ->
            (0 until array.length()).joinToString(", ") { array.getString(it) }
        }
        val rendered = """
            [Interface]
            PrivateKey = ${pair.privateKey.toBase64()}
            Address = $addresses
            DNS = $dns
            MTU = ${config.getInt("mtu")}

            [Peer]
            PublicKey = ${peer.getString("server_public_key")}
            PresharedKey = ${psk.toBase64()}
            Endpoint = ${peer.getString("endpoint")}
            AllowedIPs = 0.0.0.0/0, ::/0
            PersistentKeepalive = 25
        """.trimIndent() + "\n"
        SafeConfig.parse(rendered)
        return Result(rendered, device.getString("name"))
    }
}
