// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Inspection page bounds for live reads.
const (
	inspectMaxPages = 1
	inspectPageSize = 100
)

// InspectedItem represents a live queue item evaluated against queue policy.
type InspectedItem struct {
	QueueID               int32
	DownloadID            string
	Title                 string
	Status                string
	TrackedDownloadStatus string
	TrackedDownloadState  string
	Protocol              string
	Size                  int64
	SizeLeft              int64
	Added                 time.Time
	StatusLines           []string
	Rule                  Rule
	StrikeCount           int
	Threshold             int
	WouldRemove           bool
	Remove                bool
	BlocklistOnRemoval    bool
	Deferred              bool
	WouldImport           bool
	Import                bool
}

// Inspection is the result of evaluating an instance's live queue.
type Inspection struct {
	Items     []InspectedItem
	Truncated bool
}

// Inspect fetches the live download queue of an enabled instance, evaluates
// sweep rules purely without mutating state, and returns the inspected items.
func (s *Service) Inspect(ctx context.Context, instanceID uuid.UUID) (Inspection, error) {
	inst, err := s.instances.EnabledInstance(ctx, instanceID)
	if err != nil {
		return Inspection{}, err
	}
	cfg, err := s.store.Settings(ctx, inst.ID)
	if err != nil {
		return Inspection{}, fmt.Errorf("queue settings for %s: %w", inst.Name, err)
	}
	cfg.ForceImport = SupportsForceImport(inst.Kind)

	prev, err := s.store.Strikes(ctx, inst.ID)
	if err != nil {
		return Inspection{}, fmt.Errorf("queue strikes for %s: %w", inst.Name, err)
	}

	items, err := s.arr.Queue(ctx, inst.Kind, inst.BaseURL, inst.APIKey, inspectMaxPages, inspectPageSize)
	if err != nil {
		return Inspection{}, fmt.Errorf("fetch queue for %s: %w", inst.Name, err)
	}

	decisions := Sweep(cfg, items, prev, s.clk.Now())
	inspected := make([]InspectedItem, 0, len(items))
	for i, item := range items {
		var d Decision
		if i < len(decisions) {
			d = decisions[i]
		}
		inspected = append(inspected, buildInspectedItem(item, d))
	}

	return Inspection{
		Items:     inspected,
		Truncated: len(items) == inspectPageSize,
	}, nil
}

func buildInspectedItem(item Item, d Decision) InspectedItem {
	var (
		rule               Rule
		strikeCount        int
		threshold          int
		wouldRemove        bool
		remove             bool
		blocklistOnRemoval bool
		deferred           bool
		wouldImport        bool
		imp                bool
	)
	if d.Rule != "" {
		rule = d.Rule
		strikeCount = d.Next.Count
		threshold = d.Threshold
		wouldRemove = d.WouldRemove
		remove = d.Remove
		blocklistOnRemoval = d.Blocklist
		deferred = d.Deferred
		wouldImport = d.WouldImport
		imp = d.Import
	}
	statusLines := item.StatusLines
	if statusLines == nil {
		statusLines = []string{}
	}
	return InspectedItem{
		QueueID:               item.ID,
		DownloadID:            item.DownloadID,
		Title:                 item.Title,
		Status:                item.Status,
		TrackedDownloadStatus: item.TrackedDownloadStatus,
		TrackedDownloadState:  item.TrackedDownloadState,
		Protocol:              item.Protocol,
		Size:                  item.Size,
		SizeLeft:              item.SizeLeft,
		Added:                 item.Added,
		StatusLines:           statusLines,
		Rule:                  rule,
		StrikeCount:           strikeCount,
		Threshold:             threshold,
		WouldRemove:           wouldRemove,
		Remove:                remove,
		BlocklistOnRemoval:    blocklistOnRemoval,
		Deferred:              deferred,
		WouldImport:           wouldImport,
		Import:                imp,
	}
}
