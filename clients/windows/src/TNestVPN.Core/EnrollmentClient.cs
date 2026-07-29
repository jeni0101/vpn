using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Json;
using System.Text.Json.Serialization;
using NSec.Cryptography;

namespace TNestVPN.Core;

public sealed record EnrollmentDocument(
    [property: JsonPropertyName("version")] int Version,
    [property: JsonPropertyName("type")] string Type,
    [property: JsonPropertyName("management_url")] string ManagementUrl,
    [property: JsonPropertyName("token")] string Token,
    [property: JsonPropertyName("expires_at")] DateTimeOffset ExpiresAt,
    [property: JsonPropertyName("device_name")] string DeviceName,
    [property: JsonPropertyName("catalog_signing_key")] string CatalogSigningKey,
    [property: JsonPropertyName("purpose")] string? Purpose);

public sealed class EnrollmentClient(HttpClient http)
{
    private static readonly Uri TrustedOrigin = new(
        Environment.GetEnvironmentVariable("TNEST_MANAGEMENT_URL") ??
        "https://vpn.tnestai.asia");
    private static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web) {
        UnmappedMemberHandling = JsonUnmappedMemberHandling.Disallow
    };

    public async Task<MultiRegionProfile> EnrollProfileAsync(
        string document,
        CancellationToken cancellation)
    {
        var invite = JsonSerializer.Deserialize<EnrollmentDocument>(document, Json)
            ?? throw new FormatException("注册文件无效");
        var management = new Uri(invite.ManagementUrl);
        if (invite.Version != 2 || invite.Type != "tnest-vpn-enrollment" ||
            management.Scheme != Uri.UriSchemeHttps ||
            !string.Equals(management.Host, TrustedOrigin.Host, StringComparison.OrdinalIgnoreCase) ||
            management.Port != 443 || management.AbsolutePath != "/" ||
            invite.ExpiresAt <= DateTimeOffset.UtcNow || invite.Token.Length < 43 ||
            string.IsNullOrWhiteSpace(invite.CatalogSigningKey))
            throw new FormatException("注册文件无效或已过期");

        var catalog = await http.GetFromJsonAsync<SignedCatalog>(
            new Uri(management, "/api/v2/client/catalog"), Json, cancellation)
            ?? throw new FormatException("地区清单无效");
        VerifyCatalog(catalog, invite.CatalogSigningKey);
        if (catalog.ExpiresAt <= DateTimeOffset.UtcNow || catalog.Regions.Count == 0)
            throw new FormatException("地区清单已过期或为空");

        var generated = new List<GeneratedRegion>();
        try {
            foreach (var region in catalog.Regions.Where(value => value.Nodes.Count > 0)) {
                var algorithm = KeyAgreementAlgorithm.X25519;
                using var privateKey = new Key(algorithm, new KeyCreationParameters {
                    ExportPolicy = KeyExportPolicies.AllowPlaintextExport
                });
                var privateBytes = privateKey.Export(KeyBlobFormat.RawPrivateKey);
                var publicBytes = privateKey.PublicKey.Export(KeyBlobFormat.RawPublicKey);
                var psk = RandomNumberGenerator.GetBytes(32);
                generated.Add(new GeneratedRegion(
                    region.Code, Convert.ToBase64String(privateBytes),
                    Convert.ToBase64String(publicBytes), Convert.ToBase64String(psk)));
                CryptographicOperations.ZeroMemory(privateBytes);
                CryptographicOperations.ZeroMemory(publicBytes);
                CryptographicOperations.ZeroMemory(psk);
            }
            using var response = await http.PostAsJsonAsync(
                new Uri(management, "/api/v2/enrollments/claim"),
                new ClaimRequest(invite.Token, generated.Select(value =>
                    new RegionClaim(value.RegionCode, value.PublicKey, value.PresharedKey)).ToArray()),
                Json, cancellation);
            response.EnsureSuccessStatusCode();
            var payload = await response.Content.ReadFromJsonAsync<ClaimResponse>(Json, cancellation)
                ?? throw new FormatException("注册响应无效");
            VerifyCatalog(payload.Catalog, invite.CatalogSigningKey);
            if (payload.DeviceToken.Length < 32)
                throw new FormatException("设备凭据无效");
            var configurations = payload.Regions.ToDictionary(
                value => value.RegionCode, StringComparer.OrdinalIgnoreCase);
            var secrets = generated.Select(value => {
                if (!configurations.TryGetValue(value.RegionCode, out var configuration))
                    throw new FormatException($"缺少 {value.RegionCode} 地区配置");
                return new RegionSecret(
                    value.RegionCode, value.PrivateKey, value.PresharedKey, configuration);
            }).ToArray();
            return new MultiRegionProfile(
                invite.DeviceName, management, payload.DeviceToken,
                invite.CatalogSigningKey, payload.Catalog, secrets);
        }
        catch {
            generated.Clear();
            throw;
        }
    }

    public async Task<MultiRegionProfile> SyncProfileAsync(
        MultiRegionProfile profile,
        CancellationToken cancellation)
    {
        var catalog = await http.GetFromJsonAsync<SignedCatalog>(
            new Uri(profile.ManagementUrl, "/api/v2/client/catalog"), Json, cancellation)
            ?? throw new FormatException("地区清单无效");
        VerifyCatalog(catalog, profile.CatalogSigningKey);
        if (catalog.ExpiresAt <= DateTimeOffset.UtcNow)
            throw new FormatException("地区清单已过期");

        using var regionsRequest = new HttpRequestMessage(
            HttpMethod.Get, new Uri(profile.ManagementUrl, "/api/v2/client/regions"));
        regionsRequest.Headers.Authorization =
            new System.Net.Http.Headers.AuthenticationHeaderValue("Bearer", profile.DeviceToken);
        using var regionsResponse = await http.SendAsync(regionsRequest, cancellation);
        regionsResponse.EnsureSuccessStatusCode();
        var current = await regionsResponse.Content.ReadFromJsonAsync<RegionsResponse>(
            Json, cancellation) ?? throw new FormatException("地区配置响应无效");
        var serverConfigurations = current.Regions.ToDictionary(
            value => value.RegionCode, StringComparer.OrdinalIgnoreCase);
        var secrets = profile.Regions.Select(value =>
            serverConfigurations.TryGetValue(value.RegionCode, out var configuration)
                ? value with { Configuration = configuration }
                : value).ToList();

        foreach (var region in catalog.Regions.Where(value => value.Nodes.Count > 0)) {
            if (secrets.Any(value => string.Equals(
                value.RegionCode, region.Code, StringComparison.OrdinalIgnoreCase)))
                continue;
            var generated = GenerateRegion(region.Code);
            using var request = new HttpRequestMessage(
                HttpMethod.Post,
                new Uri(profile.ManagementUrl,
                    $"/api/v2/client/regions/{Uri.EscapeDataString(region.Code)}/enroll"));
            request.Headers.Authorization =
                new System.Net.Http.Headers.AuthenticationHeaderValue(
                    "Bearer", profile.DeviceToken);
            request.Content = JsonContent.Create(
                new RegionClaim(region.Code, generated.PublicKey, generated.PresharedKey),
                options: Json);
            using var response = await http.SendAsync(request, cancellation);
            response.EnsureSuccessStatusCode();
            var result = await response.Content.ReadFromJsonAsync<EnrollRegionResponse>(
                Json, cancellation) ?? throw new FormatException("地区注册响应无效");
            secrets.Add(new RegionSecret(
                region.Code, generated.PrivateKey, generated.PresharedKey,
                result.Configuration));
        }
        return profile with { Catalog = catalog, Regions = secrets };
    }

    public async Task<UsageSummary> FetchUsageAsync(
        MultiRegionProfile profile,
        string regionCode,
        string range,
        CancellationToken cancellation)
    {
        if (range is not ("24h" or "7d" or "30d"))
            throw new ArgumentOutOfRangeException(nameof(range));
        var path = $"/api/v2/client/usage?range={range}&region=" +
            Uri.EscapeDataString(regionCode);
        using var request = new HttpRequestMessage(
            HttpMethod.Get, new Uri(profile.ManagementUrl, path));
        request.Headers.Authorization =
            new System.Net.Http.Headers.AuthenticationHeaderValue(
                "Bearer", profile.DeviceToken);
        using var response = await http.SendAsync(request, cancellation);
        response.EnsureSuccessStatusCode();
        using var document = JsonDocument.Parse(
            await response.Content.ReadAsStreamAsync(cancellation));
        long upload = 0;
        long download = 0;
        foreach (var point in document.RootElement.GetProperty("points").EnumerateArray()) {
            upload += point.GetProperty("upload_bytes").GetInt64();
            download += point.GetProperty("download_bytes").GetInt64();
        }
        var syncedAt = document.RootElement.TryGetProperty("synced_at", out var value)
            ? value.GetDateTimeOffset()
            : DateTimeOffset.UtcNow;
        return new UsageSummary(upload, download, syncedAt);
    }

    public static void VerifyCatalog(SignedCatalog catalog, string encodedPublicKey)
    {
        byte[] publicBytes;
        byte[] signature;
        byte[] payload;
        try {
            publicBytes = DecodeRawBase64(encodedPublicKey);
            signature = DecodeRawBase64(catalog.Signature);
            payload = DecodeRawBase64(catalog.SignedPayload);
        } catch (FormatException) {
            throw new CryptographicException("地区清单签名无效");
        }
        try {
            var algorithm = SignatureAlgorithm.Ed25519;
            var publicKey = PublicKey.Import(
                algorithm, publicBytes, KeyBlobFormat.RawPublicKey);
            if (!algorithm.Verify(publicKey, payload, signature))
                throw new CryptographicException("地区清单签名无效");
            var signed = JsonSerializer.Deserialize<SignedCatalog>(payload, Json)
                ?? throw new CryptographicException("地区清单载荷无效");
            if (signed.Signature != "" || signed.SignedPayload != "" ||
                signed.Version != catalog.Version ||
                signed.ExpiresAt != catalog.ExpiresAt ||
                JsonSerializer.Serialize(signed.Regions, Json) !=
                JsonSerializer.Serialize(catalog.Regions, Json))
                throw new CryptographicException("地区清单内容与签名不一致");
        }
        finally {
            CryptographicOperations.ZeroMemory(publicBytes);
            CryptographicOperations.ZeroMemory(signature);
            CryptographicOperations.ZeroMemory(payload);
        }
    }

    private static byte[] DecodeRawBase64(string value)
    {
        var padded = value.PadRight(value.Length + ((4 - value.Length % 4) % 4), '=');
        return Convert.FromBase64String(padded);
    }

    private static GeneratedRegion GenerateRegion(string code)
    {
        var algorithm = KeyAgreementAlgorithm.X25519;
        using var privateKey = new Key(algorithm, new KeyCreationParameters {
            ExportPolicy = KeyExportPolicies.AllowPlaintextExport
        });
        var privateBytes = privateKey.Export(KeyBlobFormat.RawPrivateKey);
        var publicBytes = privateKey.PublicKey.Export(KeyBlobFormat.RawPublicKey);
        var psk = RandomNumberGenerator.GetBytes(32);
        try {
            return new GeneratedRegion(
                code, Convert.ToBase64String(privateBytes),
                Convert.ToBase64String(publicBytes), Convert.ToBase64String(psk));
        }
        finally {
            CryptographicOperations.ZeroMemory(privateBytes);
            CryptographicOperations.ZeroMemory(publicBytes);
            CryptographicOperations.ZeroMemory(psk);
        }
    }

    private sealed record GeneratedRegion(
        string RegionCode, string PrivateKey, string PublicKey, string PresharedKey);

    private sealed record RegionClaim(
        [property: JsonPropertyName("region_code")] string RegionCode,
        [property: JsonPropertyName("public_key")] string PublicKey,
        [property: JsonPropertyName("preshared_key")] string PresharedKey);

    private sealed record ClaimRequest(
        [property: JsonPropertyName("token")] string Token,
        [property: JsonPropertyName("credentials")] IReadOnlyList<RegionClaim> Credentials);

    private sealed record ClaimResponse(
        [property: JsonPropertyName("device_token")] string DeviceToken,
        [property: JsonPropertyName("catalog")] SignedCatalog Catalog,
        [property: JsonPropertyName("regions")] IReadOnlyList<RegionConfiguration> Regions);

    private sealed record RegionsResponse(
        [property: JsonPropertyName("regions")] IReadOnlyList<RegionConfiguration> Regions);

    private sealed record EnrollRegionResponse(
        [property: JsonPropertyName("configuration")] RegionConfiguration Configuration);
}

public sealed record UsageSummary(
    long UploadBytes,
    long DownloadBytes,
    DateTimeOffset SyncedAt);
