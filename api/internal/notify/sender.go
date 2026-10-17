// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/notify"
	"github.com/golusoris/golusoris/notify/discord"
	"github.com/golusoris/golusoris/notify/gotify"
	"github.com/golusoris/golusoris/notify/ntfy"
	"github.com/golusoris/golusoris/notify/slack"
	"github.com/golusoris/golusoris/notify/teams"
	"github.com/golusoris/golusoris/notify/telegram"
	"github.com/golusoris/golusoris/webhooks/out"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Payload is the data sent to notification destinations.
type Payload struct {
	Event      string
	Timestamp  time.Time
	Title      string
	Detail     string
	Level      string
	InstanceID *uuid.UUID
	RunID      *uuid.UUID
	Metadata   map[string]any
}

// WebhookPayload matches the JSON schema for outgoing webhooks.
type WebhookPayload struct {
	Event     string      `json:"event"`
	Timestamp time.Time   `json:"timestamp"`
	Data      WebhookData `json:"data"`
}

// WebhookData holds event-specific fields in the webhook body.
type WebhookData struct {
	InstanceID *uuid.UUID     `json:"instance_id,omitempty"`
	RunID      *uuid.UUID     `json:"run_id,omitempty"`
	Title      string         `json:"title"`
	Detail     string         `json:"detail,omitempty"`
	Level      string         `json:"level,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Deliver sends a notification payload to a channel synchronously.
func Deliver(ctx context.Context, ch domain.NotificationChannel, p Payload, ws out.Store, logger *slog.Logger, clk clock.Clock) error {
	if ch.Kind == domain.ChannelWebhook {
		return deliverWebhook(ctx, ch, p, ws, logger, clk)
	}
	sender, err := buildChatSender(ch)
	if err != nil {
		return fmt.Errorf("build sender for %s: %w", ch.Name, err)
	}
	body := p.Detail
	if body == "" {
		body = p.Title
	}
	msg := notify.Message{
		Subject:  p.Title,
		Body:     body,
		Text:     body,
		Metadata: map[string]string{"level": p.Level},
	}
	if err := sender.Send(ctx, msg); err != nil {
		return fmt.Errorf("send to %s: %w", ch.Name, err)
	}
	return nil
}

func buildChatSender(ch domain.NotificationChannel) (notify.Sender, error) {
	switch ch.Kind {
	case domain.ChannelDiscord:
		return buildDiscord(ch.Config)
	case domain.ChannelSlack:
		return buildSlack(ch.Config)
	case domain.ChannelTelegram:
		return buildTelegram(ch.Config)
	case domain.ChannelNtfy:
		return buildNtfy(ch.Config)
	case domain.ChannelGotify:
		return buildGotify(ch.Config)
	case domain.ChannelTeams:
		return buildTeams(ch.Config)
	case domain.ChannelApprise:
		return NewAppriseSender(AppriseOptions{
			NotifyURL: ch.Config.WebhookURL,
			Username:  ch.Config.Username,
			Password:  ch.Config.Password,
			Tags:      ch.Config.Tags,
		})
	case domain.ChannelWebhook:
		return nil, fmt.Errorf("%w: webhook channel is not a chat sender", domain.ErrInvalid)
	default:
		return nil, fmt.Errorf("%w: unsupported channel kind: %s", domain.ErrInvalid, ch.Kind)
	}
}

func buildDiscord(cfg domain.ChannelConfig) (notify.Sender, error) {
	s, err := discord.NewSender(discord.Options{
		WebhookURL: cfg.WebhookURL,
		Username:   cfg.Username,
		AvatarURL:  cfg.AvatarURL,
	})
	if err != nil {
		return nil, fmt.Errorf("notify: build discord sender: %w", err)
	}
	return s, nil
}

func buildSlack(cfg domain.ChannelConfig) (notify.Sender, error) {
	s, err := slack.NewSender(slack.Options{
		WebhookURL: cfg.WebhookURL,
		Channel:    cfg.Channel,
		Username:   cfg.Username,
		IconEmoji:  cfg.IconEmoji,
	})
	if err != nil {
		return nil, fmt.Errorf("notify: build slack sender: %w", err)
	}
	return s, nil
}

func buildTelegram(cfg domain.ChannelConfig) (notify.Sender, error) {
	s, err := telegram.NewSender(telegram.Options{
		BotToken:  cfg.BotToken,
		ChatID:    cfg.ChatID,
		ParseMode: telegram.ParseMode(cfg.ParseMode),
	})
	if err != nil {
		return nil, fmt.Errorf("notify: build telegram sender: %w", err)
	}
	return s, nil
}

func buildNtfy(cfg domain.ChannelConfig) (notify.Sender, error) {
	s, err := ntfy.NewSender(ntfy.Options{
		ServerURL: cfg.ServerURL,
		Topic:     cfg.Topic,
		Token:     cfg.Token,
		Priority:  cfg.Priority,
		Tags:      cfg.Tags,
	})
	if err != nil {
		return nil, fmt.Errorf("notify: build ntfy sender: %w", err)
	}
	return s, nil
}

func buildGotify(cfg domain.ChannelConfig) (notify.Sender, error) {
	s, err := gotify.NewSender(gotify.Options{
		ServerURL: cfg.ServerURL,
		AppToken:  cfg.AppToken,
		Priority:  cfg.Priority,
	})
	if err != nil {
		return nil, fmt.Errorf("notify: build gotify sender: %w", err)
	}
	return s, nil
}

func buildTeams(cfg domain.ChannelConfig) (notify.Sender, error) {
	s, err := teams.NewSender(teams.Options{
		WebhookURL: cfg.WebhookURL,
		ThemeColor: cfg.ThemeColor,
	})
	if err != nil {
		return nil, fmt.Errorf("notify: build teams sender: %w", err)
	}
	return s, nil
}

type singleEndpointStore struct {
	out.Store
	endpoint out.Endpoint
}

func (s *singleEndpointStore) ListEndpoints(ctx context.Context, _ string) ([]out.Endpoint, error) {
	return []out.Endpoint{s.endpoint}, nil
}

func deliverWebhook(ctx context.Context, ch domain.NotificationChannel, p Payload, ws out.Store, logger *slog.Logger, clk clock.Clock) error {
	ep := out.Endpoint{
		ID:     ch.ID.String(),
		URL:    ch.Config.URL,
		Secret: ch.Config.Secret,
		Active: true,
	}
	opts := out.Options{
		AllowPrivateNetwork: ch.Config.AllowPrivateNetwork,
		AllowInsecureHTTP:   ch.Config.AllowInsecureHTTP,
		MaxAttempts:         3,
		Timeout:             10 * time.Second,
		Backoff:             func(int) time.Duration { return 50 * time.Millisecond },
	}
	d := out.New(&singleEndpointStore{Store: ws, endpoint: ep}, opts, logger, clk)
	wp := WebhookPayload{
		Event:     p.Event,
		Timestamp: p.Timestamp,
		Data: WebhookData{
			InstanceID: p.InstanceID,
			RunID:      p.RunID,
			Title:      p.Title,
			Detail:     p.Detail,
			Level:      p.Level,
			Metadata:   p.Metadata,
		},
	}
	if err := d.Dispatch(ctx, p.Event, wp); err != nil {
		return fmt.Errorf("notify: dispatch webhook: %w", err)
	}
	return nil
}

func urlUserInfoSecrets(rawURL string) []string {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return nil
	}
	var res []string
	if pass, ok := u.User.Password(); ok && pass != "" {
		res = append(res, pass)
	}
	if uInfo := u.User.String(); uInfo != "" {
		res = append(res, uInfo)
	}
	return res
}

// SanitizeError scrubs secrets from error messages before logging or storing events.
func SanitizeError(err error, cfg domain.ChannelConfig) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	secrets := []string{
		cfg.WebhookURL, cfg.URL, cfg.Token, cfg.BotToken, cfg.AppToken, cfg.Password, cfg.Secret,
	}
	for _, rawURL := range []string{cfg.ServerURL, cfg.WebhookURL, cfg.URL} {
		secrets = append(secrets, urlUserInfoSecrets(rawURL)...)
	}
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "[REDACTED]")
		}
	}
	return msg
}

// DefaultHTTPClient creates an HTTP client for testing or custom dialers.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}
