package imapdrv

import (
	"context"
	"errors"
	"testing"
	"time"

	"mailbox/internal/sync/mailsync"
)

// A nudge from the server becomes an Event.
func TestWatchLoopReportsNudge(t *testing.T) {
	notify := make(chan struct{}, 1)
	events := make(chan mailsync.Event, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- watchLoop(ctx, notify, make(chan error), events) }()

	notify <- struct{}{}
	select {
	case ev := <-events:
		if ev.Kind != mailsync.EventChanged {
			t.Errorf("kind = %v, want %v", ev.Kind, mailsync.EventChanged)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// The connection under IDLE dying must end the loop, so that the caller
// redials. Left to itself the goroutine used to park on a dead socket forever
// and the Box quietly fell back to the poll.
func TestWatchLoopReturnsWhenIdleEnds(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"with an error", errors.New("broken pipe")},
		{"cleanly", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ended := make(chan error, 1)
			ended <- tc.err

			done := make(chan error, 1)
			go func() {
				done <- watchLoop(context.Background(), make(chan struct{}), ended, make(chan mailsync.Event, 1))
			}()

			select {
			case err := <-done:
				if err == nil {
					t.Fatal("returned nil; the caller would never redial")
				}
				if tc.err != nil && !errors.Is(err, tc.err) {
					t.Errorf("err = %v, want it to wrap %v", err, tc.err)
				}
			case <-time.After(time.Second):
				t.Fatal("watchLoop did not return when IDLE ended")
			}
		})
	}
}
