package cloudsync

import (
	"bytes"
	"sync"
)

type cachedKey struct {
	salt    [16]byte
	profile byte
	key     []byte
}

type KeyCache struct {
	mu      sync.Mutex
	entries [4]cachedKey
	next    int
	epoch   string
}

func (c *KeyCache) clearLocked() {
	for i := range c.entries {
		clear(c.entries[i].key)
		c.entries[i] = cachedKey{}
	}
	c.next = 0
	c.epoch = ""
}

func (c *KeyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearLocked()
}

func (c *KeyCache) Bind(epoch string) *KeyCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != epoch {
		c.clearLocked()
		c.epoch = epoch
	}
	if epoch == "" {
		return nil
	}
	return c
}

func (c *KeyCache) key(pass []byte, profile byte, salt [16]byte) []byte {
	if c == nil {
		return derive(pass, profile, salt[:])
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch == "" {
		return derive(pass, profile, salt[:])
	}
	for i := range c.entries {
		e := &c.entries[i]
		if e.profile == profile && e.salt == salt {
			return bytes.Clone(e.key)
		}
	}
	key := derive(pass, profile, salt[:])
	clear(c.entries[c.next].key)
	c.entries[c.next] = cachedKey{salt: salt, profile: profile, key: bytes.Clone(key)}
	c.next = (c.next + 1) % len(c.entries)
	return key
}
