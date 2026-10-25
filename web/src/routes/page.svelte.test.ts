// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { logLevelLabel } from '#lib/logs.js';
import { m } from '#lib/paraglide/messages.js';
import {
	expectStackedLabels,
	failedInstance,
	forkInstance,
	renderWithQuery,
	stubGet,
	untestedInstance
} from '#lib/testing/harness.js';
import Dashboard from './+page.svelte';

function defaultDashboardStubs(overrides: Record<string, unknown> = {}) {
	return {
		'/instances': [forkInstance],
		'/hourly-caps': [],
		'/prowlarr/status': [],
		'/runs': [],
		'/events': [],
		'/schedules': [],
		...overrides
	};
}

describe('dashboard overview redo', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders overview heading matching nav label', async () => {
		stubGet(defaultDashboardStubs());
		renderWithQuery(Dashboard);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_overview() })).toBeTruthy();
	});

	it('renders KPI tiles for searches, fleet health, and Prowlarr headroom', async () => {
		stubGet(
			defaultDashboardStubs({
				'/hourly-caps': [
					{ scope: 'global', used: 25, cap: 100, resets_at: '2026-09-01T13:00:00Z' }
				],
				'/instances': [forkInstance],
				'/prowlarr/status': [
					{
						link_id: 'p1',
						name: 'Prowlarr Main',
						reachable: true,
						checked_at: '2026-09-01T12:00:00Z',
						indexers: [
							{ id: 1, name: 'Idx1', query_limit: 1000, used_queries: 100, exhausted: false },
							{ id: 2, name: 'Idx2', query_limit: 500, used_queries: 500, exhausted: true }
						]
					}
				]
			})
		);
		renderWithQuery(Dashboard);

		// Searches KPI
		expect(await screen.findByText(m.overview_kpi_searches())).toBeTruthy();
		expect(screen.getByText('25 / 100')).toBeTruthy();

		// Fleet KPI
		expect(screen.getByText(m.overview_kpi_instances())).toBeTruthy();
		expect(screen.getByText('1 / 1')).toBeTruthy();

		// Prowlarr KPI
		expect(screen.getByText(m.overview_kpi_prowlarr())).toBeTruthy();
		expect(screen.getByText('1 / 2')).toBeTruthy();
	});

	it('renders compact instance table with StatusIcon, name link, kind badge, and StaminaGauge', async () => {
		stubGet(
			defaultDashboardStubs({
				'/instances': [forkInstance],
				'/hourly-caps': [
					{
						scope: 'instance',
						instance_id: forkInstance.id,
						used: 12,
						cap: 50,
						resets_at: '2026-09-01T13:00:00Z'
					}
				],
				'/runs': [
					{
						id: 'r1',
						instance_id: forkInstance.id,
						kind: 'missing',
						status: 'done',
						dry_run: false,
						created_at: '2026-09-01T11:00:00Z',
						started_at: '2026-09-01T11:00:00Z',
						updated_at: '2026-09-01T11:05:00Z',
						searched_count: 3
					}
				],
				'/schedules': [
					{
						id: 's1',
						instance_id: forkInstance.id,
						name: 'Nightly',
						days: [1, 2],
						start: '02:00',
						end: '04:00',
						tz: 'UTC',
						action: 'pause',
						enabled: true,
						active: false,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				]
			})
		);
		renderWithQuery(Dashboard);

		const link = await screen.findByRole('link', { name: 'Library Readarr' });
		expect(link.getAttribute('href')).toBe(`/instances/${forkInstance.id}`);

		// Kind badge
		expect(screen.getByText('readarr · Bookshelf')).toBeTruthy();

		// StatusIcon
		expect(screen.getByRole('img', { name: m.instance_last_check() })).toBeTruthy();

		// Schedule window
		expect(screen.getAllByText('02:00–04:00 UTC').length).toBeGreaterThan(0);

		// Stamina gauge
		expect(screen.getByRole('meter', { name: forkInstance.name })).toBeTruthy();
	});

	it('shows error text and danger StatusIcon for a failed instance check', async () => {
		stubGet(defaultDashboardStubs({ '/instances': [failedInstance] }));
		renderWithQuery(Dashboard);

		const nameLink = await screen.findByRole('link', { name: 'Broken Sonarr' });
		expect(nameLink).toBeTruthy();

		expect(screen.getByText('Connection refused (ECONNREFUSED)')).toBeTruthy();
		expect(
			screen.getByRole('img', {
				name: `${m.instance_check_failed()}: Connection refused (ECONNREFUSED)`
			})
		).toBeTruthy();
	});

	it('shows idle StatusIcon for an untested instance', async () => {
		stubGet(defaultDashboardStubs({ '/instances': [untestedInstance] }));
		renderWithQuery(Dashboard);

		expect(await screen.findByRole('link', { name: 'Fresh Radarr' })).toBeTruthy();
		expect(screen.getByRole('img', { name: m.instance_never_tested() })).toBeTruthy();
	});

	it('triggers quick snatch missing behind ConfirmAction', async () => {
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		stubGet(defaultDashboardStubs());
		renderWithQuery(Dashboard);

		const missingBtn = await screen.findByRole('button', { name: m.quickie_missing() });
		await fireEvent.click(missingBtn);

		// ConfirmAction prompt opened
		expect(screen.getByText(m.overview_confirm_missing({ name: forkInstance.name }))).toBeTruthy();
		const confirmBtn = screen.getByRole('button', { name: m.confirm_yes() });
		await fireEvent.click(confirmBtn);

		expect(postSpy).toHaveBeenCalledWith('/instances/{instanceId}/runs', {
			params: { path: { instanceId: forkInstance.id } },
			body: { kind: 'missing' }
		});
	});

	it('triggers quick snatch upgrade behind ConfirmAction', async () => {
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		stubGet(defaultDashboardStubs());
		renderWithQuery(Dashboard);

		const upgradeBtn = await screen.findByRole('button', { name: m.quickie_upgrade() });
		await fireEvent.click(upgradeBtn);

		// ConfirmAction prompt opened
		expect(screen.getByText(m.overview_confirm_upgrade({ name: forkInstance.name }))).toBeTruthy();
		const confirmBtn = screen.getByRole('button', { name: m.confirm_yes() });
		await fireEvent.click(confirmBtn);

		expect(postSpy).toHaveBeenCalledWith('/instances/{instanceId}/runs', {
			params: { path: { instanceId: forkInstance.id } },
			body: { kind: 'upgrade' }
		});
	});

	it('renders 3-step onboarding guide when fleet has zero instances', async () => {
		stubGet(defaultDashboardStubs({ '/instances': [] }));
		renderWithQuery(Dashboard);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.overview_onboarding_title() })
		).toBeTruthy();
		expect(screen.getByRole('link', { name: m.overview_onboarding_step1_action() })).toBeTruthy();
		expect(screen.getByRole('link', { name: m.overview_onboarding_step2_action() })).toBeTruthy();
		expect(screen.getByRole('link', { name: m.overview_onboarding_step3_action() })).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('UIF1 failure isolation: an error on one query renders ProblemNote without breaking others', async () => {
		stubGet(
			defaultDashboardStubs({
				'/instances': new ProblemError({
					type: 'about:blank',
					status: 503,
					detail: 'instance store unreachable'
				}),
				'/hourly-caps': [{ scope: 'global', used: 10, cap: 100, resets_at: '2026-09-01T13:00:00Z' }]
			})
		);
		renderWithQuery(Dashboard);

		// ProblemNote for instance store is displayed
		const alerts = await screen.findAllByRole('alert');
		expect(alerts.length).toBeGreaterThan(0);
		expect(alerts.some((el) => el.textContent?.includes('instance store unreachable'))).toBe(true);

		// KPI tile for searches still renders!
		expect(screen.getByText(m.overview_kpi_searches())).toBeTruthy();
		expect(screen.getByText('10 / 100')).toBeTruthy();
	});

	it('renders 10 recent events in right column with link to /activity', async () => {
		const sampleEvents = Array.from({ length: 12 }, (_, i) => ({
			id: i + 1,
			instance_id: forkInstance.id,
			ts: '2026-09-01T12:00:00Z',
			level: 'info' as const,
			type: 'search_dispatched',
			title: `Event ${i + 1}`,
			detail: `Detail ${i + 1}`
		}));

		stubGet(defaultDashboardStubs({ '/events': sampleEvents }));
		renderWithQuery(Dashboard);

		expect(await screen.findByText(m.overview_recent_activity())).toBeTruthy();
		expect(screen.getByRole('link', { name: m.overview_view_all_activity() })).toBeTruthy();

		// Exactly 10 events shown with translated level StatusIcon
		expect(screen.getByText('Event 1')).toBeTruthy();
		expect(screen.getByText('Event 10')).toBeTruthy();
		expect(screen.queryByText('Event 11')).toBeNull();
		expect(screen.getAllByRole('img', { name: logLevelLabel('info') }).length).toBe(10);
	});

	it('renders active pause badge with instance target on schedules card and ignores non-pause or inactive', async () => {
		stubGet(
			defaultDashboardStubs({
				'/instances': [forkInstance],
				'/schedules': [
					{
						id: 's-pause-active',
						instance_id: forkInstance.id,
						name: 'Active Pause',
						days: [1],
						start: '02:00',
						end: '04:00',
						tz: 'UTC',
						action: 'pause',
						enabled: true,
						active: true,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					},
					{
						id: 's-pause-global',
						name: 'Global Pause',
						days: [2],
						start: '04:00',
						end: '06:00',
						tz: 'UTC',
						action: 'pause',
						enabled: true,
						active: true,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					},
					{
						id: 's-cap-active',
						instance_id: forkInstance.id,
						name: 'Active Cap',
						days: [3],
						start: '06:00',
						end: '08:00',
						tz: 'UTC',
						action: 'cap_override',
						cap_value: 5,
						enabled: true,
						active: true,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					},
					{
						id: 's-pause-inactive',
						instance_id: forkInstance.id,
						name: 'Inactive Pause',
						days: [4],
						start: '08:00',
						end: '10:00',
						tz: 'UTC',
						action: 'pause',
						enabled: true,
						active: false,
						created_at: '2026-01-01T00:00:00Z',
						updated_at: '2026-01-01T00:00:00Z'
					}
				]
			})
		);
		renderWithQuery(Dashboard);

		expect(await screen.findByText('Active Pause')).toBeTruthy();
		expect(screen.getByText(m.schedule_active_pause({ target: forkInstance.name }))).toBeTruthy();
		expect(
			screen.getByText(m.schedule_active_pause({ target: m.schedule_all_instances() }))
		).toBeTruthy();
		expect(screen.queryByText(m.schedule_active_pause({ target: 'Active Cap' }))).toBeNull();
	});

	it('renders Prowlarr card with headroom and exhausted counts when links are present', async () => {
		stubGet(
			defaultDashboardStubs({
				'/prowlarr/status': [
					{
						link_id: '00000000-0000-0000-0000-000000000020',
						name: 'Primary Prowlarr',
						reachable: true,
						checked_at: '2026-09-01T12:00:00Z',
						indexers: [
							{
								id: 1,
								name: 'Fast Indexer',
								query_limit: 1000,
								grab_limit: 100,
								limits_unit: 0,
								used_queries: 250,
								used_grabs: 10,
								exhausted: false
							},
							{
								id: 2,
								name: 'Capped Indexer',
								query_limit: 100,
								grab_limit: 50,
								limits_unit: 1,
								used_queries: 100,
								used_grabs: 20,
								exhausted: true
							}
						]
					}
				]
			})
		);
		renderWithQuery(Dashboard);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.prowlarr_dashboard_title() })
		).toBeTruthy();
		expect(screen.getByText('Primary Prowlarr')).toBeTruthy();
		expect(
			screen.getByText(
				new RegExp(
					`${m.prowlarr_headroom_count({ count: 1, total: 2 })} · ${m.prowlarr_exhausted_count({ count: 1 })}`
				)
			)
		).toBeTruthy();
		const meter = screen.getByRole('meter', {
			name: m.prowlarr_headroom_label({ name: 'Primary Prowlarr' })
		});
		expect(meter.getAttribute('aria-valuenow')).toBe('1');
		expect(meter.getAttribute('aria-valuemax')).toBe('2');
	});

	it('hides Prowlarr meter section when no Prowlarr links are configured', async () => {
		stubGet(defaultDashboardStubs({ '/prowlarr/status': [] }));
		renderWithQuery(Dashboard);

		await screen.findByRole('link', { name: 'Library Readarr' });
		expect(
			screen.queryByRole('heading', { level: 2, name: m.prowlarr_dashboard_title() })
		).toBeNull();
	});

	it('stacks fleet table rows below md with data-label matching th on every td except actions', async () => {
		stubGet(defaultDashboardStubs({ '/instances': [forkInstance] }));
		renderWithQuery(Dashboard);

		const table = await screen.findByRole('table', { name: m.overview_fleet_title() });
		expectStackedLabels(table);
	});

	it('renders no fleet table rows when instances list is empty', async () => {
		stubGet(defaultDashboardStubs({ '/instances': [] }));
		renderWithQuery(Dashboard);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.overview_onboarding_title() })
		).toBeTruthy();
		expect(screen.queryByRole('table', { name: m.overview_fleet_title() })).toBeNull();
	});
});
