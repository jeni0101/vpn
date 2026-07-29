using TNestVPN.Core;
using Xunit;

namespace TNestVPN.Core.Tests;

public sealed class SafeWireGuardConfigTests
{
    private const string Key = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
    private static readonly string Valid = $"""
        [Interface]
        PrivateKey = {Key}
        Address = 10.66.0.14/32, fd66:66:66::14/128
        DNS = 1.1.1.1
        MTU = 1420

        [Peer]
        PublicKey = {Key}
        PresharedKey = {Key}
        Endpoint = 203.0.113.10:51999
        AllowedIPs = 0.0.0.0/0, ::/0
        PersistentKeepalive = 25
        """;

    [Fact]
    public void AcceptsSafeDualStackConfiguration()
    {
        Assert.Equal("203.0.113.10:51999", SafeWireGuardConfig.Parse(Valid).Endpoint);
    }

    [Theory]
    [InlineData("PostUp = calc.exe")]
    [InlineData("PreDown = powershell.exe")]
    public void RejectsCommandFields(string field)
    {
        Assert.Throws<FormatException>(() =>
            SafeWireGuardConfig.Parse(Valid.Replace("MTU = 1420", $"MTU = 1420\n{field}")));
    }

    [Fact]
    public void RejectsIPv4OnlyRoute()
    {
        Assert.Throws<FormatException>(() =>
            SafeWireGuardConfig.Parse(Valid.Replace("0.0.0.0/0, ::/0", "0.0.0.0/0")));
    }
}
