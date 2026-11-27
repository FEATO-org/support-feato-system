package interfaces

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/FEATO-org/support-feato-system/config"
	"github.com/FEATO-org/support-feato-system/usecase"
	"nhooyr.io/websocket"
)

type P2PEarthquakeInterfaces interface {
	ReceiveEEW() error
}
type eew struct {
	ID        string `json:"id"`
	Code      int    `json:"code"`
	Time      string `json:"time"`
	Test      bool   `json:"test"`
	Cancelled bool   `json:"cancelled"`
	Issue     struct {
		Time    string `json:"time"`
		EventID string `json:"eventId"`
		Serial  string `json:"serial"`
	} `json:"issue"`
	Earthquake *struct {
		Hypocenter struct {
			Name      string   `json:"name"`
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
			Depth     *float64 `json:"depth"`
			Magnitude *float64 `json:"magnitude"`
		} `json:"hypocenter"`
	} `json:"earthquake"`
	Areas []struct {
		Name        string   `json:"name"`
		ScaleFrom   *float64 `json:"scaleFrom"`
		ScaleTo     *float64 `json:"scaleTo"`
		ArrivalTime *string  `json:"arrivalTime"`
	} `json:"areas"`
}
type p2pEarthquakeInterfaces struct {
	config    config.P2PEarthquake
	ctx       context.Context
	processor *usecase.EEW
}

func NewP2PEarthquakeInterfaces(c config.P2PEarthquake, ctx context.Context, processor *usecase.EEW) P2PEarthquakeInterfaces {
	return &p2pEarthquakeInterfaces{config: c, ctx: ctx, processor: processor}
}
func (p *p2pEarthquakeInterfaces) ReceiveEEW() error {
	delay := time.Second
	for p.ctx.Err() == nil {
		conn, _, err := websocket.Dial(p.ctx, p.config.WebSocketURL, &websocket.DialOptions{HTTPClient: p.config.Client})
		if err == nil {
			delay = time.Second
			err = p.read(p.ctx, conn)
			conn.Close(websocket.StatusNormalClosure, "")
		}
		if p.ctx.Err() != nil {
			return nil
		}
		log.Printf("EEW WebSocket disconnected: %v", err)
		timer := time.NewTimer(delay)
		select {
		case <-p.ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
	return nil
}
func (p *p2pEarthquakeInterfaces) read(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var header struct {
			Code int `json:"code"`
		}
		if err = json.Unmarshal(data, &header); err != nil {
			log.Printf("Invalid WebSocket message: %v", err)
			continue
		}
		if header.Code != 556 {
			continue
		}
		var m eew
		if err = json.Unmarshal(data, &m); err != nil {
			log.Printf("Invalid EEW message: %v", err)
			continue
		}
		if err = p.processor.Handle(toEEWEvent(m), time.Now()); err != nil {
			log.Printf("EEW processing failed: %v", err)
		}
	}
}

func toEEWEvent(m eew) usecase.EEWEvent {
	out := usecase.EEWEvent{ID: m.ID, Time: m.Time, Test: m.Test, Cancelled: m.Cancelled, Issue: usecase.EEWIssue{Time: m.Issue.Time, EventID: m.Issue.EventID, Serial: m.Issue.Serial}}
	if m.Earthquake != nil {
		h := m.Earthquake.Hypocenter
		out.Earthquake = &usecase.EEWEarthquake{Hypocenter: usecase.EEWHypocenter{Name: h.Name, Latitude: h.Latitude, Longitude: h.Longitude, Depth: h.Depth, Magnitude: h.Magnitude}}
	}
	for _, a := range m.Areas {
		out.Areas = append(out.Areas, usecase.EEWArea{Name: a.Name, ScaleFrom: a.ScaleFrom, ScaleTo: a.ScaleTo, ArrivalTime: a.ArrivalTime})
	}
	return out
}
