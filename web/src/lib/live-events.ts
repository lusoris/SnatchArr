// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// The API streams every event as a named SSE event (events.EventName, lower
// snake case), and EventSource delivers named events only to listeners
// registered for that name. Keep this list in step with the event types the
// API records: worker.proto EventType plus the API-only events.
export const LIVE_EVENTS = [
	// snatcharr.v1.EventType
	'run_started',
	'page_fetched',
	'candidates_filtered',
	'budget_acquired',
	'search_dispatched',
	'search_completed',
	'search_failed',
	'instance_unreachable',
	'run_finished',
	'circuit_opened',
	'search_previewed',
	// recorded by the API itself
	'run_queued',
	'snatch_deferred',
	'snatch_resumed',
	'snatch_paced',
	'snatch_backoff',
	'stamina_exhausted',
	'seerr_request_snatched',
	'events_unconfirmed',
	'notification_failed'
] as const;
