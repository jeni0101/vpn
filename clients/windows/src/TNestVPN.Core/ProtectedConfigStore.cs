using System.Security.Cryptography;

namespace TNestVPN.Core;

public sealed class ProtectedConfigStore
{
    private readonly string directory = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData),
        "TNest VPN");
    public string ConfigPath => Path.Combine(directory, "tnest.conf.dpapi");
    public string RuntimeConfigPath => Path.Combine(directory, "tnest.conf");

    public void Save(string value)
    {
        Directory.CreateDirectory(directory);
        var plain = System.Text.Encoding.UTF8.GetBytes(value);
        try
        {
            var encrypted = ProtectedData.Protect(plain, null, DataProtectionScope.LocalMachine);
            File.WriteAllBytes(ConfigPath, encrypted);
            File.SetAttributes(ConfigPath, FileAttributes.Hidden);
        }
        finally { CryptographicOperations.ZeroMemory(plain); }
    }

    public string Load()
    {
        var encrypted = File.ReadAllBytes(ConfigPath);
        var plain = ProtectedData.Unprotect(encrypted, null, DataProtectionScope.LocalMachine);
        try { return System.Text.Encoding.UTF8.GetString(plain); }
        finally { CryptographicOperations.ZeroMemory(plain); }
    }

    public void MaterializeForService()
    {
        var value = Load();
        File.WriteAllText(RuntimeConfigPath, value, new System.Text.UTF8Encoding(false));
        // ACL hardening is applied by the elevated ServiceHost during install.
    }
}
