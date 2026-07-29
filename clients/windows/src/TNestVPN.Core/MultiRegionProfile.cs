using System.Text.Json.Serialization;

namespace TNestVPN.Core;

public sealed record CatalogNode(
    [property: JsonPropertyName("id")] string Id,
    [property: JsonPropertyName("endpoint")] string Endpoint,
    [property: JsonPropertyName("probe_url")] string ProbeUrl,
    [property: JsonPropertyName("server_public_key")] string ServerPublicKey,
    [property: JsonPropertyName("priority")] int Priority);

public sealed record CatalogRegion(
    [property: JsonPropertyName("code")] string Code,
    [property: JsonPropertyName("display_name")] string DisplayName,
    [property: JsonPropertyName("sort_order")] int SortOrder,
    [property: JsonPropertyName("exit_mode")] string ExitMode,
    [property: JsonPropertyName("config_version")] int ConfigVersion,
    [property: JsonPropertyName("nodes")] IReadOnlyList<CatalogNode> Nodes);

public sealed record SignedCatalog(
    [property: JsonPropertyName("catalog_version")] long Version,
    [property: JsonPropertyName("issued_at")] DateTimeOffset IssuedAt,
    [property: JsonPropertyName("expires_at")] DateTimeOffset ExpiresAt,
    [property: JsonPropertyName("regions")] IReadOnlyList<CatalogRegion> Regions,
    [property: JsonPropertyName("signature")] string Signature,
    [property: JsonPropertyName("signed_payload")] string SignedPayload);

public sealed record RegionConfiguration(
    [property: JsonPropertyName("region_code")] string RegionCode,
    [property: JsonPropertyName("address")] IReadOnlyList<string> Address,
    [property: JsonPropertyName("dns")] IReadOnlyList<string> Dns,
    [property: JsonPropertyName("mtu")] int Mtu,
    [property: JsonPropertyName("allowed_ips")] IReadOnlyList<string> AllowedIps,
    [property: JsonPropertyName("persistent_keepalive")] int PersistentKeepalive,
    [property: JsonPropertyName("config_version")] int ConfigVersion,
    [property: JsonPropertyName("nodes")] IReadOnlyList<CatalogNode> Nodes);

public sealed record RegionSecret(
    string RegionCode,
    string PrivateKey,
    string PresharedKey,
    RegionConfiguration Configuration);

public sealed record MultiRegionProfile(
    string DeviceName,
    Uri ManagementUrl,
    string DeviceToken,
    string CatalogSigningKey,
    SignedCatalog Catalog,
    IReadOnlyList<RegionSecret> Regions)
{
    public RegionSecret Region(string code) =>
        Regions.Single(region => string.Equals(
            region.RegionCode, code, StringComparison.OrdinalIgnoreCase));

    public WireGuardConfig Render(string regionCode, CatalogNode node)
    {
        var region = Region(regionCode);
        if (!region.Configuration.Nodes.Any(candidate => candidate.Id == node.Id))
            throw new InvalidOperationException("节点不属于所选地区");
        var text = $"""
            [Interface]
            PrivateKey = {region.PrivateKey}
            Address = {string.Join(", ", region.Configuration.Address)}
            DNS = {string.Join(", ", region.Configuration.Dns)}
            MTU = {region.Configuration.Mtu}

            [Peer]
            PublicKey = {node.ServerPublicKey}
            PresharedKey = {region.PresharedKey}
            Endpoint = {node.Endpoint}
            AllowedIPs = 0.0.0.0/0, ::/0
            PersistentKeepalive = 25

            """;
        return SafeWireGuardConfig.Parse(text);
    }
}
