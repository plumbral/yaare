package sshclient

import (
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestKeepaliveDelayedReplyWhileReceivingAndSilentDisconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := net.Pipe()
		defer peer.Close()
		transport := &sessionTransport{Conn: client}
		var requests atomic.Int32
		release := make(chan struct{})
		defer close(release)
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			transport.watchIdle(t.Context(), time.Second, func() error {
				requests.Add(1)
				<-release
				return nil
			})
		}()
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		for i := 0; i < 6; i++ {
			go peer.Write([]byte{1})
			var b [1]byte
			if _, err := transport.Read(b[:]); err != nil {
				t.Fatal("closed while receiving", err)
			}
			time.Sleep(time.Second)
			synctest.Wait()
		}
		if requests.Load() != 1 {
			t.Fatalf("expected one pending probe, got %d", requests.Load())
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		select {
		case <-stopped:
		default:
			t.Fatal("silent transport remained open")
		}
		if _, err := peer.Write([]byte{1}); err == nil {
			t.Fatal("underlying transport remained open")
		}
	})
}
