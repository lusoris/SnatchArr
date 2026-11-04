// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/golusoris/goenvoy/downloadclient/qbit/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// PriorityItem carries only the metadata needed for attribution and scheduling.
// Titles and filenames never enter a plan or its logs.
type PriorityItem struct {
	domain.DownloadQueueItem
	Category string
}

// PriorityObservation is one read-only client queue snapshot.
type PriorityObservation struct {
	Items            []PriorityItem
	QueueingDisabled bool
}

// PriorityObserver deliberately exposes no client mutation capability.
type PriorityObserver interface {
	PriorityItems(context.Context, domain.DownloadClient, string) (PriorityObservation, error)
}

// PriorityItems reads item metadata under the same deadline as backpressure observations.
func (g *Goenvoy) PriorityItems(ctx context.Context, c domain.DownloadClient, secret string) (PriorityObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	switch c.Kind {
	case domain.ClientQBittorrent:
		return g.qbitPriorityItems(ctx, c, secret)
	case domain.ClientNZBGet:
		return g.nzbgetPriorityItems(ctx, c, secret)
	case domain.ClientSABnzbd:
		return g.sabPriorityItems(ctx, c, secret)
	case domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		// ADR-0046 covers qBittorrent, NZBGet and SABnzbd only.
	}
	return PriorityObservation{}, fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
}

// qbitPriorityItems reads the qBittorrent queueing preferences and torrents as priority items.
func (g *Goenvoy) qbitPriorityItems(ctx context.Context, c domain.DownloadClient, secret string) (PriorityObservation, error) {
	cl, err := g.qbitClient(ctx, c, secret)
	if err != nil {
		return PriorityObservation{}, err
	}
	prefs, err := cl.GetPreferences(ctx)
	if err != nil {
		return PriorityObservation{}, fmt.Errorf("qbit priority preferences: %w", err)
	}
	torrents, err := cl.ListTorrents(ctx, &qbit.ListOptions{})
	if err != nil {
		return PriorityObservation{}, fmt.Errorf("qbit priority torrents: %w", err)
	}
	obs := PriorityObservation{QueueingDisabled: !prefs.QueueingEnabled}
	for _, item := range torrents {
		obs.Items = append(obs.Items, PriorityItem{
			Category: item.Category,
			ID:       item.Hash, Priority: item.Priority, Added: time.Unix(item.AddedOn, 0).UTC(),
			State: priorityItemState(classifyQbit(item.State), item.State == "pausedDL" || item.State == "stoppedDL"),
		})
	}
	return obs, nil
}

// nzbgetPriorityItems reads the NZBGet groups as priority items.
func (g *Goenvoy) nzbgetPriorityItems(ctx context.Context, c domain.DownloadClient, secret string) (PriorityObservation, error) {
	cl, err := g.nzbgetClient(c, secret)
	if err != nil {
		return PriorityObservation{}, err
	}
	groups, err := cl.ListGroups(ctx)
	if err != nil {
		return PriorityObservation{}, fmt.Errorf("nzbget priority groups: %w", err)
	}
	var obs PriorityObservation
	for _, item := range groups {
		// goenvoy v2.0.3 Group has no enqueue timestamp; MinPostTime is article age.
		// Added stays zero: the leveled planner does not use added-time ordering.
		obs.Items = append(obs.Items, PriorityItem{
			Category: item.Category,
			ID:       strconv.Itoa(item.ID), Priority: item.MaxPriority,
			State: priorityItemState(classifyNZBGet(item.Status), item.Status == "PAUSED"),
		})
	}
	return obs, nil
}

// sabPriorityItems reads the SABnzbd queue slots as priority items.
func (g *Goenvoy) sabPriorityItems(ctx context.Context, c domain.DownloadClient, secret string) (PriorityObservation, error) {
	cl, err := g.sabnzbdClient(c, secret)
	if err != nil {
		return PriorityObservation{}, err
	}
	queue, err := cl.GetQueue(ctx)
	if err != nil {
		return PriorityObservation{}, fmt.Errorf("sab priority queue: %w", err)
	}
	var obs PriorityObservation
	activeSeen := false
	for _, item := range queue.Slots {
		priority, err := sabPriority(item.Priority)
		if err != nil {
			return PriorityObservation{}, err
		}
		state, seen := sabPriorityState(item.Status, queue.Paused || queue.PausedAll, activeSeen)
		activeSeen = seen
		// goenvoy v2.0.3 Slot has no enqueue timestamp. Added stays zero, as above.
		obs.Items = append(obs.Items, PriorityItem{
			Category: item.Category,
			ID:       item.ID, Priority: priority,
			State: state,
		})
	}
	return obs, nil
}

// SABnzbd 5.1.3 sabnzbd/api.py build_queue maps slot priority via INTERFACE_PRIORITIES.
// https://github.com/sabnzbd/sabnzbd/blob/5.1.3/sabnzbd/api.py#L1699
// Accept the labels and numeric strings; unrecognised values fail closed.
func sabPriority(value string) (int, error) {
	levels := map[string]int{"Low": -1, "Normal": 0, "High": 1, "Force": 2, "Repair": 3, "Paused": -2}
	if level, ok := levels[value]; ok {
		return level, nil
	}
	level, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%w: unrecognised SAB priority", domain.ErrInvalid)
	}
	return level, nil
}

// priorityItemState maps an adapter class and the paused flag to a download item state.
func priorityItemState(class dlClass, paused bool) domain.DownloadItemState {
	if paused {
		return domain.DownloadItemPaused
	}
	switch class {
	case dlClassActive:
		return domain.DownloadItemActive
	case dlClassQueued:
		return domain.DownloadItemQueued
	case dlClassNone:
		// Completed, failed or post-processing items hold no download slot.
	}
	return domain.DownloadItemFinished
}

// SAB marks all unpaused slots Downloading; only the head transfers (see classifySAB).
func sabPriorityState(status string, globallyPaused, activeSeen bool) (domain.DownloadItemState, bool) {
	class := classifySABStatus(status)
	paused := status == "Paused" || (globallyPaused && class != dlClassNone)
	if class == dlClassActive && activeSeen {
		class = dlClassQueued
	}
	state := priorityItemState(class, paused)
	return state, activeSeen || state == domain.DownloadItemActive
}
