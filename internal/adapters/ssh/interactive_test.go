package sshclient

import (
	"context"
	"yaare/internal/testutil/sshfixture"
	"testing"
	"time"
)

func TestInteractiveMultiRoundAndOneUse(t *testing.T) {
	server := fixture(t)
	server.Interactive.Store(true)
	run := begin(t, config(server.Address()), store(t))
	host := run.wait(t, "hostkey").Challenge
	run.session.Resolve(host.ID, "once")
	first := run.wait(t, "authentication").Authentication
	if first == nil || len(first.Prompts) != 2 || first.Prompts[0].Echo || first.Prompts[1].Echo {
		t.Fatal("invalid prompts")
	}
	if run.session.AnswerAuthentication("wrong", []string{sshfixture.Password, "123456"}) || run.session.AnswerAuthentication(first.ID, []string{"one"}) {
		t.Fatal("invalid response accepted")
	}
	if !run.session.AnswerAuthentication(first.ID, []string{sshfixture.Password, "123456"}) {
		t.Fatal("valid response rejected")
	}
	if run.session.AnswerAuthentication(first.ID, []string{sshfixture.Password, "123456"}) {
		t.Fatal("replayed response accepted")
	}
	second := run.wait(t, "authentication").Authentication
	if second.ID == first.ID || !second.Prompts[0].Echo {
		t.Fatal("challenge reused")
	}
	run.session.AnswerAuthentication(second.ID, []string{"yes"})
	run.wait(t, "connected")
}
func TestInteractiveCancellationDiscardsPendingAnswers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 1)
	s := NewSession(ctx, store(t), func(e Event) error { events <- e; return nil })
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.askAuthentication("", "", []string{"OTP"}, []bool{false}) }()
	event := <-events
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("authentication did not cancel")
	}
	if s.AnswerAuthentication(event.Authentication.ID, []string{"123456"}) {
		t.Fatal("cancelled challenge accepted")
	}
}
