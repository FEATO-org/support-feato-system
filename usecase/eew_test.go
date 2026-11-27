package usecase

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeNotifier struct {
	sends, edits     int
	sendErr, editErr error
}

func (f *fakeNotifier) Send(_ EEWEvent, _ string, _ time.Time) (EEWMessageRef, error) {
	f.sends++
	return EEWMessageRef{ID: fmt.Sprint(f.sends), Channel: "c"}, f.sendErr
}
func (f *fakeNotifier) Edit(_ EEWMessageRef, _ EEWEvent, _ string, _ time.Time) error {
	f.edits++
	return f.editErr
}
func eventAt(now time.Time) EEWEvent {
	stamp := now.In(japanTime).Format("2006/01/02 15:04:05")
	return EEWEvent{ID: "one", Time: stamp, Issue: EEWIssue{EventID: "e", Serial: "1", Time: stamp}}
}
func TestEEWSerializesDuplicateEvents(t *testing.T) {
	now := time.Now()
	f := &fakeNotifier{}
	p := NewEEW(f)
	m := eventAt(now)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Handle(m, now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.sends != 1 {
		t.Fatalf("sends = %d", f.sends)
	}
}
func TestEEWFailedSendAndExpiry(t *testing.T) {
	now := time.Now()
	failure := errors.New("send")
	f := &fakeNotifier{sendErr: failure}
	p := NewEEW(f)
	m := eventAt(now)
	if err := p.Handle(m, now); !errors.Is(err, failure) {
		t.Fatalf("got %v", err)
	}
	f.sendErr = nil
	if err := p.Handle(m, now); err != nil || f.sends != 1 {
		t.Fatalf("duplicate send: %d, %v", f.sends, err)
	}
	later := now.Add(25 * time.Hour)
	m = eventAt(later)
	if err := p.Handle(m, later); err != nil || f.sends != 2 {
		t.Fatalf("expired attempt: %d, %v", f.sends, err)
	}
}
func TestEEWMissingEditFallsBack(t *testing.T) {
	now := time.Now()
	f := &fakeNotifier{}
	p := NewEEW(f)
	m := eventAt(now)
	if err := p.Handle(m, now); err != nil {
		t.Fatal(err)
	}
	m.ID = "two"
	m.Issue.Serial = "2"
	f.editErr = ErrEEWMessageNotFound
	if err := p.Handle(m, now); err != nil || f.sends != 2 || f.edits != 1 {
		t.Fatalf("sends=%d edits=%d err=%v", f.sends, f.edits, err)
	}
}
