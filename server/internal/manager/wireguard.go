package manager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/jeni0101/vpn/server/internal/model"
)

type PeerMaterial struct {
	Device       model.Device
	PresharedKey string
}

type WireGuard interface {
	Stats(context.Context) (map[string]model.LiveStats, error)
	Apply(context.Context, []PeerMaterial) error
}

type KernelWireGuard struct {
	Interface       string
	ConfigPath      string
	ApplyLive       bool
	BackupDir       string
	BackupRecipient string
}

func (k *KernelWireGuard) Stats(ctx context.Context) (map[string]model.LiveStats, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	defer client.Close()
	device, err := client.Device(k.Interface)
	if err != nil {
		return nil, err
	}
	result := make(map[string]model.LiveStats, len(device.Peers))
	for _, peer := range device.Peers {
		result[peer.PublicKey.String()] = model.LiveStats{
			PublicKey:     peer.PublicKey.String(),
			ReceiveBytes:  peer.ReceiveBytes,
			TransmitBytes: peer.TransmitBytes,
			LastHandshake: peer.LastHandshakeTime,
		}
	}
	return result, nil
}

func (k *KernelWireGuard) Apply(ctx context.Context, peers []PeerMaterial) error {
	if !k.ApplyLive {
		return errors.New("manager is running in read-only mode")
	}
	peerConfigs := make([]wgtypes.PeerConfig, 0, len(peers))
	for _, peer := range peers {
		publicKey, err := wgtypes.ParseKey(peer.Device.PublicKey)
		if err != nil {
			return fmt.Errorf("invalid peer public key for %s", peer.Device.Name)
		}
		psk, err := wgtypes.ParseKey(peer.PresharedKey)
		if err != nil {
			return fmt.Errorf("invalid preshared key for %s", peer.Device.Name)
		}
		_, ipv4, err := net.ParseCIDR(peer.Device.IPv4 + "/32")
		if err != nil {
			return err
		}
		_, ipv6, err := net.ParseCIDR(peer.Device.IPv6 + "/128")
		if err != nil {
			return err
		}
		peerConfigs = append(peerConfigs, wgtypes.PeerConfig{
			PublicKey:         publicKey,
			PresharedKey:      &psk,
			ReplaceAllowedIPs: true,
			AllowedIPs:        []net.IPNet{*ipv4, *ipv6},
		})
	}

	oldConfig, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return fmt.Errorf("read current WireGuard config: %w", err)
	}
	rendered, err := renderServerConfig(oldConfig, peers)
	if err != nil {
		return err
	}
	if err := k.backup(oldConfig); err != nil {
		return fmt.Errorf("backup WireGuard config: %w", err)
	}

	client, err := wgctrl.New()
	if err != nil {
		return err
	}
	defer client.Close()
	device, err := client.Device(k.Interface)
	if err != nil {
		return err
	}
	rollback := make([]wgtypes.PeerConfig, 0, len(device.Peers))
	for _, peer := range device.Peers {
		peerCopy := peer
		rollback = append(rollback, wgtypes.PeerConfig{
			PublicKey:         peerCopy.PublicKey,
			PresharedKey:      &peerCopy.PresharedKey,
			ReplaceAllowedIPs: true,
			AllowedIPs:        peerCopy.AllowedIPs,
		})
	}
	if err := client.ConfigureDevice(k.Interface, wgtypes.Config{
		ReplacePeers: true,
		Peers:        peerConfigs,
	}); err != nil {
		return fmt.Errorf("configure WireGuard: %w", err)
	}
	if err := atomicWrite(k.ConfigPath, rendered, 0600); err != nil {
		_ = client.ConfigureDevice(k.Interface, wgtypes.Config{
			ReplacePeers: true,
			Peers:        rollback,
		})
		return fmt.Errorf("persist WireGuard config: %w", err)
	}
	return nil
}

func renderServerConfig(current []byte, peers []PeerMaterial) ([]byte, error) {
	text := string(current)
	index := strings.Index(text, "\n# peer:")
	if index < 0 {
		index = strings.Index(text, "\n[Peer]")
	}
	header := strings.TrimSpace(text)
	if index >= 0 {
		header = strings.TrimSpace(text[:index])
	}
	if !strings.Contains(header, "[Interface]") || !strings.Contains(header, "PrivateKey") {
		return nil, errors.New("current WireGuard interface header is invalid")
	}
	var builder strings.Builder
	builder.WriteString(header)
	builder.WriteString("\n\n")
	for _, peer := range peers {
		if strings.ContainsAny(peer.Device.Name, "\r\n#[]") {
			return nil, errors.New("unsafe peer name")
		}
		fmt.Fprintf(&builder, "# peer: %s\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s/32, %s/128\n\n",
			peer.Device.Name, peer.Device.PublicKey, peer.PresharedKey,
			peer.Device.IPv4, peer.Device.IPv6)
	}
	return []byte(strings.TrimSpace(builder.String()) + "\n"), nil
}

func (k *KernelWireGuard) backup(data []byte) error {
	dir := k.BackupDir
	if dir == "" {
		dir = "/var/lib/personal-vpn/prechange"
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	name := filepath.Join(dir, "wg0-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if k.BackupRecipient == "" {
		return errors.New("encrypted pre-change backup recipient is not configured")
	}
	recipient, err := age.ParseX25519Recipient(k.BackupRecipient)
	if err != nil {
		return fmt.Errorf("parse backup recipient: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".wg0-backup-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	writer, err := age.Encrypt(tmp, recipient)
	if err != nil {
		tmp.Close()
		return err
	}
	if _, err := writer.Write(data); err != nil {
		writer.Close()
		tmp.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, name+".conf.age")
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
