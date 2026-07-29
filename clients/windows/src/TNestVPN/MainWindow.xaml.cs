using System.Diagnostics;
using System.IO;
using System.Net.NetworkInformation;
using System.Net.Http;
using System.ServiceProcess;
using System.Windows;
using Microsoft.Win32;
using TNestVPN.Core;

namespace TNestVPN;

public partial class MainWindow : Window
{
    private const string ServiceName = "WireGuardTunnel$tnest";
    private readonly ProtectedConfigStore store = new();
    private readonly HttpClient http = new() { Timeout = TimeSpan.FromSeconds(20) };
    private readonly UsageLedger ledger = new();
    private readonly System.Windows.Threading.DispatcherTimer timer = new() {
        Interval = TimeSpan.FromSeconds(5)
    };

    public MainWindow()
    {
        InitializeComponent();
        timer.Tick += (_, _) => RefreshStats();
        timer.Start();
        if (File.Exists(store.ConfigPath)) {
            ConnectButton.IsEnabled = true;
            StatusText.Text = "配置已就绪";
        }
    }

    private async void ImportClick(object sender, RoutedEventArgs e)
    {
        var dialog = new OpenFileDialog {
            Filter = "TNest VPN 配置|*.conf;*.tnestvpn",
            CheckFileExists = true
        };
        if (dialog.ShowDialog() == true) await ImportAsync(dialog.FileName);
    }

    private async void OnDrop(object sender, DragEventArgs e)
    {
        if (e.Data.GetData(DataFormats.FileDrop) is string[] { Length: 1 } files)
            await ImportAsync(files[0]);
    }

    private async Task ImportAsync(string path)
    {
        try {
            var text = await File.ReadAllTextAsync(path);
            var config = path.EndsWith(".tnestvpn", StringComparison.OrdinalIgnoreCase)
                ? await new EnrollmentClient(http).EnrollAsync(text, CancellationToken.None)
                : SafeWireGuardConfig.Parse(text);
            store.Save(config.Text);
            InstallService();
            ConnectButton.IsEnabled = true;
            StatusText.Text = "配置已就绪";
            DetailText.Text = config.Endpoint;
        } catch (Exception ex) {
            MessageBox.Show(ex.Message, "导入失败", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void InstallService()
    {
        var host = Path.Combine(AppContext.BaseDirectory, "TNestVPN.ServiceHost.exe");
        Process.Start(new ProcessStartInfo {
            FileName = host,
            Arguments = $"/install \"{store.ConfigPath}\"",
            UseShellExecute = true,
            Verb = "runas"
        })!.WaitForExit();
    }

    private void ConnectClick(object sender, RoutedEventArgs e)
    {
        try {
            using var service = new ServiceController(ServiceName);
            if (service.Status == ServiceControllerStatus.Running) {
                service.Stop();
                service.WaitForStatus(ServiceControllerStatus.Stopped, TimeSpan.FromSeconds(15));
                StatusText.Text = "已断开";
                ConnectButton.Content = "连接";
            } else {
                service.Start();
                service.WaitForStatus(ServiceControllerStatus.Running, TimeSpan.FromSeconds(20));
                StatusText.Text = "已连接";
                ConnectButton.Content = "断开";
            }
        } catch (Exception ex) {
            MessageBox.Show(ex.Message, "连接失败", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void RefreshStats()
    {
        var adapter = NetworkInterface.GetAllNetworkInterfaces().FirstOrDefault(
            x => x.Name.Contains("tnest", StringComparison.OrdinalIgnoreCase));
        if (adapter is null) return;
        var stats = adapter.GetIPStatistics();
        var totals = ledger.Update(stats.BytesReceived, stats.BytesSent, DateTimeOffset.Now);
        UsageText.Text = $"当前　下载 {Format(totals.SessionDownload)}　上传 {Format(totals.SessionUpload)}";
        HistoryUsageText.Text =
            $"今日 ↓{Format(totals.TodayDownload)} / ↑{Format(totals.TodayUpload)}　" +
            $"本月 ↓{Format(totals.MonthDownload)} / ↑{Format(totals.MonthUpload)}";
    }

    private static string Format(long value) => value switch {
        >= 1L << 30 => $"{value / (double)(1L << 30):F2} GB",
        >= 1L << 20 => $"{value / (double)(1L << 20):F2} MB",
        _ => $"{value / 1024d:F1} KB"
    };
}
