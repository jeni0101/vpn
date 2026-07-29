package webapp

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

type download struct {
	Name      string
	MediaType string
	Data      []byte
	ExpiresAt time.Time
}

type downloadCache struct {
	mu    sync.Mutex
	items map[string]download
	now   func() time.Time
}

func newDownloadCache() *downloadCache {
	return &downloadCache{items: make(map[string]download), now: time.Now}
}

func (c *downloadCache) Put(name, mediaType string, data []byte, ttl time.Duration) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := c.now().UTC().Add(ttl)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	c.items[token] = download{
		Name: name, MediaType: mediaType,
		Data: append([]byte(nil), data...), ExpiresAt: expires,
	}
	return token, expires, nil
}

func (c *downloadCache) Take(token string) (download, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	item, ok := c.items[token]
	if !ok {
		return download{}, errors.New("download expired or already used")
	}
	delete(c.items, token)
	return item, nil
}

func (c *downloadCache) pruneLocked() {
	now := c.now().UTC()
	for token, item := range c.items {
		if !item.ExpiresAt.After(now) {
			for i := range item.Data {
				item.Data[i] = 0
			}
			delete(c.items, token)
		}
	}
}
