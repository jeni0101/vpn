package asia.tnestai.vpn

import android.content.Context
import com.wireguard.android.backend.GoBackend
import com.wireguard.android.backend.Tunnel
import com.wireguard.config.Config
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

class TunnelController(context: Context) : Tunnel {
    data class Usage(val download: Long = 0, val upload: Long = 0, val handshake: Long = 0)
    private val backend = GoBackend(context.applicationContext)
    @Volatile var state: Tunnel.State = Tunnel.State.DOWN
        private set
    private var config: Config? = null

    override fun getName() = "tnest"
    override fun onStateChange(newState: Tunnel.State) { state = newState }
    fun load(value: Config) { config = value }

    suspend fun setConnected(up: Boolean) = withContext(Dispatchers.IO) {
        state = backend.setState(this@TunnelController,
            if (up) Tunnel.State.UP else Tunnel.State.DOWN,
            if (up) requireNotNull(config) else null)
        state
    }

    suspend fun statistics(): Usage = withContext(Dispatchers.IO) {
        val stats = backend.getStatistics(this@TunnelController)
        var handshake = 0L
        stats.peers().forEach { key ->
            handshake = maxOf(handshake, stats.peer(key)?.latestHandshakeEpochMillis() ?: 0)
        }
        Usage(stats.totalRx(), stats.totalTx(), handshake)
    }
}
