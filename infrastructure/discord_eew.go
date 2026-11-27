package infrastructure

import (
	"errors"
	"fmt"
	"github.com/FEATO-org/support-feato-system/usecase"
	"github.com/bwmarrin/discordgo"
	"net/http"
	"strings"
	"time"
)

type EEWSender interface {
	ChannelMessageSendComplex(string, *discordgo.MessageSend, ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessageEditComplex(*discordgo.MessageEdit, ...discordgo.RequestOption) (*discordgo.Message, error)
}
type EEWDiscordNotifier struct {
	sender  EEWSender
	channel string
}

func NewEEWDiscordNotifier(sender EEWSender, channel string) *EEWDiscordNotifier {
	return &EEWDiscordNotifier{sender: sender, channel: channel}
}
func (n *EEWDiscordNotifier) Send(m usecase.EEWEvent, kind string, issue time.Time) (usecase.EEWMessageRef, error) {
	msg, err := n.sender.ChannelMessageSendComplex(n.channel, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{makeEmbed(m, kind, issue)}, AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}})
	if err != nil {
		return usecase.EEWMessageRef{}, err
	}
	return usecase.EEWMessageRef{ID: msg.ID, Channel: n.channel}, nil
}
func (n *EEWDiscordNotifier) Edit(ref usecase.EEWMessageRef, m usecase.EEWEvent, kind string, issue time.Time) error {
	embeds := []*discordgo.MessageEmbed{makeEmbed(m, kind, issue)}
	_, err := n.sender.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: ref.ID, Channel: ref.Channel, Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}})
	var rest *discordgo.RESTError
	if errors.As(err, &rest) && rest.Response != nil && rest.Response.StatusCode == http.StatusNotFound && rest.Message != nil && rest.Message.Code == 10008 {
		return usecase.ErrEEWMessageNotFound
	}
	return err
}
func makeEmbed(m usecase.EEWEvent, kind string, issue time.Time) *discordgo.MessageEmbed {
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
