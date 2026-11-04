// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/golusoris/goenvoy/downloadclient/sabnzbd/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ErrPriorityUnsupported indicates an operation is not supported for the download client kind.
var ErrPriorityUnsupported = errors.New("dlclients: priority operation unsupported for this client kind")

// PartialWriteError records items successfully mutated before a multi-request batch failed.
type PartialWriteError struct {
	Done []string
	Err  error
}

// Error reports how many items completed before the batch failed, and the cause.
func (e *PartialWriteError) Error() string {
	return fmt.Sprintf("partial write completed %d item(s) before failure: %v", len(e.Done), e.Err)
}

// Unwrap returns the error of the request that stopped the batch.
func (e *PartialWriteError) Unwrap() error {
	return e.Err
}

// PriorityWriter executes priority and queue mutations on supported download clients.
//
// For multi-request batch operations where some items succeed before a subsequent item
// fails, implementations do not roll back completed mutations (to avoid extra unrequested
// writes) and instead return a *PartialWriteError naming the item IDs successfully
// completed before the failure. Callers such as the reconciler can inspect this error
// using errors.As to record ownership and durable state for the completed writes.
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
	if strings.TrimSpace(itemID) == "" {
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

// setNZBGetItemPriority checks the level and the positive item ID, then sets the NZBGet item priority.
func (g *Goenvoy) setNZBGetItemPriority(ctx context.Context, c domain.DownloadClient, secret, itemID string, level int) error {
	if !slices.Contains(domain.NZBGetPriorityLevels().Best2Worst, level) {
		return fmt.Errorf("%w: invalid nzbget priority level %d", domain.ErrInvalid, level)
	}
	id, err := validateNZBGetID(itemID)
	if err != nil {
		return err
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

// setSABnzbdItemPriority checks the level and the item ID, then sets the SABnzbd item priority.
func (g *Goenvoy) setSABnzbdItemPriority(ctx context.Context, c domain.DownloadClient, secret, itemID string, level int) error {
	if !slices.Contains(domain.SABnzbdPriorityLevels().Best2Worst, level) {
		return fmt.Errorf("%w: invalid sabnzbd priority level %d", domain.ErrInvalid, level)
	}
	if err := validateSABnzbdID(itemID); err != nil {
		return err
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

type qbitQueueOp string

const (
	qbitOpTopPriority    qbitQueueOp = "top priority"
	qbitOpBottomPriority qbitQueueOp = "bottom priority"
	qbitOpStopTorrents   qbitQueueOp = "stop torrents"
	qbitOpStartTorrents  qbitQueueOp = "start torrents"
)

// execQbitQueueOp runs one allow-listed qBittorrent queue operation on validated hashes;
// an operation outside the closed set fails with domain.ErrInvalid.
func (g *Goenvoy) execQbitQueueOp(ctx context.Context, c domain.DownloadClient, secret string, ids []string, op qbitQueueOp) error {
	cl, err := g.qbitClient(ctx, c, secret)
	if err != nil {
		return err
	}
	switch op {
	case qbitOpTopPriority:
		if err := cl.TopPriority(ctx, ids); err != nil {
			return fmt.Errorf("qbittorrent top priority: %w", err)
		}
	case qbitOpBottomPriority:
		if err := cl.BottomPriority(ctx, ids); err != nil {
			return fmt.Errorf("qbittorrent bottom priority: %w", err)
		}
	case qbitOpStopTorrents:
		if err := cl.StopTorrents(ctx, ids); err != nil {
			return fmt.Errorf("qbittorrent stop torrents: %w", err)
		}
	case qbitOpStartTorrents:
		if err := cl.StartTorrents(ctx, ids); err != nil {
			return fmt.Errorf("qbittorrent start torrents: %w", err)
		}
	default:
		return fmt.Errorf("%w: forbidden qbittorrent queue operation %q", domain.ErrInvalid, op)
	}
	return nil
}

// MoveToTop moves the specified items to the top of the queue (qBittorrent only).
func (g *Goenvoy) MoveToTop(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: item IDs must not be empty", domain.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientQBittorrent:
		if err := validateQbitIDs(ids); err != nil {
			return err
		}
		return g.execQbitQueueOp(ctx, c, secret, ids, qbitOpTopPriority)
	case domain.ClientNZBGet, domain.ClientSABnzbd, domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		return fmt.Errorf("%w: client kind %q does not support MoveToTop", ErrPriorityUnsupported, c.Kind)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

// MoveToBottom moves the specified items to the bottom of the queue (qBittorrent only).
func (g *Goenvoy) MoveToBottom(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: item IDs must not be empty", domain.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientQBittorrent:
		if err := validateQbitIDs(ids); err != nil {
			return err
		}
		return g.execQbitQueueOp(ctx, c, secret, ids, qbitOpBottomPriority)
	case domain.ClientNZBGet, domain.ClientSABnzbd, domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		return fmt.Errorf("%w: client kind %q does not support MoveToBottom", ErrPriorityUnsupported, c.Kind)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

type pauseResumeOp int

const (
	opPause pauseResumeOp = iota
	opResume
)

// dispatchPauseResume applies op to ids on the client kind's adapter under the writer timeout.
// Transmission, Deluge and rTorrent fail with ErrPriorityUnsupported; an unknown kind fails
// with domain.ErrInvalid.
func (g *Goenvoy) dispatchPauseResume(ctx context.Context, c domain.DownloadClient, secret string, ids []string, op pauseResumeOp) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: item IDs must not be empty", domain.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	switch c.Kind {
	case domain.ClientQBittorrent:
		return g.dispatchQbitPauseResume(ctx, c, secret, ids, op)
	case domain.ClientNZBGet:
		return g.dispatchNZBGetPauseResume(ctx, c, secret, ids, op)
	case domain.ClientSABnzbd:
		return g.dispatchSABnzbdPauseResume(ctx, c, secret, ids, op)
	case domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		opName := "PauseItems"
		if op == opResume {
			opName = "ResumeItems"
		}
		return fmt.Errorf("%w: client kind %q does not support %s", ErrPriorityUnsupported, c.Kind, opName)
	default:
		return fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, c.Kind)
	}
}

// dispatchQbitPauseResume validates the hashes and stops or starts those torrents.
func (g *Goenvoy) dispatchQbitPauseResume(ctx context.Context, c domain.DownloadClient, secret string, ids []string, op pauseResumeOp) error {
	if err := validateQbitIDs(ids); err != nil {
		return err
	}
	qbitOp := qbitOpStopTorrents
	if op == opResume {
		qbitOp = qbitOpStartTorrents
	}
	return g.execQbitQueueOp(ctx, c, secret, ids, qbitOp)
}

// dispatchNZBGetPauseResume maps op to the NZBGet GroupPause or GroupResume command.
func (g *Goenvoy) dispatchNZBGetPauseResume(ctx context.Context, c domain.DownloadClient, secret string, ids []string, op pauseResumeOp) error {
	cmd := nzbgetGroupPause
	if op == opResume {
		cmd = nzbgetGroupResume
	}
	return g.editNZBGetQueue(ctx, c, secret, cmd, ids)
}

// dispatchSABnzbdPauseResume validates the IDs and pauses or resumes each SABnzbd item.
func (g *Goenvoy) dispatchSABnzbdPauseResume(ctx context.Context, c domain.DownloadClient, secret string, ids []string, op pauseResumeOp) error {
	if err := validateSABnzbdIDs(ids); err != nil {
		return err
	}
	if op == opPause {
		return g.pauseSABnzbdItems(ctx, c, secret, ids)
	}
	return g.resumeSABnzbdItems(ctx, c, secret, ids)
}

// PauseItems pauses the specified download items in supported clients.
func (g *Goenvoy) PauseItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	return g.dispatchPauseResume(ctx, c, secret, ids, opPause)
}

// ResumeItems resumes the specified download items in supported clients.
func (g *Goenvoy) ResumeItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	return g.dispatchPauseResume(ctx, c, secret, ids, opResume)
}

type nzbgetQueueCommand string

const (
	nzbgetGroupPause  nzbgetQueueCommand = "GroupPause"
	nzbgetGroupResume nzbgetQueueCommand = "GroupResume"
)

// editNZBGetQueue sends one allow-listed EditQueue command for validated item IDs and fails
// when NZBGet rejects it.
func (g *Goenvoy) editNZBGetQueue(ctx context.Context, c domain.DownloadClient, secret string, command nzbgetQueueCommand, ids []string) error {
	intIDs, err := validateNZBGetIDs(ids)
	if err != nil {
		return err
	}
	cl, err := g.nzbgetClient(c, secret)
	if err != nil {
		return err
	}
	var (
		ok      bool
		callErr error
	)
	switch command {
	case nzbgetGroupPause:
		ok, callErr = cl.EditQueue(ctx, "GroupPause", "", intIDs)
	case nzbgetGroupResume:
		ok, callErr = cl.EditQueue(ctx, "GroupResume", "", intIDs)
	default:
		return fmt.Errorf("%w: forbidden nzbget editqueue command %q", domain.ErrInvalid, command)
	}
	if callErr != nil {
		return fmt.Errorf("nzbget edit queue %s: %w", command, callErr)
	}
	if !ok {
		return fmt.Errorf("nzbget edit queue %s rejected for items %v", command, intIDs)
	}
	return nil
}

// pauseSABnzbdItems pauses the items one request at a time; a failure after the first success
// returns a *PartialWriteError naming the paused items.
func (g *Goenvoy) pauseSABnzbdItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	cl, err := g.sabnzbdClient(c, secret)
	if err != nil {
		return err
	}
	done := make([]string, 0, len(ids))
	for _, id := range ids {
		if err := cl.PauseItem(ctx, id); err != nil {
			pauseErr := fmt.Errorf("sabnzbd pause item %q: %w", id, err)
			if len(done) > 0 {
				return &PartialWriteError{Done: done, Err: pauseErr}
			}
			return pauseErr
		}
		done = append(done, id)
	}
	return nil
}

// resumeSABnzbdItems resumes the items one request at a time; a failure after the first success
// returns a *PartialWriteError naming the resumed items.
func (g *Goenvoy) resumeSABnzbdItems(ctx context.Context, c domain.DownloadClient, secret string, ids []string) error {
	cl, err := g.sabnzbdClient(c, secret)
	if err != nil {
		return err
	}
	done := make([]string, 0, len(ids))
	for _, id := range ids {
		if err := cl.ResumeItem(ctx, id); err != nil {
			resumeErr := fmt.Errorf("sabnzbd resume item %q: %w", id, err)
			if len(done) > 0 {
				return &PartialWriteError{Done: done, Err: resumeErr}
			}
			return resumeErr
		}
		done = append(done, id)
	}
	return nil
}

var qbitHashRegex = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// validateQbitIDs accepts a non-empty list of distinct 40- or 64-hex info hashes, so selectors
// such as "all" or "a|b" never reach qBittorrent.
func validateQbitIDs(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: item IDs must not be empty", domain.ErrInvalid)
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !qbitHashRegex.MatchString(id) {
			return fmt.Errorf("%w: invalid qbittorrent item ID %q: must be 40- or 64-hex info hash", domain.ErrInvalid, id)
		}
		canonical := strings.ToLower(id)
		if _, ok := seen[canonical]; ok {
			return fmt.Errorf("%w: duplicate item ID %q", domain.ErrInvalid, id)
		}
		seen[canonical] = struct{}{}
	}
	return nil
}

// validateNZBGetID parses id as a positive NZBGet item ID.
func validateNZBGetID(id string) (int, error) {
	if strings.TrimSpace(id) == "" {
		return 0, fmt.Errorf("%w: nzbget item ID must not be empty", domain.ErrInvalid)
	}
	parsed, err := strconv.Atoi(id)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid nzbget item ID %q: %w", domain.ErrInvalid, id, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%w: invalid nzbget item ID %d: must be positive", domain.ErrInvalid, parsed)
	}
	return parsed, nil
}

// validateNZBGetIDs parses a non-empty list of distinct positive NZBGet item IDs.
func validateNZBGetIDs(ids []string) ([]int, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: item IDs must not be empty", domain.ErrInvalid)
	}
	intIDs := make([]int, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for i, id := range ids {
		val, err := validateNZBGetID(id)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[val]; ok {
			return nil, fmt.Errorf("%w: duplicate item ID %q", domain.ErrInvalid, id)
		}
		seen[val] = struct{}{}
		intIDs[i] = val
	}
	return intIDs, nil
}

// validateSABnzbdID rejects an empty SABnzbd item ID and one with whitespace or control characters.
func validateSABnzbdID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: sabnzbd item ID must not be empty", domain.ErrInvalid)
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%w: invalid sabnzbd item ID %q: contains whitespace or control character", domain.ErrInvalid, id)
		}
	}
	return nil
}

// validateSABnzbdIDs accepts a non-empty list of distinct valid SABnzbd item IDs.
func validateSABnzbdIDs(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: item IDs must not be empty", domain.ErrInvalid)
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if err := validateSABnzbdID(id); err != nil {
			return err
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate item ID %q", domain.ErrInvalid, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}
