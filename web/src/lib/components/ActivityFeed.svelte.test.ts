// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import { MockEventSource } from '#lib/testing/sse.js';
import ActivityFeed from './ActivityFeed.svelte';

type Event = components['schemas']['Event'];
type Instance = components['schemas']['Instance'];
type SystemStatus = components['schemas']['SystemStatus'];
type LogPage = components['schemas']['LogPage'];

const sampleInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000001',
	name: 'Sonarr Main',
	kind: 'sonarr',
	base_url: 'http://localhost:8989',
	enabled: true,
	source: 'manual',
	created_at: '2026-10-01T00:00:00Z',
	updated_at: '2026-10-01T00:00:00Z'
};

const sampleEvent1: Event = {
	id: 101,
	run_id: '11111111-1111-1111-1111-111111111111',
	instance_id: sampleInstance.id,
	ts: '2026-10-20T12:00:00Z',
	level: 'info',
	type: 'run_started',
	title: 'Run Started',
	detail: 'Starting snatch run'
};

const sampleEvent2: Event = {
	id: 102,
	run_id: '11111111-1111-1111-1111-111111111111',
	instance_id: sampleInstance.id,
	ts: '2026-10-20T12:00:10Z',
	level: 'info',
	type: 'search_dispatched',
	title: 'Searching Sonarr',
	detail: '10 items'
};

const sampleSingleEvent: Event = {
	id: 201,
	instance_id: sampleInstance.id,
	ts: '2026-10-20T12:30:00Z',
	level: 'warn',
	type: 'queue_item_struck',
	title: 'Stalled download struck',
	detail: 'Stalled for 60m'
};

const sampleStatus: SystemStatus = {
	name: 'SnatchArr',
	version: '0.1.0',
	commit: 'abc1234',
	build_date: '2026-10-20',
	profile: 'dev',
	setup_complete: true,
	worker_last_contact_at: '2026-10-20T12:30:00Z'
};

const sampleLogs: LogPage = {
	capacity: 500,
	dropped: 0,
	records: [
		{
			seq: 1,
			time: '2026-10-20T12:00:00Z',
			level: 'info',
			source: 'api',
			message: 'API server ready',
			attrs: [{ key: 'port', value: '8080' }]
		}
	]
};

const navMocks = vi.hoisted(() => ({ goto: vi.fn() }));
vi.mock('$app/navigation', () => ({ goto: navMocks.goto }));

const pageMock = vi.hoisted(() => ({
	url: new URL('http://localhost/activity')
}));
vi.mock('$app/state', () => ({
	page: pageMock
}));

describe('ActivityFeed', () => {
	beforeEach(() => {
		pageMock.url = new URL('http://localhost/activity');
		MockEventSource.instances = [];
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		stubGet({
			'/events': [sampleEvent1, sampleEvent2, sampleSingleEvent],
			'/instances': [sampleInstance],
			'/schedules': [],
			'/system/status': sampleStatus,
			'/logs': sampleLogs
		});
	});

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('renders events backlog, grouped run card, and single event row', async () => {
		renderWithQuery(ActivityFeed);

		// Log role is visible on feed container
		const log = await screen.findByRole('log');
		expect(log).toBeTruthy();

		// Run summary card for consecutive run1 events
		expect(await screen.findByText(/Run 11111111/)).toBeTruthy();
		expect(screen.getByText(/1 dispatches, 10 items/)).toBeTruthy();

		// Single event row is visible
		expect(screen.getByText('Stalled download struck')).toBeTruthy();
	});

	it('expands group card to reveal child events', async () => {
		renderWithQuery(ActivityFeed);

		const runBtn = await screen.findByRole('button', { name: /Run 11111111/ });
		expect(runBtn.getAttribute('aria-expanded')).toBe('false');

		await fireEvent.click(runBtn);
		expect(runBtn.getAttribute('aria-expanded')).toBe('true');

		expect(screen.getByText('Starting snatch run')).toBeTruthy();
		expect(screen.getByText('Searching Sonarr')).toBeTruthy();
	});

	it('expands single event to show JSON detail', async () => {
		renderWithQuery(ActivityFeed);

		const rowBtn = await screen.findByRole('button', { name: /Stalled download struck/ });
		expect(rowBtn.getAttribute('aria-expanded')).toBe('false');

		await fireEvent.click(rowBtn);
		expect(rowBtn.getAttribute('aria-expanded')).toBe('true');

		expect(screen.getByText(/"queue_item_struck"/)).toBeTruthy();
	});

	it('renders segmented control for admin and allows switching to logs', async () => {
		renderWithQuery(ActivityFeed);

		const eventsTab = screen.getByRole('tab', { name: m.activity_source_events() });
		const logTab = screen.getByRole('tab', { name: m.activity_source_log() });

		expect(eventsTab.getAttribute('aria-selected')).toBe('true');
		expect(logTab.getAttribute('aria-selected')).toBe('false');

		await fireEvent.click(logTab);
		expect(navMocks.goto).toHaveBeenCalledWith('/activity?source=log', {
			replaceState: false,
			reset: false
		});
	});

	it('renders server logs when source=log and admin', async () => {
		pageMock.url = new URL('http://localhost/activity?source=log');
		renderWithQuery(ActivityFeed);

		expect(await screen.findByText('API server ready')).toBeTruthy();
		expect(screen.getByText('port=8080')).toBeTruthy();
	});

	it('omits server log tab for viewers and never queries logs', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		pageMock.url = new URL('http://localhost/activity?source=log');
		renderWithQuery(ActivityFeed);

		expect(screen.queryByRole('tab', { name: m.activity_source_log() })).toBeNull();
		// Falls back to events
		expect(await screen.findByText('Stalled download struck')).toBeTruthy();
	});

	it('renders empty state with worker status and schedules when backlog is empty', async () => {
		stubGet({
			'/events': [],
			'/instances': [sampleInstance],
			'/schedules': [],
			'/system/status': sampleStatus,
			'/logs': sampleLogs
		});
		renderWithQuery(ActivityFeed);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.live_empty_title() })
		).toBeTruthy();
		expect(screen.getByText(m.live_worker_label())).toBeTruthy();
		expect(screen.getByText(m.live_schedules_label())).toBeTruthy();
	});

	it('renders problem note when events request fails', async () => {
		stubGet({
			'/events': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'Database unreachable'
			}),
			'/instances': [sampleInstance],
			'/schedules': [],
			'/system/status': sampleStatus,
			'/logs': sampleLogs
		});
		renderWithQuery(ActivityFeed);

		expect(await screen.findByRole('alert')).toBeTruthy();
	});

	it('fixedInstanceId prop omits instance filter dropdown and bounds desktop log height', async () => {
		renderWithQuery(ActivityFeed, { fixedInstanceId: sampleInstance.id });

		expect(screen.queryByLabelText(m.activity_filter_instance())).toBeNull();
		const log = await screen.findByRole('log');
		expect(log.className).toContain('h-[20rem]');
		expect(log.className).toContain('md:h-[16rem]');
	});
});
