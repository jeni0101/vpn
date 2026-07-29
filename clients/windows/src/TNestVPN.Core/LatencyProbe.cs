using System.Diagnostics;

namespace TNestVPN.Core;

public sealed record NodeLatency(CatalogNode Node, TimeSpan? Median);

public sealed class LatencyProbe(HttpClient http)
{
    public async Task<IReadOnlyList<NodeLatency>> ProbeAsync(
        IEnumerable<CatalogNode> nodes,
        CancellationToken cancellation)
    {
        using var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancellation);
        deadline.CancelAfter(TimeSpan.FromSeconds(5));
        var values = new List<NodeLatency>();
        foreach (var node in nodes.OrderBy(value => value.Priority)) {
            var samples = new List<TimeSpan>(3);
            for (var attempt = 0; attempt < 3; attempt++) {
                try {
                    using var request = new HttpRequestMessage(HttpMethod.Head, node.ProbeUrl);
                    var stopwatch = Stopwatch.StartNew();
                    using var response = await http.SendAsync(
                        request, HttpCompletionOption.ResponseHeadersRead, deadline.Token);
                    stopwatch.Stop();
                    if (response.IsSuccessStatusCode) samples.Add(stopwatch.Elapsed);
                } catch (OperationCanceledException) when (!cancellation.IsCancellationRequested) {
                    break;
                } catch (HttpRequestException) {
                    // A failed sample does not expose traffic or trigger a large transfer.
                }
            }
            TimeSpan? median = samples.Count == 0
                ? null
                : samples.Order().ElementAt(samples.Count / 2);
            values.Add(new NodeLatency(node, median));
            if (deadline.IsCancellationRequested) break;
        }
        return values;
    }

    public static CatalogNode Select(IReadOnlyList<NodeLatency> results) =>
        results.Where(result => result.Median is not null)
            .OrderBy(result => result.Node.Priority)
            .ThenBy(result => result.Median)
            .Select(result => result.Node)
            .FirstOrDefault()
        ?? throw new InvalidOperationException("所选地区没有可用节点");
}
