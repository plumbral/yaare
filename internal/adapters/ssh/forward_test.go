package sshclient

import (
	"context"
	"net"
	"testing"
	"time"
)

type delayedForwardListener struct {
	net.Listener
	closing chan struct{}
	release chan struct{}
}

func (l *delayedForwardListener) Close() error {
	close(l.closing)
	<-l.release
	return nil
}

func TestRemoteForwardOwnsLateListenAndClose(t *testing.T) {
	for _, late := range []bool{false, true} {
		name := "running"
		if late {
			name = "late-listen"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			gate := make(chan struct{}, 1)
			gate <- struct{}{}
			ready := make(chan remoteForwardResult)
			allowListen := make(chan struct{})
			listener := &delayedForwardListener{closing: make(chan struct{}), release: make(chan struct{})}
			defer close(listener.release)
			done := make(chan struct{})
			go func() {
				defer close(done)
				ownRemoteForward(ctx, gate, ready, func() (net.Listener, error) {
					<-allowListen
					return listener, nil
				})
			}()
			if late {
				cancel()
			}
			close(allowListen)
			if !late {
				select {
				case result := <-ready:
					if result.listener != listener || result.err != nil {
						t.Fatal("listener not published")
					}
				case <-time.After(time.Second):
					t.Fatal("listener did not start")
				}
				cancel()
			}
			select {
			case <-listener.closing:
			case <-time.After(time.Second):
				t.Fatal("remote close did not start")
			}
			select {
			case gate <- struct{}{}:
				t.Fatal("another request could start while close was outstanding")
			default:
			}
			t.Cleanup(func() {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("listener owner did not exit")
				}
				if len(gate) != 0 {
					t.Error("listener owner did not release gate")
				}
			})
		})
	}
}
