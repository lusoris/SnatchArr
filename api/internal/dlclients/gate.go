// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package dlclients observes torrent and Usenet download clients and turns what it sees
// into snatch backpressure (ADR-0006): an unreachable, paused or saturated client stops
// snatches that would only pile up grabs, and a bandwidth budget paces per-cycle counts.
// It never adds, removes or reorders downloads.
package dlclients

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// Snapshot is one download client's state as last observed.
type Snapshot struct {
	ClientID     uuid.UUID
	Name         string
	Kind         domain.DownloadClientKind
	Reachable    bool
	Paused       bool
	Active       int
	Queued       int
	DownloadRate int64 // bytes per second
	UploadRate   int64 // bytes per second
	Version      string
	Error        string
	CheckedAt    time.Time
}

// Verdict is the backpressure decision for one instance.
type Verdict struct {
	Allowed    bool
	Reason     string
	PaceFactor float64 // share (0..1) of per_cycle that the bandwidth budgets leave free
}

// Evaluate applies the backpressure rules over the clients that apply to an instance.
// Clients without an observation yet never block: ignorance is not backpressure.
func Evaluate(clients []domain.DownloadClient, snaps map[uuid.UUID]Snapshot) Verdict {
	v := Verdict{Allowed: true, PaceFactor: 1}
	for _, c := range clients {
		s, ok := snaps[c.ID]
		if !c.Enabled || !ok {
			continue
		}
		if reason := blockReason(c, s); reason != "" {
			return Verdict{Reason: reason}
		}
		f := paceFactor(c, s)
		if f <= 0 {
			return Verdict{Reason: fmt.Sprintf("download client %s is saturating its bandwidth budget", c.Name)}
		}
		v.PaceFactor = min(v.PaceFactor, f)
	}
	return v
}

func blockReason(c domain.DownloadClient, s Snapshot) string {
	switch {
	case !s.Reachable:
		return fmt.Sprintf("download client %s is unreachable: %s", c.Name, s.Error)
	case s.Paused:
		return fmt.Sprintf("download client %s is paused", c.Name)
	case c.MaxActive > 0 && s.Active >= c.MaxActive:
		return fmt.Sprintf("download client %s has %d active downloads (limit %d)", c.Name, s.Active, c.MaxActive)
	default:
		return ""
	}
}

// paceFactor is the share of the bandwidth budget still free: 1 without a budget, 0 when
// the client already downloads at or above it.
func paceFactor(c domain.DownloadClient, s Snapshot) float64 {
	if c.BandwidthBudgetBPS <= 0 {
		return 1
	}
	used := float64(s.DownloadRate) / float64(c.BandwidthBudgetBPS)
	return max(0, min(1, 1-used))
}

// Scale reduces a per-cycle count by the pace factor, never below one while snatching is
// allowed at all.
func Scale(perCycle int, factor float64) int {
	switch {
	case perCycle <= 0 || factor <= 0:
		return 0
	case factor >= 1:
		return perCycle
	default:
		return max(1, int(math.Ceil(float64(perCycle)*factor)))
	}
}

// PlannerGate adapts the service to snatch.Gate and records state transitions as events so
// history shows why an instance stopped snatching, without an entry per planner tick.
type PlannerGate struct {
	svc    *Service
	rec    *snatch.Recorder
	logger *slog.Logger
	mu     sync.Mutex
	block  map[uuid.UUID]string
}

// NewPlannerGate wires the gate.
func NewPlannerGate(svc *Service, rec *snatch.Recorder, logger *slog.Logger) *PlannerGate {
	return &PlannerGate{svc: svc, rec: rec, logger: logger, block: make(map[uuid.UUID]string)}
}

// Allow implements snatch.Gate.
func (g *PlannerGate) Allow(ctx context.Context, inst domain.Instance, _ domain.SnatchKind) (bool, string, error) {
	v, err := g.svc.Verdict(ctx, inst.ID)
	if err != nil {
		return false, "", err
	}
	g.transition(ctx, inst, v)
	return v.Allowed, v.Reason, nil
}

func (g *PlannerGate) transition(ctx context.Context, inst domain.Instance, v Verdict) {
	g.mu.Lock()
	prev, wasBlocked := g.block[inst.ID]
	if v.Allowed {
		delete(g.block, inst.ID)
	} else {
		g.block[inst.ID] = v.Reason
	}
	g.mu.Unlock()
	switch {
	case !v.Allowed && prev != v.Reason:
		g.record(ctx, domain.Event{InstanceID: inst.ID, Level: "warn", Type: "snatch_deferred", Title: "Foreplay: snatching deferred by download-client backpressure", Detail: v.Reason})
	case v.Allowed && wasBlocked:
		g.record(ctx, domain.Event{InstanceID: inst.ID, Level: "info", Type: "snatch_resumed", Title: "Foreplay over: download-client backpressure cleared", Detail: prev})
	}
}

func (g *PlannerGate) record(ctx context.Context, e domain.Event) {
	if err := g.rec.Record(ctx, e); err != nil {
		g.logger.WarnContext(ctx, "dlclients: record gate event", slog.String("error", err.Error()))
	}
}
