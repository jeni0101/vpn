package asia.tnestai.vpn

import org.junit.Assert.assertThrows
import org.junit.Test

class SafeConfigTest {
    private val key = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
    private val valid = """
        [Interface]
        PrivateKey = $key
        Address = 10.66.0.14/32, fd66:66:66::14/128
        DNS = 1.1.1.1
        MTU = 1420

        [Peer]
        PublicKey = $key
        PresharedKey = $key
        Endpoint = 203.0.113.10:51999
        AllowedIPs = 0.0.0.0/0, ::/0
        PersistentKeepalive = 25
    """.trimIndent()

    @Test fun acceptsSafeDualStackConfig() {
        SafeConfig.parse(valid)
    }

    @Test fun rejectsCommandFields() {
        assertThrows(IllegalArgumentException::class.java) {
            SafeConfig.parse(valid.replace("MTU = 1420", "MTU = 1420\nPostUp = calc.exe"))
        }
    }

    @Test fun rejectsIPv4OnlyRoute() {
        assertThrows(IllegalArgumentException::class.java) {
            SafeConfig.parse(valid.replace("0.0.0.0/0, ::/0", "0.0.0.0/0"))
        }
    }
}
