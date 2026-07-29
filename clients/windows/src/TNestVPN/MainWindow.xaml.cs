using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Net.NetworkInformation;
using System.ServiceProcess;
using System.Windows;
using System.Windows.Controls;
using Microsoft.Win32;
using TNestVPN.Core;

namespace TNestVPN;

public partial class MainWindow : Window
{
    private const string ServiceName = "WireGuardTunnel$tnest";
    private readonly ProtectedConfigStore store = new();
    private readonly HttpClient http = new() { Timeout = TimeSpan.FromSeconds(20) };
    private readonly UsageLedger ledger = new();
    private readonly Dictionary<string, CatalogNode> selectedNodes =
        new(StringComparer.OrdinalIgnoreCase);
    private readonly System.Windows.Threading.DispatcherTimer timer = new() {
        Interval = TimeSpan.FromSeconds(5)
    };
    private MultiRegionProfile? profile;

    public MainWindow()
    {
        InitializeComponent();
        Loaded += SyncProfileOnLoad;
        timer.Tick += (_, _) => RefreshStats();
        timer.Start();
        if (store.HasProfile) {
            try {
                profile = store.LoadProfile();
                LoadRegions(profile);
            } catch {
                DetailText.Text = "多地区配置损坏，请重新导入注册文件";
            }
        }
        if (File.Exists(store.ConfigPath)) {
            ConnectButton.IsEnabled = true;
            StatusText.Text = "配置已就绪";
        }
    }

    private async void SyncProfileOnLoad(object sender, RoutedEventArgs e)
    {
        if (profile is null || IsServiceRunning()) return;
        try {
            var selected = (RegionBox.SelectedItem as RegionChoice)?.Code;
            profile = await new EnrollmentClient(http).SyncProfileAsync(
                profile, CancellationToken.None);
            store.SaveProfile(profile);
            LoadRegions(profile);
            if (selected is not null) {
                RegionBox.SelectedItem = (RegionBox.ItemsSource as IEnumerable<RegionChoice>)
                    ?.FirstOrDefault(value => value.Code == selected);
            }
            PrepareSelectedRegion();
            DetailText.Text = "地区和节点清单已同步";
        } catch (Exception ex) {
            DetailText.Text = $"使用已验签缓存 · 上次同步失败：{ex.Message}";
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
            if (path.EndsWith(".tnestvpn", StringComparison.OrdinalIgnoreCase)) {
                profile = await new EnrollmentClient(http).EnrollProfileAsync(
                    text, CancellationToken.None);
                store.SaveProfile(profile);
                LoadRegions(profile);
                PrepareSelectedRegion();
            } else {
                profile = null;
                var config = SafeWireGuardConfig.Parse(text);
                store.Save(config.Text);
                RegionBox.ItemsSource = null;
                RegionBox.IsEnabled = false;
                LatencyButton.IsEnabled = false;
                DetailText.Text = config.Endpoint;
            }
            InstallService();
            ConnectButton.IsEnabled = true;
            StatusText.Text = "配置已就绪";
        } catch (Exception ex) {
            MessageBox.Show(ex.Message, "导入失败", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void LoadRegions(MultiRegionProfile value)
    {
        var choices = value.Catalog.Regions
            .Where(region => value.Regions.Any(secret =>
                string.Equals(secret.RegionCode, region.Code, StringComparison.OrdinalIgnoreCase)))
            .OrderBy(region => region.SortOrder)
            .Select(region => new RegionChoice(region.Code, region.DisplayName))
            .ToArray();
        RegionBox.ItemsSource = choices;
        RegionBox.SelectedIndex = choices.Length == 0 ? -1 : 0;
        RegionBox.IsEnabled = choices.Length > 0 && !IsServiceRunning();
        LatencyButton.IsEnabled = choices.Length > 0 && !IsServiceRunning();
    }

    private void RegionSelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (profile is null || RegionBox.SelectedItem is not RegionChoice || IsServiceRunning())
            return;
        try {
            PrepareSelectedRegion();
            InstallService();
        } catch (Exception ex) {
            MessageBox.Show(ex.Message, "切换地区失败", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private async void TestLatencyClick(object sender, RoutedEventArgs e)
    {
        if (profile is null || RegionBox.SelectedItem is not RegionChoice choice)
            return;
        if (IsServiceRunning()) {
            MessageBox.Show("请先断开 VPN，再测试延迟。");
            return;
        }
        try {
            LatencyButton.IsEnabled = false;
            DetailText.Text = "正在执行三次轻量 HTTPS 延迟探测…";
            var region = profile.Region(choice.Code);
            var results = await new LatencyProbe(http).ProbeAsync(
                region.Configuration.Nodes, CancellationToken.None);
            var selected = LatencyProbe.Select(results);
            selectedNodes[choice.Code] = selected;
            PrepareSelectedRegion();
            InstallService();
            var latency = results.First(value => value.Node.Id == selected.Id).Median;
            DetailText.Text = $"{choice.DisplayName} · {latency?.TotalMilliseconds:F0} ms";
        } catch (Exception ex) {
            DetailText.Text = ex.Message;
        } finally {
            LatencyButton.IsEnabled = true;
        }
    }

    private void PrepareSelectedRegion()
    {
        if (profile is null || RegionBox.SelectedItem is not RegionChoice choice)
            return;
        var region = profile.Region(choice.Code);
        var node = selectedNodes.GetValueOrDefault(choice.Code)
            ?? region.Configuration.Nodes.OrderBy(value => value.Priority).First();
        selectedNodes[choice.Code] = node;
        var config = profile.Render(choice.Code, node);
        store.Save(config.Text);
        DetailText.Text = $"{choice.DisplayName} · {node.Endpoint}";
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
                RegionBox.IsEnabled = profile is not null;
                LatencyButton.IsEnabled = profile is not null;
            } else {
                PrepareSelectedRegion();
                store.MaterializeForService();
                service.Start();
                service.WaitForStatus(ServiceControllerStatus.Running, TimeSpan.FromSeconds(20));
                StatusText.Text = "已连接";
                ConnectButton.Content = "断开";
                RegionBox.IsEnabled = false;
                LatencyButton.IsEnabled = false;
            }
        } catch (Exception ex) {
            MessageBox.Show(ex.Message, "连接失败", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private static bool IsServiceRunning()
    {
        try {
            using var service = new ServiceController(ServiceName);
            return service.Status == ServiceControllerStatus.Running ||
                service.Status == ServiceControllerStatus.StartPending;
        } catch {
            return false;
        }
    }

    private void RefreshStats()
    {
        var adapter = NetworkInterface.GetAllNetworkInterfaces().FirstOrDefault(
            value => value.Name.Contains("tnest", StringComparison.OrdinalIgnoreCase));
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

    private sealed record RegionChoice(string Code, string DisplayName);
}
