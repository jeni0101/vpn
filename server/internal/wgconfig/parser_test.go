package wgconfig

import (
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestParseClientAcceptsSafeDualStackConfig(t *testing.T) {
	privateKey, _ := wgtypes.GeneratePrivateKey()
	serverKey, _ := wgtypes.GeneratePrivateKey()
	psk, _ := wgtypes.GenerateKey()
	input := `[Interface]
PrivateKey = ` + privateKey.String() + `
Address = 10.66.0.14/32, fd66:66:66::14/128
DNS = 1.1.1.1, 2606:4700:4700::1111
MTU = 1420

[Peer]
PublicKey = ` + serverKey.PublicKey().String() + `
PresharedKey = ` + psk.String() + `
Endpoint = 203.0.113.10:51999
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`
	config, err := ParseClient(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if config.Endpoint != "203.0.113.10:51999" || config.MTU != 1420 {
		t.Fatalf("unexpected parsed config: %#v", config)
	}
}

func TestParseClientRejectsCommandHooks(t *testing.T) {
	privateKey, _ := wgtypes.GeneratePrivateKey()
	serverKey, _ := wgtypes.GeneratePrivateKey()
	psk, _ := wgtypes.GenerateKey()
	input := `[Interface]
PrivateKey = ` + privateKey.String() + `
Address = 10.66.0.14/32
PostUp = calc.exe
[Peer]
PublicKey = ` + serverKey.PublicKey().String() + `
PresharedKey = ` + psk.String() + `
Endpoint = 203.0.113.10:51999
AllowedIPs = 0.0.0.0/0, ::/0
`
	if _, err := ParseClient(strings.NewReader(input)); err == nil {
		t.Fatal("unsafe PostUp directive was accepted")
	}
}
