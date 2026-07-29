package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
)

type envelope struct {
	Version   int             `json:"version"`
	Payload   json.RawMessage `json:"payload"`
	PublicKey string          `json:"public_key"`
	Signature string          `json:"signature"`
}

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: personal-vpn-release-tool keygen|sign|verify"))
	}
	switch os.Args[1] {
	case "keygen":
		keygen(os.Args[2:])
	case "sign":
		sign(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	default:
		fatal(errors.New("unknown command"))
	}
}

func keygen(args []string) {
	flags := flag.NewFlagSet("keygen", flag.ExitOnError)
	privatePath := flags.String("private", "", "offline private key output")
	publicPath := flags.String("public", "", "public key output")
	_ = flags.Parse(args)
	if *privatePath == "" || *publicPath == "" {
		fatal(errors.New("--private and --public are required"))
	}
	if _, err := os.Stat(*privatePath); !errors.Is(err, os.ErrNotExist) {
		fatal(errors.New("private key output already exists"))
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	fatal(err)
	fatal(os.WriteFile(*privatePath,
		[]byte(base64.RawStdEncoding.EncodeToString(privateKey)), 0600))
	fatal(os.WriteFile(*publicPath,
		[]byte(base64.RawStdEncoding.EncodeToString(publicKey)+"\n"), 0644))
	fmt.Println("generated Ed25519 release key; move the private file offline")
}

func sign(args []string) {
	flags := flag.NewFlagSet("sign", flag.ExitOnError)
	privatePath := flags.String("private", "", "offline private key")
	payloadPath := flags.String("payload", "", "release payload JSON")
	outputPath := flags.String("out", "", "signed envelope output")
	_ = flags.Parse(args)
	privateKey := readKey(*privatePath, ed25519.PrivateKeySize)
	payload, err := os.ReadFile(*payloadPath)
	fatal(err)
	var compact bytes.Buffer
	fatal(json.Compact(&compact, payload))
	var value any
	fatal(json.Unmarshal(compact.Bytes(), &value))
	publicKey := privateKey[ed25519.PrivateKeySize-ed25519.PublicKeySize:]
	result := envelope{
		Version: 1, Payload: compact.Bytes(),
		PublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
		Signature: base64.RawStdEncoding.EncodeToString(
			ed25519.Sign(ed25519.PrivateKey(privateKey), compact.Bytes())),
	}
	data, err := json.MarshalIndent(result, "", "  ")
	fatal(err)
	data = append(data, '\n')
	fatal(os.WriteFile(*outputPath, data, 0644))
}

func verify(args []string) {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	publicPath := flags.String("public", "", "trusted public key")
	inputPath := flags.String("file", "", "signed release envelope")
	_ = flags.Parse(args)
	publicKey := readKey(*publicPath, ed25519.PublicKeySize)
	data, err := os.ReadFile(*inputPath)
	fatal(err)
	var value envelope
	fatal(json.Unmarshal(data, &value))
	signature, err := base64.RawStdEncoding.DecodeString(value.Signature)
	fatal(err)
	var compact bytes.Buffer
	fatal(json.Compact(&compact, value.Payload))
	if value.Version != 1 || !ed25519.Verify(ed25519.PublicKey(publicKey), compact.Bytes(), signature) {
		fatal(errors.New("release manifest signature is invalid"))
	}
	fmt.Println("release manifest signature is valid")
}

func readKey(path string, size int) []byte {
	if path == "" {
		fatal(errors.New("key path is required"))
	}
	data, err := os.ReadFile(path)
	fatal(err)
	key, err := base64.RawStdEncoding.DecodeString(string(bytes.TrimSpace(data)))
	fatal(err)
	if len(key) != size {
		fatal(errors.New("invalid key length"))
	}
	return key
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
