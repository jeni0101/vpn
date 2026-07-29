using System.Net;
using System.Text.RegularExpressions;

namespace TNestVPN.Core;

public sealed record WireGuardConfig(string Text, string Endpoint);

public static partial class SafeWireGuardConfig
{
    private static readonly HashSet<string> InterfaceFields =
        new(StringComparer.OrdinalIgnoreCase) { "PrivateKey", "Address", "DNS", "MTU" };
    private static readonly HashSet<string> PeerFields =
        new(StringComparer.OrdinalIgnoreCase) {
            "PublicKey", "PresharedKey", "Endpoint", "AllowedIPs", "PersistentKeepalive"
        };

    public static WireGuardConfig Parse(string text)
    {
        if (string.IsNullOrWhiteSpace(text) || text.Length > 64 * 1024)
            throw new FormatException("配置为空或过大");
        var section = "";
        var peerCount = 0;
        var values = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        foreach (var original in text.Replace("\r", "").Split('\n'))
        {
            var line = original.Split('#', ';')[0].Trim();
            if (line.Length == 0) continue;
            if (line.StartsWith('[') && line.EndsWith(']'))
            {
                section = line.ToLowerInvariant();
                if (section is not "[interface]" and not "[peer]")
                    throw new FormatException("配置包含未知区域");
                if (section == "[peer]") peerCount++;
                continue;
            }
            var split = line.IndexOf('=');
            if (split < 1) throw new FormatException("配置行格式错误");
            var key = line[..split].Trim();
            var value = line[(split + 1)..].Trim();
            var allowed = section == "[interface]" ? InterfaceFields :
                section == "[peer]" ? PeerFields : null;
            if (allowed is null || !allowed.Contains(key))
                throw new FormatException($"拒绝危险或不支持的字段：{key}");
            values[section + key] = value;
        }
        if (peerCount != 1) throw new FormatException("TNest VPN 只接受一个 Peer");
        foreach (var key in new[] {
            "[interface]PrivateKey", "[interface]Address", "[peer]PublicKey",
            "[peer]PresharedKey", "[peer]Endpoint", "[peer]AllowedIPs" })
            if (!values.ContainsKey(key)) throw new FormatException($"缺少字段：{key}");
        ValidateKey(values["[interface]PrivateKey"]);
        ValidateKey(values["[peer]PublicKey"]);
        ValidateKey(values["[peer]PresharedKey"]);
        var routes = values["[peer]AllowedIPs"].Split(',').Select(x => x.Trim()).ToHashSet();
        if (!routes.SetEquals(new[] { "0.0.0.0/0", "::/0" }))
            throw new FormatException("必须使用 IPv4、IPv6 全局路由");
        var endpoint = values["[peer]Endpoint"];
        if (!EndpointPattern().IsMatch(endpoint))
            throw new FormatException("Endpoint 无效");
        return new WireGuardConfig(text, endpoint);
    }

    private static void ValidateKey(string value)
    {
        byte[] data;
        try { data = Convert.FromBase64String(value); }
        catch { throw new FormatException("WireGuard 密钥格式无效"); }
        if (data.Length != 32) throw new FormatException("WireGuard 密钥长度无效");
        Array.Clear(data);
    }

    [GeneratedRegex(@"^(\[[0-9A-Fa-f:]+\]|[A-Za-z0-9.-]+):[1-9][0-9]{0,4}$")]
    private static partial Regex EndpointPattern();
}
