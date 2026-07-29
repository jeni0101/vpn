package asia.tnestai.vpn

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.net.VpnService
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import com.wireguard.android.backend.Tunnel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {
    private lateinit var controller: TunnelController
    private lateinit var store: SecureConfigStore
    private lateinit var ledger: UsageLedger
    private var profile: MultiRegionProfile? = null
    private var pendingDocumentUri by mutableStateOf<Uri?>(null)
    private val selectedNodes = mutableMapOf<String, CatalogNode>()
    private data class UsageRanges(
        val day: UsageSummary,
        val week: UsageSummary,
        val month: UsageSummary
    )

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        controller = TunnelController(this)
        store = SecureConfigStore(this)
        ledger = UsageLedger(this)
        pendingDocumentUri = intent
            .takeIf { it.action == Intent.ACTION_VIEW }
            ?.data
        profile = runCatching { store.loadProfile() }.getOrNull()
        if (profile != null && profile!!.regions.isNotEmpty()) {
            activateRegion(profile!!.regions.first().code)
        } else {
            store.load()?.let { controller.load(SafeConfig.parse(it)) }
        }
        setContent { App() }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        pendingDocumentUri = intent
            .takeIf { it.action == Intent.ACTION_VIEW }
            ?.data
    }

    private fun activateRegion(code: String): CatalogNode {
        val region = requireNotNull(profile).region(code)
        val node = selectedNodes[code] ?: region.nodes.minBy { it.priority }
        selectedNodes[code] = node
        val configText = region.render(node)
        store.save(configText)
        controller.load(SafeConfig.parse(configText))
        return node
    }

    @androidx.compose.runtime.Composable
    private fun App() {
        var status by remember { mutableStateOf(controller.state) }
        var message by remember { mutableStateOf("请选择 .conf 或 .tnestvpn 配置") }
        var usage by remember { mutableStateOf(TunnelController.Usage()) }
        var totals by remember { mutableStateOf(UsageLedger.Totals(0, 0, 0, 0, 0, 0)) }
        var historical by remember { mutableStateOf<UsageRanges?>(null) }
        var historyAvailable by remember { mutableStateOf(true) }
        var activeProfile by remember { mutableStateOf(profile) }
        var selectedRegion by remember {
            mutableStateOf(profile?.regions?.firstOrNull()?.code.orEmpty())
        }
        var probing by remember { mutableStateOf(false) }
        val scope = androidx.compose.runtime.rememberCoroutineScope()
        val vpnPermission = rememberLauncherForActivityResult(
            ActivityResultContracts.StartActivityForResult()
        ) { result ->
            if (result.resultCode == Activity.RESULT_OK) scope.launch {
                runCatching { controller.setConnected(true) }
                    .onSuccess { status = it; message = "已连接 $selectedRegion" }
                    .onFailure { message = it.message ?: "连接失败" }
            }
        }

        suspend fun importEnrollment(text: String) {
            val value = withContext(Dispatchers.IO) {
                EnrollmentClient().enroll(text, store.load())
            }
            store.saveProfile(value)
            profile = value
            activeProfile = value
            selectedRegion = value.regions.first().code
            val node = activateRegion(selectedRegion)
            message = "配置已安全保存 · ${node.endpoint}"
        }

        suspend fun importDocument(source: String) {
            val document = ConfigDocumentParser.parse(source)
            when (document.type) {
                ConfigDocumentType.ENROLLMENT -> importEnrollment(document.text)
                ConfigDocumentType.WIREGUARD -> {
                    val config = SafeConfig.parse(document.text)
                    profile = null
                    activeProfile = null
                    selectedRegion = ""
                    selectedNodes.clear()
                    store.clearProfile()
                    store.save(document.text)
                    controller.load(config)
                    message = "WireGuard 配置已安全保存"
                }
            }
        }

        suspend fun importUri(uri: Uri) {
            val text = withContext(Dispatchers.IO) {
                contentResolver.openInputStream(uri)?.bufferedReader(Charsets.UTF_8)
                    ?.use { it.readText() }
                    ?: throw IllegalArgumentException("无法读取所选配置文件")
            }
            importDocument(text)
        }

        val picker = rememberLauncherForActivityResult(
            ActivityResultContracts.OpenDocument()
        ) { uri: Uri? ->
            if (uri != null) scope.launch {
                runCatching { importUri(uri) }
                    .onFailure { message = it.message ?: "导入失败" }
            }
        }
        val scanner = rememberLauncherForActivityResult(ScanContract()) { result ->
            if (!result.contents.isNullOrBlank()) scope.launch {
                runCatching { importDocument(result.contents) }
                    .onFailure { message = it.message ?: "二维码无效" }
            }
        }
        LaunchedEffect(pendingDocumentUri) {
            val uri = pendingDocumentUri ?: return@LaunchedEffect
            runCatching { importUri(uri) }
                .onFailure { message = it.message ?: "导入失败" }
            pendingDocumentUri = null
        }
        LaunchedEffect(activeProfile?.deviceToken) {
            val cached = activeProfile
            if (cached != null) {
                runCatching {
                    val synced = withContext(Dispatchers.IO) {
                        EnrollmentClient().sync(cached)
                    }
                    store.saveProfile(synced)
                    profile = synced
                    activeProfile = synced
                    if (synced.regions.none { it.code == selectedRegion }) {
                        selectedRegion = synced.regions.first().code
                    }
                    activateRegion(selectedRegion)
                    message = "地区和节点清单已同步"
                }.onFailure {
                    message = "使用已验签缓存 · 地区同步暂不可用"
                }
            }
        }
        LaunchedEffect(status) {
            while (status == Tunnel.State.UP) {
                runCatching { controller.statistics() }.onSuccess {
                    usage = it
                    totals = ledger.update(it.download, it.upload)
                }
                delay(5_000)
            }
        }
        LaunchedEffect(activeProfile, selectedRegion) {
            val current = activeProfile
            if (current != null && selectedRegion.isNotBlank()) {
                historyAvailable = true
                historical = runCatching {
                    withContext(Dispatchers.IO) {
                        val client = EnrollmentClient()
                        UsageRanges(
                            client.usage(current, selectedRegion, "24h"),
                            client.usage(current, selectedRegion, "7d"),
                            client.usage(current, selectedRegion, "30d")
                        )
                    }
                }.onFailure {
                    historyAvailable = false
                }.getOrNull()
            }
        }
        MaterialTheme {
            Column(
                modifier = Modifier.fillMaxSize().padding(24.dp),
                verticalArrangement = Arrangement.spacedBy(18.dp)
            ) {
                Text("TNest VPN", style = MaterialTheme.typography.headlineLarge)
                Text(message)
                activeProfile?.let { value ->
                    Text("选择地区")
                    Row(
                        Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        value.regions.forEach { region ->
                            Button(
                                enabled = status != Tunnel.State.UP,
                                onClick = {
                                    selectedRegion = region.code
                                    val node = activateRegion(region.code)
                                    message = "${region.displayName} · ${node.endpoint}"
                                }
                            ) { Text(region.displayName) }
                        }
                    }
                    Button(
                        enabled = status != Tunnel.State.UP && !probing &&
                            selectedRegion.isNotBlank(),
                        onClick = {
                            scope.launch {
                                probing = true
                                message = "正在执行三次轻量 HTTPS 延迟探测…"
                                runCatching {
                                    val region = requireNotNull(profile).region(selectedRegion)
                                    val results = withContext(Dispatchers.IO) {
                                        LatencyProbe.probe(region.nodes)
                                    }
                                    val node = LatencyProbe.select(results)
                                    selectedNodes[selectedRegion] = node
                                    activateRegion(selectedRegion)
                                    val ms = results.first { it.node.id == node.id }.medianMs
                                    message = "${region.displayName} · ${ms} ms"
                                }.onFailure { message = it.message ?: "延迟测试失败" }
                                probing = false
                            }
                        }
                    ) { Text(if (probing) "测试中…" else "测试延迟") }
                }
                Card(Modifier.fillMaxWidth()) {
                    Column(
                        Modifier.padding(18.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp)
                    ) {
                        Row(
                            Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween
                        ) {
                            Text(if (status == Tunnel.State.UP) "已连接" else "未连接")
                            Switch(
                                checked = status == Tunnel.State.UP,
                                onCheckedChange = { up ->
                                    if (up) {
                                        val intent = VpnService.prepare(this@MainActivity)
                                        if (intent != null) vpnPermission.launch(intent)
                                        else scope.launch {
                                            runCatching { controller.setConnected(true) }
                                                .onSuccess {
                                                    status = it
                                                    message = "已连接 $selectedRegion"
                                                }
                                                .onFailure {
                                                    message = it.message ?: "连接失败"
                                                }
                                        }
                                    } else scope.launch {
                                        runCatching { controller.setConnected(false) }
                                            .onSuccess {
                                                status = it
                                                message = "已断开，可切换地区或测试延迟"
                                            }
                                    }
                                }
                            )
                        }
                        Text("当前　↓ ${formatBytes(totals.sessionDownload)}　↑ ${formatBytes(totals.sessionUpload)}")
                        Text("今日　↓ ${formatBytes(totals.todayDownload)}　↑ ${formatBytes(totals.todayUpload)}")
                        Text("本月　↓ ${formatBytes(totals.monthDownload)}　↑ ${formatBytes(totals.monthUpload)}")
                        historical?.let {
                            Text("服务器 24 小时　↓ ${formatBytes(it.day.downloadBytes)}　↑ ${formatBytes(it.day.uploadBytes)}")
                            Text("服务器 7 天　↓ ${formatBytes(it.week.downloadBytes)}　↑ ${formatBytes(it.week.uploadBytes)}")
                            Text("服务器 30 天　↓ ${formatBytes(it.month.downloadBytes)}　↑ ${formatBytes(it.month.uploadBytes)}")
                        }
                        if (!historyAvailable) {
                            Text("服务器历史暂不可用；当前会话和本机累计仍可查看")
                        }
                        if (usage.handshake > 0) {
                            Text("最近握手　${java.time.Instant.ofEpochMilli(usage.handshake)}")
                        }
                    }
                }
                Button(onClick = {
                    picker.launch(arrayOf("*/*"))
                }) { Text("导入配置") }
                Button(onClick = {
                    scanner.launch(
                        ScanOptions().setPrompt("扫描 TNest VPN 或 WireGuard 配置二维码")
                            .setBeepEnabled(false).setOrientationLocked(false)
                    )
                }) { Text("扫描二维码") }
                Button(onClick = {
                    startActivity(Intent(Settings.ACTION_VPN_SETTINGS))
                }) { Text("设置始终开启与无 VPN 阻断") }
            }
        }
    }

    private fun formatBytes(value: Long): String =
        when {
            value >= 1L shl 30 -> "%.2f GB".format(value.toDouble() / (1L shl 30))
            value >= 1L shl 20 -> "%.2f MB".format(value.toDouble() / (1L shl 20))
            else -> "%.1f KB".format(value.toDouble() / 1024)
        }
}
