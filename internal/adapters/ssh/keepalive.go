package sshclient

import (
	"context"
	"net"
	"sync/atomic"
	"time"
)

type sessionTransport struct {
	net.Conn
	received atomic.Uint64
}

func (c *sessionTransport) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.received.Add(uint64(n))
	}
	return n, err
}

func (c *sessionTransport) watchIdle(ctx context.Context, interval time.Duration, probe func() error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var reply chan error
	previous := c.received.Load()
	missed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-reply:
			if err != nil {
				c.Close()
				return
			}
			reply = nil
			missed = 0
		case <-ticker.C:
			current := c.received.Load()
			if current != previous {
				previous = current
				missed = 0
				continue
			}
			if reply == nil {
				reply = make(chan error, 1)
				go func(done chan<- error) { done <- probe() }(reply)
				continue
			}
			missed++
			if missed >= 3 {
				c.Close()
				return
			}
		}
	}
}
