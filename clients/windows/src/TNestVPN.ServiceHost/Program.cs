using System.ComponentModel;
using System.Runtime.InteropServices;
using Microsoft.Win32.SafeHandles;
using System.Security.AccessControl;
using System.Security.Principal;
using TNestVPN.Core;

namespace TNestVPN.ServiceHost;

internal static class Program
{
    private const string ServiceName = "WireGuardTunnel$tnest";

    static int Main(string[] args)
    {
        if (args is ["/service", var config])
            return TunnelNative.Run(config) ? 0 : 1;
        if (args is ["/install", var encryptedConfig])
        {
            if (!Path.GetFullPath(encryptedConfig).Equals(
                Path.GetFullPath(new ProtectedConfigStore().ConfigPath),
                StringComparison.OrdinalIgnoreCase))
                throw new InvalidOperationException("配置路径不受信任");
            var store = new ProtectedConfigStore();
            store.MaterializeForService();
            ServiceInstaller.Install(Environment.ProcessPath!, store.RuntimeConfigPath);
            return 0;
        }
        if (args is ["/remove"])
        {
            ServiceInstaller.Remove();
            return 0;
        }
        Console.Error.WriteLine("TNestVPN.ServiceHost /service|/install|/remove");
        return 2;
    }

    private static class TunnelNative
    {
        [UnmanagedFunctionPointer(CallingConvention.Cdecl, CharSet = CharSet.Unicode)]
        private delegate bool TunnelProc([MarshalAs(UnmanagedType.LPWStr)] string config);
        [DllImport("kernel32", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern nint LoadLibrary(string file);
        [DllImport("kernel32", SetLastError = true, CharSet = CharSet.Ansi)]
        private static extern nint GetProcAddress(nint module, string name);

        public static bool Run(string config)
        {
            var module = LoadLibrary(Path.Combine(AppContext.BaseDirectory, "tunnel.dll"));
            if (module == 0) throw new Win32Exception();
            var address = GetProcAddress(module, "WireGuardTunnelService");
            if (address == 0) throw new Win32Exception();
            return Marshal.GetDelegateForFunctionPointer<TunnelProc>(address)(config);
        }
    }

    private static class ServiceInstaller
    {
        private const uint ScManagerAllAccess = 0xF003F;
        private const uint ServiceAllAccess = 0xF01FF;
        private const uint ServiceWin32OwnProcess = 0x10;
        private const uint ServiceAutoStart = 2;
        private const uint ServiceErrorNormal = 1;
        private const uint ServiceConfigSidInfo = 5;

        public static void Install(string executable, string config)
        {
            using var manager = OpenSCManager(null, null, ScManagerAllAccess);
            if (manager.IsInvalid) throw new Win32Exception();
            var command = $"\"{executable}\" /service \"{Path.GetFullPath(config)}\"";
            using var service = CreateService(manager, ServiceName, "TNest VPN Tunnel",
                ServiceAllAccess, ServiceWin32OwnProcess, ServiceAutoStart, ServiceErrorNormal,
                command, null, nint.Zero, "Nsi\0TcpIp\0\0", null, null);
            if (service.IsInvalid && Marshal.GetLastWin32Error() != 1073)
                throw new Win32Exception();
            using var current = service.IsInvalid ? OpenService(manager, ServiceName, ServiceAllAccess) : service;
            if (current.IsInvalid) throw new Win32Exception();
            var sid = new SERVICE_SID_INFO { ServiceSidType = 1 };
            if (!ChangeServiceConfig2(current, ServiceConfigSidInfo, ref sid))
                throw new Win32Exception();
            HardenConfig(config);
        }

        private static void HardenConfig(string path)
        {
            var security = new FileSecurity();
            security.SetAccessRuleProtection(true, false);
            security.AddAccessRule(new FileSystemAccessRule(
                new SecurityIdentifier(WellKnownSidType.LocalSystemSid, null),
                FileSystemRights.FullControl, AccessControlType.Allow));
            security.AddAccessRule(new FileSystemAccessRule(
                new SecurityIdentifier(WellKnownSidType.BuiltinAdministratorsSid, null),
                FileSystemRights.FullControl, AccessControlType.Allow));
            security.AddAccessRule(new FileSystemAccessRule(
                new NTAccount("NT SERVICE", ServiceName),
                FileSystemRights.Read, AccessControlType.Allow));
            new FileInfo(path).SetAccessControl(security);
        }

        public static void Remove()
        {
            using var manager = OpenSCManager(null, null, ScManagerAllAccess);
            using var service = OpenService(manager, ServiceName, ServiceAllAccess);
            if (!service.IsInvalid && !DeleteService(service)) throw new Win32Exception();
        }

        [StructLayout(LayoutKind.Sequential)]
        private struct SERVICE_SID_INFO { public uint ServiceSidType; }
        private sealed class ServiceHandle : SafeHandleZeroOrMinusOneIsInvalid {
            private ServiceHandle() : base(true) { }
            protected override bool ReleaseHandle() => CloseServiceHandle(handle);
        }
        [DllImport("advapi32", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern ServiceHandle OpenSCManager(string? machine, string? database, uint access);
        [DllImport("advapi32", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern ServiceHandle CreateService(ServiceHandle manager, string name,
            string displayName, uint access, uint serviceType, uint startType, uint errorControl,
            string binaryPath, string? group, nint tag, string dependencies,
            string? account, string? password);
        [DllImport("advapi32", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern ServiceHandle OpenService(ServiceHandle manager, string name, uint access);
        [DllImport("advapi32", SetLastError = true)]
        private static extern bool ChangeServiceConfig2(ServiceHandle service, uint level,
            ref SERVICE_SID_INFO info);
        [DllImport("advapi32", SetLastError = true)]
        private static extern bool DeleteService(ServiceHandle service);
        [DllImport("advapi32", SetLastError = true)]
        private static extern bool CloseServiceHandle(nint handle);
    }
}
