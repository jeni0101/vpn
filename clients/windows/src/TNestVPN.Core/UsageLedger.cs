using System.Text.Json;

namespace TNestVPN.Core;

public sealed record UsageTotals(
    long SessionDownload, long SessionUpload,
    long TodayDownload, long TodayUpload,
    long MonthDownload, long MonthUpload);

public sealed class UsageLedger
{
    private sealed class State
    {
        public string Day { get; set; } = "";
        public string Month { get; set; } = "";
        public long DayRx { get; set; }
        public long DayTx { get; set; }
        public long MonthRx { get; set; }
        public long MonthTx { get; set; }
    }

    private readonly string path = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
        "TNest VPN", "usage.json");
    private State state;
    private long lastRx = -1, lastTx = -1, sessionRx, sessionTx;
    private DateTimeOffset lastPersist = DateTimeOffset.MinValue;

    public UsageLedger()
    {
        try { state = JsonSerializer.Deserialize<State>(File.ReadAllText(path)) ?? new(); }
        catch { state = new(); }
    }

    public UsageTotals Update(long rx, long tx, DateTimeOffset now)
    {
        var shanghai = TimeZoneInfo.ConvertTime(now,
            TimeZoneInfo.FindSystemTimeZoneById("China Standard Time"));
        var day = shanghai.ToString("yyyy-MM-dd");
        var month = shanghai.ToString("yyyy-MM");
        if (state.Day != day) {
            state.Day = day;
            state.DayRx = state.DayTx = 0;
        }
        if (state.Month != month) {
            state.Month = month;
            state.MonthRx = state.MonthTx = 0;
        }
        if (lastRx >= 0) {
            var deltaRx = rx >= lastRx ? rx - lastRx : rx;
            var deltaTx = tx >= lastTx ? tx - lastTx : tx;
            sessionRx += deltaRx;
            sessionTx += deltaTx;
            state.DayRx += deltaRx;
            state.DayTx += deltaTx;
            state.MonthRx += deltaRx;
            state.MonthTx += deltaTx;
        }
        lastRx = rx;
        lastTx = tx;
        if (now - lastPersist >= TimeSpan.FromSeconds(30)) {
            Directory.CreateDirectory(Path.GetDirectoryName(path)!);
            File.WriteAllText(path, JsonSerializer.Serialize(state));
            lastPersist = now;
        }
        return new(sessionRx, sessionTx, state.DayRx, state.DayTx,
            state.MonthRx, state.MonthTx);
    }
}
