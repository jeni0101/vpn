package wgconfig

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var allowedInterface = map[string]bool{
	"privatekey": true, "address": true, "dns": true, "mtu": true,
}

var allowedPeer = map[string]bool{
	"publickey": true, "presharedkey": true, "endpoint": true,
	"allowedips": true, "persistentkeepalive": true,
}

type ClientConfig struct {
	PrivateKey          string
	Addresses           []string
	DNS                 []string
	MTU                 int
	ServerPublicKey     string
	PresharedKey        string
	Endpoint            string
	AllowedIPs          []string
	PersistentKeepalive int
}

func ParseClient(r io.Reader) (ClientConfig, error) {
	var cfg ClientConfig
	section := ""
	seenPeer := false
	scanner := bufio.NewScanner(io.LimitReader(r, 64*1024))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "peer" {
				if seenPeer {
					return ClientConfig{}, errors.New("exactly one peer is supported")
				}
				seenPeer = true
			} else if section != "interface" {
				return ClientConfig{}, fmt.Errorf("unsupported section %q", section)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			return ClientConfig{}, fmt.Errorf("invalid configuration line")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch section {
		case "interface":
			if !allowedInterface[key] {
				return ClientConfig{}, fmt.Errorf("unsafe or unsupported interface field %q", key)
			}
			if err := parseInterfaceField(&cfg, key, value); err != nil {
				return ClientConfig{}, err
			}
		case "peer":
			if !allowedPeer[key] {
				return ClientConfig{}, fmt.Errorf("unsafe or unsupported peer field %q", key)
			}
			if err := parsePeerField(&cfg, key, value); err != nil {
				return ClientConfig{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ClientConfig{}, err
	}
	if !seenPeer || cfg.PrivateKey == "" || cfg.ServerPublicKey == "" ||
		cfg.PresharedKey == "" || cfg.Endpoint == "" {
		return ClientConfig{}, errors.New("configuration is incomplete")
	}
	if len(cfg.AllowedIPs) != 2 || !contains(cfg.AllowedIPs, "0.0.0.0/0") ||
		!contains(cfg.AllowedIPs, "::/0") {
		return ClientConfig{}, errors.New("TNest VPN requires full dual-stack routes")
	}
	return cfg, nil
}

func parseInterfaceField(cfg *ClientConfig, key, value string) error {
	switch key {
	case "privatekey":
		if _, err := wgtypes.ParseKey(value); err != nil {
			return errors.New("invalid private key")
		}
		cfg.PrivateKey = value
	case "address":
		cfg.Addresses = splitList(value)
		for _, address := range cfg.Addresses {
			if _, _, err := net.ParseCIDR(address); err != nil {
				return fmt.Errorf("invalid address %q", address)
			}
		}
	case "dns":
		cfg.DNS = splitList(value)
		for _, server := range cfg.DNS {
			if net.ParseIP(server) == nil {
				return fmt.Errorf("invalid DNS server %q", server)
			}
		}
	case "mtu":
		mtu, err := strconv.Atoi(value)
		if err != nil || mtu < 1280 || mtu > 1500 {
			return errors.New("MTU must be between 1280 and 1500")
		}
		cfg.MTU = mtu
	}
	return nil
}

func parsePeerField(cfg *ClientConfig, key, value string) error {
	switch key {
	case "publickey":
		if _, err := wgtypes.ParseKey(value); err != nil {
			return errors.New("invalid server public key")
		}
		cfg.ServerPublicKey = value
	case "presharedkey":
		if _, err := wgtypes.ParseKey(value); err != nil {
			return errors.New("invalid preshared key")
		}
		cfg.PresharedKey = value
	case "endpoint":
		if _, _, err := net.SplitHostPort(value); err != nil {
			return errors.New("invalid endpoint")
		}
		cfg.Endpoint = value
	case "allowedips":
		cfg.AllowedIPs = splitList(value)
		for _, prefix := range cfg.AllowedIPs {
			if _, _, err := net.ParseCIDR(prefix); err != nil {
				return fmt.Errorf("invalid allowed IP %q", prefix)
			}
		}
	case "persistentkeepalive":
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 0 || seconds > 65535 {
			return errors.New("invalid keepalive")
		}
		cfg.PersistentKeepalive = seconds
	}
	return nil
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		result = append(result, strings.TrimSpace(part))
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
