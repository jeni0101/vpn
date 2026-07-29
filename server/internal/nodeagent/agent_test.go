package nodeagent

import (
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/jeni0101/vpn/server/internal/model"
)

func TestRenderSyncConfigIsDeterministic(t *testing.T) {
	server, _ := wgtypes.GeneratePrivateKey()
	peerA, _ := wgtypes.GeneratePrivateKey()
	peerB, _ := wgtypes.GeneratePrivateKey()
	pskA, _ := wgtypes.GenerateKey()
	pskB, _ := wgtypes.GenerateKey()
	state := model.NodeDesiredState{
		Version: 1, NodeID: "my-kul-01", RegionCode: "MY",
		ExitMode: model.ExitModeIPv4BlockIPv6, ListenPort: 53147,
		Peers: []model.DesiredPeer{
			{DeviceID: "b", PublicKey: peerB.PublicKey().String(),
				PresharedKey: pskB.String(), IPv4: "10.67.0.15", IPv6: "fd67:67:67::15"},
			{DeviceID: "a", PublicKey: peerA.PublicKey().String(),
				PresharedKey: pskA.String(), IPv4: "10.67.0.14", IPv6: "fd67:67:67::14"},
		},
	}
	if err := validateState(state); err != nil {
		t.Fatal(err)
	}
	value := string(renderSyncConfig(server.String(), state))
	if strings.Index(value, peerA.PublicKey().String()) >
		strings.Index(value, peerB.PublicKey().String()) {
		t.Fatal("peers were not rendered in deterministic device order")
	}
	if !strings.Contains(value, "AllowedIPs = 10.67.0.14/32, fd67:67:67::14/128") {
		t.Fatalf("missing safe peer routes: %s", value)
	}
}

func TestValidateStateRejectsDuplicateKeys(t *testing.T) {
	peer, _ := wgtypes.GeneratePrivateKey()
	psk, _ := wgtypes.GenerateKey()
	value := model.DesiredPeer{
		DeviceID: "a", PublicKey: peer.PublicKey().String(),
		PresharedKey: psk.String(), IPv4: "10.67.0.14", IPv6: "fd67:67:67::14",
	}
	state := model.NodeDesiredState{
		Version: 1, NodeID: "my-kul-01", RegionCode: "MY",
		ExitMode: model.ExitModeIPv4BlockIPv6, ListenPort: 53147,
		Peers: []model.DesiredPeer{value, value},
	}
	if err := validateState(state); err == nil {
		t.Fatal("duplicate peer key was accepted")
	}
}
