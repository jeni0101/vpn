package asia.tnestai.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class ConfigDocumentParserTest {
    @Test fun acceptsEnrollmentFileWithBom() {
        val document = ConfigDocumentParser.parse(
            "\uFEFF  {\"version\":2,\"type\":\"tnest-vpn-enrollment\"}\n"
        )

        assertEquals(ConfigDocumentType.ENROLLMENT, document.type)
        assertEquals("{\"version\":2,\"type\":\"tnest-vpn-enrollment\"}", document.text)
    }

    @Test fun acceptsWireGuardQrPayload() {
        val document = ConfigDocumentParser.parse(
            """
                [Interface]
                PrivateKey = test

                [Peer]
                PublicKey = test
            """.trimIndent()
        )

        assertEquals(ConfigDocumentType.WIREGUARD, document.type)
    }

    @Test fun rejectsUnrelatedQrPayload() {
        assertThrows(IllegalArgumentException::class.java) {
            ConfigDocumentParser.parse("https://example.com/not-a-vpn-config")
        }
    }
}
