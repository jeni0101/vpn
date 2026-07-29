$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$artifacts = Join-Path $root "artifacts"
$native = Join-Path $artifacts "wireguard-windows"
$commit = "4e6726c23ae9c5cb58e0c9910f3b7515621d133d"

if (-not (Test-Path $native)) {
    git clone --filter=blob:none https://git.zx2c4.com/wireguard-windows $native
}
git -C $native fetch origin $commit
git -C $native checkout --detach $commit
if ((git -C $native rev-parse HEAD).Trim() -ne $commit) {
    throw "WireGuard Windows source pin mismatch"
}

& (Join-Path $native "embeddable-dll-service\build.bat")
$publish = Join-Path $artifacts "publish"
dotnet publish (Join-Path $root "src\TNestVPN\TNestVPN.csproj") `
    -c Release -r win-x64 --self-contained true -o $publish
dotnet publish (Join-Path $root "src\TNestVPN.ServiceHost\TNestVPN.ServiceHost.csproj") `
    -c Release -r win-x64 --self-contained true -o $publish
Copy-Item (Join-Path $native "embeddable-dll-service\amd64\tunnel.dll") $publish
$wireguardDll = Get-ChildItem $native -Recurse -Filter wireguard.dll |
    Where-Object FullName -Match "amd64" | Select-Object -First 1
if (-not $wireguardDll) { throw "Pinned build did not provide amd64 wireguard.dll" }
Copy-Item $wireguardDll.FullName $publish

dotnet build (Join-Path $root "installer\TNestVPN.Installer.wixproj") -c Release
Get-FileHash (Join-Path $publish "*") -Algorithm SHA256 |
    Format-Table -HideTableHeaders Path, Hash |
    Out-File (Join-Path $artifacts "SHA256SUMS.txt") -Encoding utf8
