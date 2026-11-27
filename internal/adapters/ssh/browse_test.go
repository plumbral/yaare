package sshclient

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"yaare/internal/testutil/sshfixture"
)

type browseBarrier struct {
	entered chan struct{}
	resume  chan struct{}
}

func holdBrowse(ctx context.Context, barrier browseBarrier) {
	close(barrier.entered)
	select {
	case <-ctx.Done():
	case <-barrier.resume:
	}
}

func waitBrowseSignal[Value any](t *testing.T, signal <-chan Value) Value {
	t.Helper()
	select {
	case result := <-signal:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("SFTP fixture signal timed out")
		var zero Value
		return zero
	}
}

type directoryProtocol struct {
	ctx        context.Context
	beforeOpen chan browseBarrier
	beforeList chan browseBarrier
	opened     chan ssh.Channel
	closed     chan struct{}
	listCalls  atomic.Int32
	rejectNext atomic.Bool
}

func (fixture *directoryProtocol) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	server, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer server.Close()
	go ssh.DiscardRequests(requests)
	for request := range channels {
		select {
		case barrier := <-fixture.beforeOpen:
			holdBrowse(fixture.ctx, barrier)
		default:
		}
		if fixture.rejectNext.Swap(false) {
			_ = request.Reject(ssh.ResourceShortage, "fixture session limit")
			continue
		}
		channel, channelRequests, err := request.Accept()
		if err == nil {
			go fixture.serveChannel(channel, channelRequests)
		}
	}
}

func (fixture *directoryProtocol) serveChannel(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "pty-req", "window-change":
			_ = request.Reply(true, nil)
		case "shell":
			_ = request.Reply(true, nil)
			go ssh.DiscardRequests(requests)
			_, _ = io.Copy(channel, channel)
			return
		case "exec":
			_ = request.Reply(true, nil)
			go ssh.DiscardRequests(requests)
			_, _ = io.WriteString(channel, "ok\n")
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: 0}))
			return
		case "subsystem":
			var payload struct{ Name string }
			if ssh.Unmarshal(request.Payload, &payload) != nil || payload.Name != "sftp" {
				_ = request.Reply(false, nil)
				continue
			}
			fixture.opened <- channel
			_ = request.Reply(true, nil)
			go ssh.DiscardRequests(requests)
			handlers := sftp.InMemHandler()
			handlers.FileList = gatedBrowseLister{handlers.FileList, fixture}
			server := sftp.NewRequestServer(channel, handlers)
			_ = server.Serve()
			_ = server.Close()
			fixture.closed <- struct{}{}
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}

type gatedBrowseLister struct {
	sftp.FileLister
	fixture *directoryProtocol
}

func (lister gatedBrowseLister) Filelist(req *sftp.Request) (sftp.ListerAt, error) {
	if req.Method == "List" {
		lister.fixture.listCalls.Add(1)
		select {
		case barrier := <-lister.fixture.beforeList:
			holdBrowse(lister.fixture.ctx, barrier)
		default:
		}
	}
	return lister.FileLister.Filelist(req)
}

func startDirectoryProtocol(t *testing.T) (*directoryProtocol, *runResult) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	signer, err := sshfixture.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if meta.User() != sshfixture.Username || string(password) != sshfixture.Password {
			return nil, errors.New("invalid fixture credentials")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(signer)
	fixture := &directoryProtocol{
		ctx: t.Context(), beforeOpen: make(chan browseBarrier, 1), beforeList: make(chan browseBarrier, 1),
		opened: make(chan ssh.Channel, 16), closed: make(chan struct{}, 16),
	}
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			fixture.serve(conn, serverConfig)
		}
	}()
	run := begin(t, config(listener.Addr().String()), store(t))
	challenge := run.wait(t, "hostkey").Challenge
	if challenge.Fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) || !run.session.Resolve(challenge.ID, "once") {
		t.Fatal("fixture host key was not verified")
	}
	run.wait(t, "connected")
	return fixture, run
}

func listBrowseFixture(t *testing.T, session *Session, path string, idleTimeout time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	resolved, _, err := session.browseDirectory(ctx, path, idleTimeout)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func assertNoExtraBrowseChannel(t *testing.T, fixture *directoryProtocol) {
	t.Helper()
	select {
	case <-fixture.opened:
		t.Fatal("unexpected extra SFTP subsystem")
	default:
	}
}

func TestBrowseProtocolReusesExpiresAndRecovers(t *testing.T) {
	fixture, run := startDirectoryProtocol(t)
	if resolved := listBrowseFixture(t, run.session, ".", browseIdleTimeout); resolved != "/" {
		t.Fatalf("working directory = %q", resolved)
	}
	waitBrowseSignal(t, fixture.opened)
	if _, _, err := run.session.BrowseDirectory(t.Context(), "/no-such-directory"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing directory: %v", err)
	}
	listBrowseFixture(t, run.session, "/", time.Millisecond)
	assertNoExtraBrowseChannel(t, fixture)
	waitBrowseSignal(t, fixture.closed)
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	closedByPeer := waitBrowseSignal(t, fixture.opened)
	if err := closedByPeer.Close(); err != nil {
		t.Fatal(err)
	}
	waitBrowseSignal(t, fixture.closed)
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	waitBrowseSignal(t, fixture.opened)
	assertNoExtraBrowseChannel(t, fixture)
	run.session.Close()
	waitBrowseSignal(t, fixture.closed)
	waitBrowseSignal(t, run.done)
	if _, _, err := run.session.BrowseDirectory(t.Context(), "/"); err == nil {
		t.Fatal("ended attempt reused a browse channel")
	}
}

func TestBrowseProtocolCancellationKeepsOtherChannels(t *testing.T) {
	fixture, run := startDirectoryProtocol(t)
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	waitBrowseSignal(t, fixture.opened)
	barrier := browseBarrier{entered: make(chan struct{}), resume: make(chan struct{})}
	fixture.beforeList <- barrier
	active, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	failed := make(chan error, 1)
	go func() {
		_, _, err := run.session.BrowseDirectory(active, "/")
		failed <- err
	}()
	waitBrowseSignal(t, barrier.entered)
	queued, cancelQueued := context.WithCancel(t.Context())
	cancelQueued()
	if _, _, err := run.session.BrowseDirectory(queued, "/"); !errors.Is(err, context.Canceled) || fixture.listCalls.Load() != 2 {
		t.Fatalf("cancelled waiter reached the server: %v", err)
	}
	transfer, closeTransfer, err := run.session.OpenFiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTransfer()
	waitBrowseSignal(t, fixture.opened)
	cancelActive()
	if err := waitBrowseSignal(t, failed); !errors.Is(err, context.Canceled) {
		t.Fatalf("active request: %v", err)
	}
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	waitBrowseSignal(t, fixture.opened)
	close(barrier.resume)
	if _, err = transfer.ReadDir("/"); err != nil {
		t.Fatalf("browse cancellation broke a separate file channel: %v", err)
	}
	if !run.session.Write([]byte("browsing still has PTY\r")) {
		t.Fatal("browse cancellation broke terminal input")
	}
	eventually(t, func() bool { return strings.Contains(run.text(), "browsing still has PTY") })
}

func TestBrowseProtocolCancelledOpenIsNotReused(t *testing.T) {
	fixture, run := startDirectoryProtocol(t)
	barrier := browseBarrier{entered: make(chan struct{}), resume: make(chan struct{})}
	fixture.beforeOpen <- barrier
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, _, err := run.session.BrowseDirectory(ctx, "/")
		finished <- err
	}()
	waitBrowseSignal(t, barrier.entered)
	cancel()
	if err := waitBrowseSignal(t, finished); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open was not promptly released: %v", err)
	}
	close(barrier.resume)
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	waitBrowseSignal(t, fixture.opened)
	if got := fixture.listCalls.Load(); got != 1 {
		t.Fatalf("cancelled open performed a listing: %d total listings", got)
	}
}

func TestBrowseProtocolYieldsIdleChannelOnQuotaRejection(t *testing.T) {
	fixture, run := startDirectoryProtocol(t)
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	waitBrowseSignal(t, fixture.opened)
	fixture.rejectNext.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	transfer, closeTransfer, err := run.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTransfer()
	waitBrowseSignal(t, fixture.closed)
	waitBrowseSignal(t, fixture.opened)
	if _, err = transfer.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	waitBrowseSignal(t, fixture.opened)
}

func TestBrowseProtocolYieldsIdleChannelForExecOnQuotaRejection(t *testing.T) {
	fixture, run := startDirectoryProtocol(t)
	listBrowseFixture(t, run.session, "/", browseIdleTimeout)
	waitBrowseSignal(t, fixture.opened)
	fixture.rejectNext.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd, err := run.session.OpenCommand(ctx, "echo ok")
	if err != nil {
		t.Fatalf("OpenCommand did not evict idle browse channel on quota rejection: %v", err)
	}
	defer cmd.Close()
	waitBrowseSignal(t, fixture.closed)
	out, err := io.ReadAll(cmd.Output)
	if err != nil || string(out) != "ok\n" {
		t.Fatalf("unexpected exec stdout %q err=%v", string(out), err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("exec wait: %v", err)
	}
}
