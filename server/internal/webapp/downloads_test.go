package webapp

import (
	"testing"
	"time"
)

func TestDownloadIsOneTimeAndExpires(t *testing.T) {
	cache := newDownloadCache()
	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	token, _, err := cache.Put("test.conf", "text/plain", []byte("secret"), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	item, err := cache.Take(token)
	if err != nil || string(item.Data) != "secret" {
		t.Fatalf("first take failed: %v", err)
	}
	if _, err := cache.Take(token); err == nil {
		t.Fatal("download token was accepted twice")
	}
	token, _, _ = cache.Put("expired.conf", "text/plain", []byte("secret"), time.Minute)
	now = now.Add(2 * time.Minute)
	if _, err := cache.Take(token); err == nil {
		t.Fatal("expired download was returned")
	}
}
