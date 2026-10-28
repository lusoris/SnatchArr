// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';

export type ApiEvent = components['schemas']['Event'];

export interface LiveFrame {
	id?: number;
	event: string;
	run_id?: string;
	instance_id?: string;
	ts?: string;
	level?: string;
	type?: string;
	entity_type?: string;
	entity_id?: number;
	title?: string;
	detail?: string;
}

export function parseFrame(eventType: string, rawPayload: string, lastEventId?: string): LiveFrame {
	let parsed: Partial<LiveFrame>;
	try {
		parsed = JSON.parse(rawPayload) as Partial<LiveFrame>;
	} catch {
		parsed = { title: rawPayload };
	}

	let id = parsed.id;
	if (id === undefined && lastEventId) {
		const parsedId = Number.parseInt(lastEventId, 10);
		if (!Number.isNaN(parsedId) && parsedId > 0) {
			id = parsedId;
		}
	}

	return {
		id,
		event: eventType,
		run_id: parsed.run_id,
		instance_id: parsed.instance_id,
		ts: parsed.ts,
		level: parsed.level,
		type: parsed.type ?? eventType,
		entity_type: parsed.entity_type,
		entity_id: parsed.entity_id,
		title: parsed.title,
		detail: parsed.detail
	};
}

export function eventToLiveFrame(e: ApiEvent): LiveFrame {
	return {
		id: e.id,
		event: e.type,
		run_id: e.run_id,
		instance_id: e.instance_id,
		ts: e.ts,
		level: e.level,
		type: e.type,
		entity_type: e.entity_type,
		entity_id: e.entity_id,
		title: e.title,
		detail: e.detail
	};
}

/** Adapts partial SSE frames for the same detail view as persisted events. */
export function liveFrameToEvent(frame: LiveFrame): ApiEvent {
	const level = frame.level;
	return {
		...frame,
		id: frame.id ?? 0,
		instance_id: frame.instance_id ?? '',
		ts: frame.ts ?? '',
		type: frame.type ?? frame.event,
		level: level === 'error' || level === 'warn' || level === 'debug' ? level : 'info'
	};
}

function parseTime(ts?: string): number {
	if (!ts) return 0;
	const parsed = Date.parse(ts);
	return Number.isNaN(parsed) ? 0 : parsed;
}

/**
 * Merges stream and backlog frames, newest first, deduplicating by frame ID.
 * Frames lacking an ID are preserved as is. Caps output to `cap` items (default 500).
 */
export function mergeLiveFrames(
	streamFrames: readonly LiveFrame[],
	backlogFrames: readonly LiveFrame[],
	cap = 500
): LiveFrame[] {
	// Stream frames arrive real-time after mount; backlog represents stored history.
	// Sort newest first by timestamp; if equal or missing, preserve relative stream -> backlog order.
	const combined = [...streamFrames, ...backlogFrames].sort((a, b) => {
		const diff = parseTime(b.ts) - parseTime(a.ts);
		if (diff !== 0) return diff;
		return 0;
	});

	const seenIds = new Set<number>();
	const merged: LiveFrame[] = [];

	for (const frame of combined) {
		if (frame.id !== undefined && frame.id !== 0) {
			if (seenIds.has(frame.id)) {
				continue;
			}
			seenIds.add(frame.id);
		}
		merged.push(frame);
		if (merged.length >= cap) {
			break;
		}
	}

	return merged;
}

/**
 * Maps event levels to design-token based CSS classes (no hardcoded color literals).
 */
export function levelTokenClasses(level?: string): {
	chipClass: string;
	dotClass: string;
	textClass: string;
} {
	switch (level?.toLowerCase()) {
		case 'error':
			return {
				chipClass: 'chip-danger',
				dotClass: 'bg-danger',
				textClass: 'text-danger'
			};
		case 'warn':
		case 'warning':
			return {
				chipClass: 'chip-warning',
				dotClass: 'bg-warning',
				textClass: 'text-warning'
			};
		case 'debug':
			return {
				chipClass: 'chip-subtle',
				dotClass: 'bg-subtle',
				textClass: 'text-subtle'
			};
		case 'info':
		default:
			return {
				chipClass: 'chip-brand',
				dotClass: 'bg-brand',
				textClass: 'text-brand'
			};
	}
}
