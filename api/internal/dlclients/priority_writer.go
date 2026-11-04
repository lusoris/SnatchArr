// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/golusoris/goenvoy/downloadclient/sabnzbd/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ErrPriorityUnsupported indicates an operation is not supported for the download client kind.
var ErrPriorityUnsupported = errors.New("dlclients: priority operation unsupported for this client kind")

// PriorityWriter executes priority and queue mutations on supported download clients.
type PriorityWriter interface {
	SetItemPriority(ctx context.Context, c domain.DownloadClient, secret, itemID string, level int) error
	MoveToTop(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error
	MoveToBottom(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error
	PauseItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error
	ResumeItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error
}

var _ PriorityWriter = (*Goenvoy)(nil)

// SetItemPriority sets an item's priority level in supported clients (NZBGet, SABnzbd).
func (g *Goenvoy) SetItemPriority(ctx context.Context, c domain.DownloadClient, secret, itemID string, level int) error {
	if itemID == "" {
		return fmt.Errorf("%w: item ID must not be empty", domain.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientNZBGet:
		return g.setNZBGetItemPriority(ctx, c, secret, itemID, level)
	case domain.ClientSABnzbd:
		return g.setSABnzbdItemPriority(ctx, c, secret, itemID, level)
	case domain.ClientQBittorrent, domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		return fmt.Errorf("%w: client kind %q does not support SetItemPriority", ErrPriorityUnsupported, c.Kind)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

func (g *Goenvoy) setNZBGetItemPriority(ctx context.Context, c domain.DownloadClient, secret, itemID string, level int) error {
	if !slices.Contains(domain.NZBGetPriorityLevels().Best2Worst, level) {
		return fmt.Errorf("%w: invalid nzbget priority level %d", domain.ErrInvalid, level)
	}
	id, err := strconv.Atoi(itemID)
	if err != nil {
		return fmt.Errorf("%w: invalid nzbget item ID %q: %w", domain.ErrInvalid, itemID, err)
	}
	cl, err := g.nzbgetClient(c, secret)
	if err != nil {
		return err
	}
	ok, err := cl.SetPriority(ctx, id, level)
	if err != nil {
		return fmt.Errorf("nzbget set priority: %w", err)
	}
	if !ok {
		return fmt.Errorf("nzbget set priority rejected for item %d", id)
	}
	return nil
}

func (g *Goenvoy) setSABnzbdItemPriority(ctx context.Context, c domain.DownloadClient, secret, itemID string, level int) error {
	if !slices.Contains(domain.SABnzbdPriorityLevels().Best2Worst, level) {
		return fmt.Errorf("%w: invalid sabnzbd priority level %d", domain.ErrInvalid, level)
	}
	cl, err := g.sabnzbdClient(c, secret)
	if err != nil {
		return err
	}
	if _, err := cl.SetPriority(ctx, itemID, sabnzbd.Priority(level)); err != nil {
		return fmt.Errorf("sabnzbd set priority: %w", err)
	}
	return nil
}

// MoveToTop moves the specified items to the top of the queue (qBittorrent only).
func (g *Goenvoy) MoveToTop(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	if err := validateIDs(ids); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientQBittorrent:
		cl, err := g.qbitClient(ctx, c, secret)
		if err != nil {
			return err
		}
		if err := cl.TopPriority(ctx, ids); err != nil {
			return fmt.Errorf("qbittorrent top priority: %w", err)
		}
		return nil
	case domain.ClientNZBGet, domain.ClientSABnzbd, domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		return fmt.Errorf("%w: client kind %q does not support MoveToTop", ErrPriorityUnsupported, c.Kind)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

// MoveToBottom moves the specified items to the bottom of the queue (qBittorrent only).
func (g *Goenvoy) MoveToBottom(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	if err := validateIDs(ids); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientQBittorrent:
		cl, err := g.qbitClient(ctx, c, secret)
		if err != nil {
			return err
		}
		if err := cl.BottomPriority(ctx, ids); err != nil {
			return fmt.Errorf("qbittorrent bottom priority: %w", err)
		}
		return nil
	case domain.ClientNZBGet, domain.ClientSABnzbd, domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		return fmt.Errorf("%w: client kind %q does not support MoveToBottom", ErrPriorityUnsupported, c.Kind)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

// PauseItems pauses the specified download items in supported clients.
func (g *Goenvoy) PauseItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	if err := validateIDs(ids); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientQBittorrent:
		return g.pauseQbitItems(ctx, c, secret, ids)
	case domain.ClientNZBGet:
		return g.editNZBGetQueue(ctx, c, secret, "GroupPause", ids)
	case domain.ClientSABnzbd:
		return g.pauseSABnzbdItems(ctx, c, secret, ids)
	case domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		return fmt.Errorf("%w: client kind %q does not support PauseItems", ErrPriorityUnsupported, c.Kind)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

// ResumeItems resumes the specified download items in supported clients.
func (g *Goenvoy) ResumeItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	if err := validateIDs(ids); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientQBittorrent:
		return g.resumeQbitItems(ctx, c, secret, ids)
	case domain.ClientNZBGet:
		return g.editNZBGetQueue(ctx, c, secret, "GroupResume", ids)
	case domain.ClientSABnzbd:
		return g.resumeSABnzbdItems(ctx, c, secret, ids)
	case domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		return fmt.Errorf("%w: client kind %q does not support ResumeItems", ErrPriorityUnsupported, c.Kind)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

func (g *Goenvoy) pauseQbitItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	cl, err := g.qbitClient(ctx, c, secret)
	if err != nil {
		return err
	}
	if err := cl.StopTorrents(ctx, ids); err != nil {
		return fmt.Errorf("qbittorrent stop torrents: %w", err)
	}
	return nil
}

func (g *Goenvoy) resumeQbitItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	cl, err := g.qbitClient(ctx, c, secret)
	if err != nil {
		return err
	}
	if err := cl.StartTorrents(ctx, ids); err != nil {
		return fmt.Errorf("qbittorrent start torrents: %w", err)
	}
	return nil
}

func (g *Goenvoy) editNZBGetQueue(ctx context.Context, c domain.DownloadClient, secret, command string, ids []string) error {
	intIDs := make([]int, len(ids))
	for i, id := range ids {
		parsed, err := strconv.Atoi(id)
		if err != nil {
			return fmt.Errorf("%w: invalid nzbget item ID %q: %w", domain.ErrInvalid, id, err)
		}
		intIDs[i] = parsed
	}
	cl, err := g.nzbgetClient(c, secret)
	if err != nil {
		return err
	}
	var ok bool
	switch command {
	case "GroupPause":
		ok, err = cl.EditQueue(ctx, "GroupPause", "", intIDs)
	case "GroupResume":
		ok, err = cl.EditQueue(ctx, "GroupResume", "", intIDs)
	default:
		return fmt.Errorf("%w: forbidden nzbget editqueue command %q", domain.ErrInvalid, command)
	}
	if err != nil {
		return fmt.Errorf("nzbget edit queue %s: %w", command, err)
	}
	if !ok {
		return fmt.Errorf("nzbget edit queue %s rejected for items %v", command, intIDs)
	}
	return nil
}

func (g *Goenvoy) pauseSABnzbdItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	cl, err := g.sabnzbdClient(c, secret)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := cl.PauseItem(ctx, id); err != nil {
			return fmt.Errorf("sabnzbd pause item %q: %w", id, err)
		}
	}
	return nil
}

func (g *Goenvoy) resumeSABnzbdItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	cl, err := g.sabnzbdClient(c, secret)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := cl.ResumeItem(ctx, id); err != nil {
			return fmt.Errorf("sabnzbd resume item %q: %w", id, err)
		}
	}
	return nil
}

func validateIDs(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: item IDs must not be empty", domain.ErrInvalid)
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("%w: item ID must not be empty", domain.ErrInvalid)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate item ID %q", domain.ErrInvalid, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}
