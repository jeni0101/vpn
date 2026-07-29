package auth

import (
	"context"
	"testing"

	"github.com/jeni0101/vpn/server/internal/security"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password was rejected")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password was accepted")
	}
}

func TestAdminInitializeAndRecoveryCodeIsOneTime(t *testing.T) {
	sealer, err := security.NewSealer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.TempDir()+"/auth.db", sealer)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, err := store.Initialize(context.Background(), "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RecoveryCodes) != 10 || result.ProvisionURI == "" {
		t.Fatalf("unexpected initialization result: %#v", result)
	}
	if !store.CheckPassword(context.Background(), "admin", "correct horse battery staple") {
		t.Fatal("initialized password was rejected")
	}
	code := result.RecoveryCodes[0]
	if !store.UseRecoveryCode(context.Background(), code) {
		t.Fatal("valid recovery code was rejected")
	}
	if store.UseRecoveryCode(context.Background(), code) {
		t.Fatal("recovery code was accepted twice")
	}
}
