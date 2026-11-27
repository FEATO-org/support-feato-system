package usecase

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

const (
	eventTTL           = 24 * time.Hour
	warningMaxAge      = 5 * time.Minute
	cancellationMaxAge = 24 * time.Hour
	futureTolerance    = time.Minute
)

var japanTime = time.FixedZone("JST", 9*60*60)

type EEWEvent struct {
	ID, Time        string
	Test, Cancelled bool
	Issue           EEWIssue
	Earthquake      *EEWEarthquake
	Areas           []EEWArea
}
type EEWIssue struct{ Time, EventID, Serial string }
type EEWEarthquake struct{ Hypocenter EEWHypocenter }
type EEWHypocenter struct {
	Name                                  string
	Latitude, Longitude, Depth, Magnitude *float64
}
type EEWArea struct {
	Name               string
	ScaleFrom, ScaleTo *float64
	ArrivalTime        *string
}
type EEWMessageRef struct{ ID, Channel string }
type EEWNotifier interface {
	Send(EEWEvent, string, time.Time) (EEWMessageRef, error)
	Edit(EEWMessageRef, EEWEvent, string, time.Time) error
}

var ErrEEWMessageNotFound = errors.New("EEW message not found")

type eventState struct {
	ref       EEWMessageRef
	serial    *big.Int
	cancelled bool
	updated   time.Time
	ids       map[string]bool
}
type EEW struct {
	mu       sync.Mutex
	notifier EEWNotifier
	events   map[string]*eventState
	attempts map[string]time.Time
}

func NewEEW(notifier EEWNotifier) *EEW {
	return &EEW{notifier: notifier, events: make(map[string]*eventState), attempts: make(map[string]time.Time)}
}
func (p *EEW) Handle(m EEWEvent, now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range p.events {
		if now.Sub(v.updated) >= eventTTL {
			delete(p.events, k)
		}
	}
	for k, t := range p.attempts {
		if now.Sub(t) >= eventTTL {
			delete(p.attempts, k)
		}
	}
	if m.ID == "" || m.Issue.EventID == "" {
		return errors.New("missing EEW ID or event ID")
	}
	serial, ok := new(big.Int).SetString(m.Issue.Serial, 10)
	if !ok || serial.Sign() < 0 {
		return errors.New("invalid EEW serial")
	}
	issue, err := time.ParseInLocation("2006/01/02 15:04:05", m.Issue.Time, japanTime)
	if err != nil {
		return fmt.Errorf("invalid issue time: %w", err)
	}
	received, err := time.ParseInLocation("2006/01/02 15:04:05", m.Time, japanTime)
	if err != nil {
		return fmt.Errorf("invalid receive time: %w", err)
	}
	maxAge := warningMaxAge
	if m.Cancelled {
		maxAge = cancellationMaxAge
	}
	if now.Sub(issue) > maxAge || now.Sub(received) > maxAge || issue.Sub(now) > futureTolerance || received.Sub(now) > futureTolerance {
		return nil
	}
	key := fmt.Sprintf("%t:%s", m.Test, m.Issue.EventID)
	state := p.events[key]
	attemptKey := key + ":" + m.ID
	if _, ok := p.attempts[attemptKey]; ok {
		return nil
	}
	if state != nil && (state.ids[m.ID] || state.cancelled || (!m.Cancelled && serial.Cmp(state.serial) <= 0)) {
		return nil
	}
	kind := "初報"
	if state != nil {
		kind = "続報"
	}
	if m.Cancelled {
		kind = "取消報"
	}
	if state == nil || m.Cancelled {
		p.attempts[attemptKey] = now
		ref, err := p.notifier.Send(m, kind, issue)
		if err != nil {
			return err
		}
		if state == nil {
			state = &eventState{ids: make(map[string]bool)}
			p.events[key] = state
		}
		if !m.Cancelled {
			state.ref = ref
		}
	} else {
		err := p.notifier.Edit(state.ref, m, kind, issue)
		if errors.Is(err, ErrEEWMessageNotFound) {
			p.attempts[attemptKey] = now
			ref, sendErr := p.notifier.Send(m, kind, issue)
			if sendErr != nil {
				return sendErr
			}
			state.ref = ref
			err = nil
		}
		if err != nil {
			return err
		}
	}

	state.serial = serial
	state.cancelled = m.Cancelled
	state.updated = now
	state.ids[m.ID] = true
	return nil
}
