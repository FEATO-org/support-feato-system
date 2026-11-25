package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/FEATO-org/support-feato-system/config"
	"github.com/bwmarrin/discordgo"
	"nhooyr.io/websocket"
)

const (
	eventTTL           = 24 * time.Hour
	warningMaxAge      = 5 * time.Minute
	cancellationMaxAge = 24 * time.Hour
	futureTolerance    = time.Minute
)

var japanTime = time.FixedZone("JST", 9*60*60)

type P2PEarthquakeInterfaces interface {
	ReceiveEEWToDiscord(*discordgo.Session) error
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
type eventState struct {
	messageID, channelID string
	serial               *big.Int
	cancelled            bool
	updated              time.Time
	ids                  map[string]bool
}
type eewSender interface {
	ChannelMessageSendComplex(string, *discordgo.MessageSend, ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessageEditComplex(*discordgo.MessageEdit, ...discordgo.RequestOption) (*discordgo.Message, error)
}
type p2pEarthquakeInterfaces struct {
	config   config.P2PEarthquake
	ctx      context.Context
	discord  config.DiscordConfig
	events   map[string]*eventState
	attempts map[string]time.Time
}

func NewP2PEarthquakeInterfaces(c config.P2PEarthquake, ctx context.Context, d config.DiscordConfig) P2PEarthquakeInterfaces {
	return &p2pEarthquakeInterfaces{config: c, ctx: ctx, discord: d, events: make(map[string]*eventState), attempts: make(map[string]time.Time)}
}
func (p *p2pEarthquakeInterfaces) ReceiveEEWToDiscord(s *discordgo.Session) error {
	delay := time.Second
	for p.ctx.Err() == nil {
		conn, _, err := websocket.Dial(p.ctx, p.config.WebSocketURL, &websocket.DialOptions{HTTPClient: p.config.Client})
		if err == nil {
			delay = time.Second
			err = p.read(p.ctx, conn, s)
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
func (p *p2pEarthquakeInterfaces) read(ctx context.Context, conn *websocket.Conn, s eewSender) error {
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
		if err = p.handle(s, m, time.Now()); err != nil {
			log.Printf("EEW processing failed: %v", err)
		}
	}
}
func (p *p2pEarthquakeInterfaces) handle(s eewSender, m eew, now time.Time) error {
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
	embed := makeEmbed(m, kind, issue)
	mentions := &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}
	channel := p.discord.NotifyChannelID
	if state == nil || m.Cancelled {
		p.attempts[attemptKey] = now
		msg, err := s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}, AllowedMentions: mentions})
		if err != nil {
			return err
		}
		if state == nil {
			state = &eventState{ids: make(map[string]bool)}
			p.events[key] = state
		}
		if !m.Cancelled {
			state.messageID = msg.ID
			state.channelID = channel
		}
	} else {
		embeds := []*discordgo.MessageEmbed{embed}
		_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: state.messageID, Channel: state.channelID, Embeds: &embeds, AllowedMentions: mentions})
		var rest *discordgo.RESTError
		if errors.As(err, &rest) && rest.Response != nil && rest.Response.StatusCode == http.StatusNotFound && rest.Message != nil && rest.Message.Code == 10008 {
			p.attempts[attemptKey] = now
			msg, sendErr := s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{Embeds: embeds, AllowedMentions: mentions})
			if sendErr != nil {
				return sendErr
			}
			state.messageID = msg.ID
			state.channelID = channel
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
func makeEmbed(m eew, kind string, issue time.Time) *discordgo.MessageEmbed {
	title := "緊急地震速報（警報）"
	description := ""
	if m.Test {
		title = "【訓練・テスト】" + title
		description = "これはテスト情報です。実際の緊急地震速報ではありません。\n"
	}
	title += "（" + kind + "）"
	fields := []*discordgo.MessageEmbedField{{Name: "イベントID", Value: m.Issue.EventID, Inline: true}, {Name: "報番号", Value: m.Issue.Serial, Inline: true}, {Name: "発表時刻", Value: m.Issue.Time, Inline: true}}
	if m.Earthquake != nil {
		h := m.Earthquake.Hypocenter
		if h.Name != "" {
			fields = append(fields, &discordgo.MessageEmbedField{Name: "震源地", Value: h.Name, Inline: true})
		}
		if h.Magnitude != nil && *h.Magnitude >= 0 {
			fields = append(fields, &discordgo.MessageEmbedField{Name: "マグニチュード", Value: fmt.Sprintf("%.1f", *h.Magnitude), Inline: true})
		}
		if h.Depth != nil && *h.Depth >= 0 {
			fields = append(fields, &discordgo.MessageEmbedField{Name: "深さ", Value: fmt.Sprintf("%.0f km", *h.Depth), Inline: true})
		}
	}
	max := -1
	for _, a := range m.Areas {
		if a.ScaleTo != nil && int(*a.ScaleTo) > max {
			max = int(*a.ScaleTo)
		}
	}
	scales := map[int]string{0: "0", 10: "1", 20: "2", 30: "3", 40: "4", 45: "5弱", 50: "5強", 55: "6弱", 60: "6強", 70: "7", 99: "程度以上"}
	if v, ok := scales[max]; ok {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "予想最大震度（上限）", Value: strings.TrimSpace(v), Inline: true})
	}
	return &discordgo.MessageEmbed{Title: title, Description: description, Fields: fields, Timestamp: issue.UTC().Format(time.RFC3339), Color: 0xffff00, Provider: &discordgo.MessageEmbedProvider{Name: "P2P 地震情報", URL: "https://www.p2pquake.net/app/web/"}}
}
