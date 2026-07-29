package asia.tnestai.vpn

import android.content.Context
import java.time.Instant
import java.time.ZoneId

class UsageLedger(context: Context) {
    data class Totals(
        val sessionDownload: Long,
        val sessionUpload: Long,
        val todayDownload: Long,
        val todayUpload: Long,
        val monthDownload: Long,
        val monthUpload: Long
    )

    private val prefs = context.getSharedPreferences("usage", Context.MODE_PRIVATE)
    private val zone = ZoneId.of("Asia/Shanghai")
    private var lastRx = -1L
    private var lastTx = -1L
    private var sessionRx = 0L
    private var sessionTx = 0L
    private var day = prefs.getString("day", "") ?: ""
    private var month = prefs.getString("month", "") ?: ""
    private var dayRx = prefs.getLong("day_rx", 0)
    private var dayTx = prefs.getLong("day_tx", 0)
    private var monthRx = prefs.getLong("month_rx", 0)
    private var monthTx = prefs.getLong("month_tx", 0)
    private var lastPersist = 0L

    fun update(rx: Long, tx: Long, nowMillis: Long = System.currentTimeMillis()): Totals {
        val local = Instant.ofEpochMilli(nowMillis).atZone(zone)
        val nextDay = local.toLocalDate().toString()
        val nextMonth = "${local.year}-${local.monthValue.toString().padStart(2, '0')}"
        if (day != nextDay) {
            day = nextDay
            dayRx = 0
            dayTx = 0
        }
        if (month != nextMonth) {
            month = nextMonth
            monthRx = 0
            monthTx = 0
        }
        if (lastRx >= 0) {
            val deltaRx = if (rx >= lastRx) rx - lastRx else rx
            val deltaTx = if (tx >= lastTx) tx - lastTx else tx
            sessionRx += deltaRx
            sessionTx += deltaTx
            dayRx += deltaRx
            dayTx += deltaTx
            monthRx += deltaRx
            monthTx += deltaTx
        }
        lastRx = rx
        lastTx = tx
        if (nowMillis - lastPersist >= 30_000) {
            prefs.edit()
                .putString("day", day).putString("month", month)
                .putLong("day_rx", dayRx).putLong("day_tx", dayTx)
                .putLong("month_rx", monthRx).putLong("month_tx", monthTx)
                .apply()
            lastPersist = nowMillis
        }
        return Totals(sessionRx, sessionTx, dayRx, dayTx, monthRx, monthTx)
    }
}
