// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// JellyfinRevalidateInterval is the leader loop interval for revalidating Jellyfin sessions.
	JellyfinRevalidateInterval = 30 * time.Minute
	// JellyfinSessionBatchSize bounds the number of live sessions loaded in one leader loop tick.
	JellyfinSessionBatchSize int32 = 1000
	// JellyfinMaxDistinctSubjects bounds distinct member lookups against Jellyfin in one tick.
	JellyfinMaxDistinctSubjects = 200
)

type sessionTarget struct {
	id   string
	role string
}

// JellyfinRevalidateTicker periodically checks live Jellyfin sessions against Jellyfin.
type JellyfinRevalidateTicker struct {
	*gate.Periodic

	svc     *Service
	reval   *jellyfin.Revalidator
	cfg     config.Options
	clk     clock.Clock
	logger  *slog.Logger
	timeout time.Duration
}

// NewJellyfinRevalidateTicker constructs the periodic leader loop ticker for Jellyfin sessions.
func NewJellyfinRevalidateTicker(svc *Service, reval *jellyfin.Revalidator, cfg config.Options, clk clock.Clock, m *metrics.Metrics, logger *slog.Logger) *JellyfinRevalidateTicker {
	timeout := cfg.Jellyfin.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	t := &JellyfinRevalidateTicker{
		svc:     svc,
		reval:   reval,
		cfg:     cfg,
		clk:     clk,
		logger:  logger,
		timeout: timeout,
	}
	t.Periodic = gate.NewPeriodic("jellyfin: revalidate", JellyfinRevalidateInterval, clk, m, t.tick)
	return t
}

// Tick executes one revalidation pass (exposed for tests).
func (t *JellyfinRevalidateTicker) Tick(ctx context.Context) error {
	return t.tick(ctx)
}

func (t *JellyfinRevalidateTicker) tick(ctx context.Context) error {
	if !t.cfg.Jellyfin.Enabled {
		return nil
	}

	rows, err := t.svc.st.Q().ListSessionsBySSOSource(ctx, sqlcgen.ListSessionsBySSOSourceParams{
		Source: "jellyfin",
		Now:    t.clk.Now(),
		Batch:  JellyfinSessionBatchSize,
	})
	if err != nil {
		return fmt.Errorf("auth: list jellyfin sessions: %w", store.MapError(err))
	}
	if len(rows) == 0 {
		return nil
	}

	bySubject, subjects := collectDistinctSubjects(rows, JellyfinMaxDistinctSubjects)
	counts := make(map[string]int)

	for _, subj := range subjects {
		sessions := bySubject[subj]
		t.revalidateSubjectSessions(ctx, subj, sessions, counts)
	}

	t.logRevocations(ctx, counts)
	return nil
}

func collectDistinctSubjects(rows []sqlcgen.ListSessionsBySSOSourceRow, limit int) (map[string][]sessionTarget, []string) {
	bySubject := make(map[string][]sessionTarget)
	var subjects []string

	for _, row := range rows {
		subj, _ := row.Subject.(string)
		subj = strings.TrimSpace(subj)
		if subj == "" {
			continue
		}
		role, _ := row.Role.(string)
		target := sessionTarget{id: row.ID, role: strings.TrimSpace(role)}

		if list, exists := bySubject[subj]; exists {
			bySubject[subj] = append(list, target)
			continue
		}
		if len(subjects) >= limit {
			continue
		}
		subjects = append(subjects, subj)
		bySubject[subj] = []sessionTarget{target}
	}
	return bySubject, subjects
}

func (t *JellyfinRevalidateTicker) revalidateSubjectSessions(ctx context.Context, subj string, sessions []sessionTarget, counts map[string]int) {
	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	m, reason, err := t.reval.RevalidateSubject(callCtx, subj)
	cancel()

	if err != nil {
		if t.logger != nil {
			t.logger.WarnContext(ctx, "auth: jellyfin session revalidation failed",
				slog.String("subject", subj),
				slog.String("error", err.Error()),
			)
		}
		return
	}

	if reason != "" {
		t.revokeSessions(ctx, sessions, reason, counts)
		return
	}

	for _, sess := range sessions {
		if roleReason := t.reval.CheckRole(m, domain.Role(sess.role)); roleReason != "" {
			t.revokeSingleSession(ctx, sess.id, roleReason, counts)
		}
	}
}

func (t *JellyfinRevalidateTicker) revokeSessions(ctx context.Context, sessions []sessionTarget, reason string, counts map[string]int) {
	for _, sess := range sessions {
		t.revokeSingleSession(ctx, sess.id, reason, counts)
	}
}

func (t *JellyfinRevalidateTicker) revokeSingleSession(ctx context.Context, sessID, reason string, counts map[string]int) {
	if err := t.svc.RevokeSession(ctx, sessID); err != nil {
		if t.logger != nil {
			t.logger.WarnContext(ctx, "auth: revoke jellyfin session failed",
				slog.String("reason", reason),
				slog.String("error", err.Error()),
			)
		}
		return
	}
	counts[reason]++
}

func (t *JellyfinRevalidateTicker) logRevocations(ctx context.Context, counts map[string]int) {
	if t.logger == nil {
		return
	}
	for reason, count := range counts {
		if count > 0 {
			t.logger.InfoContext(ctx, "auth: jellyfin sessions revoked",
				slog.String("reason", reason),
				slog.Int("count", count),
			)
		}
	}
}
