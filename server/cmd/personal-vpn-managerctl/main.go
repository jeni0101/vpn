package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/jeni0101/vpn/server/internal/manager"
	"github.com/jeni0101/vpn/server/internal/managerclient"
)

func main() {
	socket := os.Getenv("TNEST_MANAGER_SOCKET")
	if socket == "" {
		socket = "/run/personal-vpn/manager.sock"
	}
	client := managerclient.New(socket)
	command := "health"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch command {
	case "health":
		fatal(client.Health(ctx))
		fmt.Println("ok")
	case "devices":
		devices, err := client.Devices(ctx)
		fatal(err)
		fatal(json.NewEncoder(os.Stdout).Encode(map[string]any{"devices": devices}))
	case "import":
		flags := flag.NewFlagSet("import", flag.ExitOnError)
		file := flags.String("file", "-", "JSON import file or - for stdin")
		_ = flags.Parse(os.Args[2:])
		reader := io.Reader(os.Stdin)
		if *file != "-" {
			handle, err := os.Open(*file)
			fatal(err)
			defer handle.Close()
			reader = handle
		}
		decoder := json.NewDecoder(io.LimitReader(reader, 1024*1024))
		decoder.DisallowUnknownFields()
		var request manager.ImportRequest
		fatal(decoder.Decode(&request))
		fatal(client.Import(ctx, request))
		fmt.Printf("imported %d devices\n", len(request.Devices))
	case "verify-import":
		request := readImport(os.Stdin)
		fatal(verifyImport(request))
		fmt.Printf("verified %d live peers\n", len(request.Devices))
	default:
		fmt.Fprintln(os.Stderr, "usage: personal-vpn-managerctl [health|devices|verify-import|import --file <path>]")
		os.Exit(2)
	}
}

func readImport(reader io.Reader) manager.ImportRequest {
	decoder := json.NewDecoder(io.LimitReader(reader, 1024*1024))
	decoder.DisallowUnknownFields()
	var request manager.ImportRequest
	fatal(decoder.Decode(&request))
	return request
}

func verifyImport(request manager.ImportRequest) error {
	client, err := wgctrl.New()
	if err != nil {
		return err
	}
	defer client.Close()
	device, err := client.Device("wg0")
	if err != nil {
		return err
	}
	if len(request.Devices) != len(device.Peers) {
		return fmt.Errorf("import has %d peers but wg0 has %d", len(request.Devices), len(device.Peers))
	}
	live := make(map[string]struct {
		psk     string
		allowed []string
	}, len(device.Peers))
	for _, peer := range device.Peers {
		allowed := make([]string, 0, len(peer.AllowedIPs))
		for _, prefix := range peer.AllowedIPs {
			allowed = append(allowed, prefix.String())
		}
		sort.Strings(allowed)
		live[peer.PublicKey.String()] = struct {
			psk     string
			allowed []string
		}{peer.PresharedKey.String(), allowed}
	}
	for _, item := range request.Devices {
		peer, ok := live[item.PublicKey]
		if !ok {
			return fmt.Errorf("peer %s public key is not active", item.Name)
		}
		if peer.psk != item.PresharedKey {
			return fmt.Errorf("peer %s preshared key does not match", item.Name)
		}
		expected := []string{item.IPv4 + "/32", item.IPv6 + "/128"}
		sort.Strings(expected)
		if fmt.Sprint(peer.allowed) != fmt.Sprint(expected) {
			return fmt.Errorf("peer %s AllowedIPs do not match", item.Name)
		}
	}
	return nil
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
