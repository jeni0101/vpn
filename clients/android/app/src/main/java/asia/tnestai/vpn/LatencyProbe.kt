package asia.tnestai.vpn

import java.net.HttpURLConnection
import java.net.URL
import kotlin.system.measureNanoTime

object LatencyProbe {
    data class Result(val node: CatalogNode, val medianMs: Long?)

    fun probe(nodes: List<CatalogNode>): List<Result> {
        val deadline = System.nanoTime() + 5_000_000_000L
        return nodes.sortedBy { it.priority }.map { node ->
            val samples = mutableListOf<Long>()
            repeat(3) {
                if (System.nanoTime() >= deadline) return@repeat
                var status = 0
                val elapsed = measureNanoTime {
                    val connection = URL(node.probeUrl).openConnection() as HttpURLConnection
                    try {
                        connection.requestMethod = "HEAD"
                        connection.connectTimeout = 1_500
                        connection.readTimeout = 1_500
                        connection.useCaches = false
                        status = connection.responseCode
                    } finally {
                        connection.disconnect()
                    }
                } / 1_000_000
                if (status in 200..299) samples += elapsed
            }
            Result(node, samples.sorted().let { if (it.isEmpty()) null else it[it.size / 2] })
        }
    }

    fun select(results: List<Result>): CatalogNode =
        results.filter { it.medianMs != null }
            .sortedWith(compareBy<Result> { it.node.priority }.thenBy { it.medianMs })
            .firstOrNull()?.node
            ?: error("所选地区没有可用节点")
}
