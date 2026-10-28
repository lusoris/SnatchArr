// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	eventToLiveFrame,
	liveFrameToEvent,
	levelTokenClasses,
	mergeLiveFrames,
	parseFrame,
	type LiveFrame
} from './live.js';

describe('live events helper', () => {
	it('parseFrame parses JSON payload and extracts fields', () => {
		const frame = parseFrame(
			'search_dispatched',
			JSON.stringify({
				id: 101,
				ts: '2026-10-20T12:00:00Z',
				level: 'info',
				title: 'Searching',
				detail: 'details'
			})
		);
		expect(frame.id).toBe(101);
		expect(frame.event).toBe('search_dispatched');
		expect(frame.title).toBe('Searching');
	});

	it('parseFrame falls back to lastEventId when id is missing from JSON', () => {
		const frame = parseFrame('search_dispatched', JSON.stringify({ title: 'No ID' }), '42');
		expect(frame.id).toBe(42);
	});

	it('parseFrame handles invalid JSON gracefully', () => {
		const frame = parseFrame('raw_event', 'non-json string');
		expect(frame.title).toBe('non-json string');
		expect(frame.event).toBe('raw_event');
	});

	it('eventToLiveFrame maps API event fields', () => {
		const frame = eventToLiveFrame({
			id: 200,
			instance_id: '00000000-0000-0000-0000-000000000001',
			ts: '2026-10-20T12:00:00Z',
			level: 'error',
			type: 'search_failed',
			title: 'Search failed',
			detail: 'timeout'
		});
		expect(frame.id).toBe(200);
		expect(frame.event).toBe('search_failed');
		expect(frame.level).toBe('error');
	});

	describe('mergeLiveFrames', () => {
		it('dedupes items by ID preserving first/newest occurrence', () => {
			const stream: LiveFrame[] = [
				{ id: 1, event: 'run_queued', ts: '2026-10-20T12:02:00Z', title: 'stream 1' },
				{ id: 2, event: 'run_started', ts: '2026-10-20T12:01:00Z', title: 'stream 2' }
			];
			const backlog: LiveFrame[] = [
				{ id: 1, event: 'run_queued', ts: '2026-10-20T12:02:00Z', title: 'backlog 1' },
				{ id: 3, event: 'run_finished', ts: '2026-10-20T12:00:00Z', title: 'backlog 3' }
			];

			const merged = mergeLiveFrames(stream, backlog);
			expect(merged).toHaveLength(3);
			expect(merged.map((m) => m.id)).toEqual([1, 2, 3]);
			expect(merged[0]?.title).toBe('stream 1');
		});

		it('preserves frames without ID without deduplicating them', () => {
			const stream: LiveFrame[] = [
				{ event: 'queue_item_struck', ts: '2026-10-20T12:02:00Z', title: 'strike 1' },
				{ event: 'queue_item_struck', ts: '2026-10-20T12:01:00Z', title: 'strike 2' }
			];
			const backlog: LiveFrame[] = [{ id: 10, event: 'run_finished', ts: '2026-10-20T12:00:00Z' }];

			const merged = mergeLiveFrames(stream, backlog);
			expect(merged).toHaveLength(3);
			expect(merged[0]?.title).toBe('strike 1');
			expect(merged[1]?.title).toBe('strike 2');
			expect(merged[2]?.id).toBe(10);
		});

		it('orders frames newest first by timestamp', () => {
			const f1: LiveFrame = { id: 1, event: 'e1', ts: '2026-10-20T10:00:00Z' };
			const f2: LiveFrame = { id: 2, event: 'e2', ts: '2026-10-20T12:00:00Z' };
			const f3: LiveFrame = { id: 3, event: 'e3', ts: '2026-10-20T11:00:00Z' };

			const merged = mergeLiveFrames([f1], [f2, f3]);
			expect(merged.map((m) => m.id)).toEqual([2, 3, 1]);
		});

		it('boundary caps output to 500 items', () => {
			const many: LiveFrame[] = Array.from({ length: 600 }, (_, i) => ({
				id: i + 1,
				event: 'tick',
				ts: new Date(1760000000000 - i * 1000).toISOString()
			}));

			const merged = mergeLiveFrames(many.slice(0, 300), many.slice(300));
			expect(merged).toHaveLength(500);
			expect(merged[0]?.id).toBe(1);
			expect(merged[499]?.id).toBe(500);
		});
	});

	describe('levelTokenClasses', () => {
		it('returns correct token-based class mappings', () => {
			expect(levelTokenClasses('error').textClass).toBe('text-danger');
			expect(levelTokenClasses('warn').textClass).toBe('text-warning');
			expect(levelTokenClasses('warning').textClass).toBe('text-warning');
			expect(levelTokenClasses('debug').textClass).toBe('text-subtle');
			expect(levelTokenClasses('info').textClass).toBe('text-brand');
			expect(levelTokenClasses(undefined).textClass).toBe('text-brand');
		});
	});
});

describe('liveFrameToEvent', () => {
	it('preserves associated event fields', () => {
		const frame: LiveFrame = {
			event: 'search_dispatched',
			id: 42,
			instance_id: 'i1',
			run_id: 'r1',
			level: 'warn',
			ts: '2026-10-28T12:00:00Z',
			title: 'Movie'
		};
		expect(liveFrameToEvent(frame)).toMatchObject(frame);
	});
	it('defaults an unknown level to info', () => {
		expect(liveFrameToEvent({ event: 'unknown', level: 'invalid' }).level).toBe('info');
	});
	it('retains absent associations and timestamps as empty values', () => {
		expect(liveFrameToEvent({ event: 'worker_contact' })).toMatchObject({
			id: 0,
			type: 'worker_contact',
			instance_id: '',
			ts: ''
		});
	});
});
