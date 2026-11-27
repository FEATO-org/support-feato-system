package interfaces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"nhooyr.io/websocket"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FEATO-org/support-feato-system/config"
	"github.com/FEATO-org/support-feato-system/infrastructure"
	"github.com/FEATO-org/support-feato-system/usecase"
	"github.com/bwmarrin/discordgo"
)

type fakeEEW struct {
	sends, edits int
	last         *discordgo.MessageSend
	edit         *discordgo.MessageEdit
	missing      bool
}

func (f *fakeEEW) ChannelMessageSendComplex(_ string, m *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.sends++
	f.last = m
	return &discordgo.Message{ID: fmt.Sprint(f.sends)}, nil
}
func (f *fakeEEW) ChannelMessageEditComplex(m *discordgo.MessageEdit, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.edits++
	f.edit = m
	if f.missing {
		return nil, &discordgo.RESTError{Response: &http.Response{StatusCode: 404}, Message: &discordgo.APIErrorMessage{Code: 10008}}
	}
	return &discordgo.Message{}, nil
}
func TestEEWLifecycle(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("JST", 9*60*60))
	fixture := `{"id":"one","code":556,"time":"2026/01/02 03:04:05.123","test":true,"issue":{"time":"2026/01/02 03:04:05","eventId":"event","serial":"9"},"cancelled":false,"earthquake":{"hypocenter":{"name":"震源","latitude":44.9,"longitude":142.1,"depth":10.0,"magnitude":5.5}},"areas":[{"name":"地域","scaleFrom":45.0,"scaleTo":50.0,"arrivalTime":null}]}`
	var m eew
	if err := json.Unmarshal([]byte(fixture), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Areas) != 1 || m.Areas[0].ArrivalTime != nil || *m.Earthquake.Hypocenter.Magnitude != 5.5 {
		t.Fatal("fixture decoding failed")
	}
	converted := toEEWEvent(m)
	if converted.Issue.EventID != "event" || !converted.Test || converted.Earthquake == nil || *converted.Earthquake.Hypocenter.Magnitude != 5.5 || len(converted.Areas) != 1 || *converted.Areas[0].ScaleTo != 50 {
		t.Fatal("EEW conversion lost data")
	}
	f := &fakeEEW{}
	processor := usecase.NewEEW(infrastructure.NewEEWDiscordNotifier(f, "channel"))
	run := func() {
		t.Helper()
		if err := processor.Handle(toEEWEvent(m), now); err != nil {
			t.Fatal(err)
		}
	}
	run()
	run()
	if f.sends != 1 || !strings.Contains(f.last.Embeds[0].Title, "訓練・テスト") || !strings.Contains(f.last.Embeds[0].Description, "実際の緊急地震速報ではありません") || len(f.last.AllowedMentions.Parse) != 0 {
		t.Fatal("first or duplicate notification")
	}
	m.ID = "two"
	m.Issue.Serial = "10"
	run()
	if f.edits != 1 || len(f.edit.AllowedMentions.Parse) != 0 {
		t.Fatal("update")
	}
	m.ID = "old"
	m.Issue.Serial = "2"
	run()
	if f.edits != 1 {
		t.Fatal("old serial")
	}
	m.ID = "three"
	m.Issue.Serial = "11"
	f.missing = true
	run()
	if f.sends != 2 {
		t.Fatal("missing message recovery")
	}
	m.ID = "cancel"
	m.Issue.Serial = "12"
	m.Cancelled = true
	m.Earthquake = nil
	run()
	run()
	if f.sends != 3 {
		t.Fatal("cancellation")
	}
	m.ID = "late"
	m.Issue.Serial = "13"
	m.Cancelled = false
	run()
	if f.edits != 2 {
		t.Fatal("update after cancellation")
	}
	if err := processor.Handle(toEEWEvent(m), now.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestReadSkipsOtherCodes(t *testing.T) {
	now := time.Now().In(time.FixedZone("JST", 9*60*60))
	stamp := now.Format("2006/01/02 15:04:05")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		c.Write(r.Context(), websocket.MessageText, []byte(`{"code":551}`))
		c.Write(r.Context(), websocket.MessageText, []byte(fmt.Sprintf(`{"id":"x","code":556,"time":%q,"issue":{"time":%q,"eventId":"e","serial":"1"}}`, stamp, stamp)))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	f := &fakeEEW{}
	p := NewP2PEarthquakeInterfaces(config.P2PEarthquake{}, ctx, usecase.NewEEW(infrastructure.NewEEWDiscordNotifier(f, "channel"))).(*p2pEarthquakeInterfaces)
	_ = p.read(ctx, c)
	if f.sends != 1 {
		t.Fatalf("got %d notifications", f.sends)
	}
}
func TestReconnectAndCancel(t *testing.T) {
	var count int32
	connected := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		if atomic.AddInt32(&count, 1) >= 2 {
			select {
			case connected <- struct{}{}:
			default:
				{
				}
			}
		}
		if atomic.LoadInt32(&count) >= 2 {
			_, _, _ = c.Read(r.Context())
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	p := NewP2PEarthquakeInterfaces(config.P2PEarthquake{WebSocketURL: "ws" + strings.TrimPrefix(server.URL, "http")}, ctx, nil)
	done := make(chan struct{})
	go func() { p.ReceiveEEW(); close(done) }()
	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("did not reconnect")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("did not stop")
	}
}
