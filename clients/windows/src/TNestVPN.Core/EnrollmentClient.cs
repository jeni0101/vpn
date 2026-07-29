using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Json;
using NSec.Cryptography;

namespace TNestVPN.Core;

public sealed record EnrollmentDocument(
    int Version,
    string Type,
    string ManagementUrl,
    string Token,
    DateTimeOffset ExpiresAt,
    string DeviceName,
    string? Purpose);

public sealed class EnrollmentClient(HttpClient http)
{
    private static readonly Uri TrustedOrigin = new("https://vpn.example.com");
    private static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web);

    public async Task<WireGuardConfig> EnrollAsync(string document, CancellationToken cancellation)
    {
        var invite = JsonSerializer.Deserialize<EnrollmentDocument>(document, Json)
            ?? throw new FormatException("注册文件无效");
        var management = new Uri(invite.ManagementUrl);
        if (invite.Version != 1 || invite.Type != "tnest-vpn-enrollment" ||
            management.Scheme != Uri.UriSchemeHttps || management.Host != TrustedOrigin.Host ||
            management.Port != 443 || management.AbsolutePath != "/" ||
            invite.ExpiresAt <= DateTimeOffset.UtcNow || invite.Token.Length < 43)
            throw new FormatException("注册文件无效或已过期");

        var algorithm = KeyAgreementAlgorithm.X25519;
        using var privateKey = new Key(algorithm, new KeyCreationParameters {
            ExportPolicy = KeyExportPolicies.AllowPlaintextExport
        });
        var privateBytes = privateKey.Export(KeyBlobFormat.RawPrivateKey);
        var publicBytes = privateKey.PublicKey.Export(KeyBlobFormat.RawPublicKey);
        var psk = RandomNumberGenerator.GetBytes(32);
        try
        {
            using var response = await http.PostAsJsonAsync(
                new Uri(TrustedOrigin, "/api/v1/enrollments/claim"),
                new {
                    token = invite.Token,
                    public_key = Convert.ToBase64String(publicBytes),
                    preshared_key = Convert.ToBase64String(psk)
                }, Json, cancellation);
            response.EnsureSuccessStatusCode();
            using var payload = await JsonDocument.ParseAsync(
                await response.Content.ReadAsStreamAsync(cancellation),
                cancellationToken: cancellation);
            var root = payload.RootElement;
            var config = root.GetProperty("configuration");
            var peer = config.GetProperty("peer");
            var addresses = string.Join(", ", config.GetProperty("address")
                .EnumerateArray().Select(x => x.GetString()));
            var dns = string.Join(", ", config.GetProperty("dns")
                .EnumerateArray().Select(x => x.GetString()));
            var rendered = $"""
                [Interface]
                PrivateKey = {Convert.ToBase64String(privateBytes)}
                Address = {addresses}
                DNS = {dns}
                MTU = {config.GetProperty("mtu").GetInt32()}

                [Peer]
                PublicKey = {peer.GetProperty("server_public_key").GetString()}
                PresharedKey = {Convert.ToBase64String(psk)}
                Endpoint = {peer.GetProperty("endpoint").GetString()}
                AllowedIPs = 0.0.0.0/0, ::/0
                PersistentKeepalive = 25

                """;
            return SafeWireGuardConfig.Parse(rendered);
        }
        finally
        {
            CryptographicOperations.ZeroMemory(privateBytes);
            CryptographicOperations.ZeroMemory(psk);
        }
    }
}
