// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { expectStackedLabels, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import SeasonGatePanel from './SeasonGatePanel.svelte';

type Instance = components['schemas']['Instance'];
type GateSeries = components['schemas']['WatchawareGateSeries'];
type GateDecision = components['schemas']['WatchawareGateDecision'];

const sonarrInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000002',
	name: 'Primary Sonarr',
	kind: 'sonarr',
	base_url: 'http://sonarr.local:8989',
	source: 'manual',
	enabled: true,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const radarrInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000003',
	name: 'Primary Radarr',
	kind: 'radarr',
	base_url: 'http://radarr.local:7878',
	source: 'manual',
	enabled: true,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const sampleSeries: GateSeries = {
	instance_id: sonarrInstance.id,
	series_id: 42,
	tvdb_id: 1042,
	title: 'Gated Test Show',
	gate_season: 1,
	state: 'gated',
	added_before_gate: false,
	held_seasons: [2, 3],
	updated_at: '2026-10-25T12:00:00Z'
};

const sampleDecision: GateDecision = {
	id: '00000000-0000-0000-0000-000000000099',
	instance_id: sonarrInstance.id,
	series_id: 42,
	season_number: 2,
	action: 'unmonitor',
	would_act: false,
	reason: 'season after gate',
	decided_at: '2026-10-25T12:00:00Z'
};

describe('SeasonGatePanel', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
	});

	it('positive: renders observe note, series rows, held seasons text, and decisions with stacked labels', async () => {
		stubGet({
			'/instances': [sonarrInstance],
			'/watchaware/gate/series': [sampleSeries],
			'/watchaware/gate/decisions': [sampleDecision]
		});

		renderWithQuery(SeasonGatePanel);

		expect(await screen.findByText(m.watchaware_gate_observe_note())).toBeTruthy();
		const titles = await screen.findAllByText('Gated Test Show');
		expect(titles.length).toBe(2);
		expect(screen.getByText(m.watchaware_gate_state_gated())).toBeTruthy();
		expect(screen.getByText('2, 3')).toBeTruthy();

		expect(screen.getByText(m.watchaware_gate_action_unmonitor())).toBeTruthy();
		expect(screen.getByText(m.watchaware_gate_mode_observe())).toBeTruthy();
		expect(screen.getByText('season after gate')).toBeTruthy();

		const tables = screen.getAllByRole('table');
		expect(tables.length).toBe(2);
		expectStackedLabels(tables[0]);
		expectStackedLabels(tables[1]);
	});

	it('negative: API problem document renders ProblemNote', async () => {
		stubGet({
			'/instances': [sonarrInstance],
			'/watchaware/gate/series': new ProblemError({
				type: 'about:blank',
				title: 'Database Unavailable',
				status: 503
			}),
			'/watchaware/gate/decisions': []
		});

		renderWithQuery(SeasonGatePanel);

		expect(await screen.findByText(/Database Unavailable/)).toBeTruthy();
	});

	it('boundary: no Sonarr instances renders empty text', async () => {
		stubGet({
			'/instances': [radarrInstance],
			'/watchaware/gate/series': [],
			'/watchaware/gate/decisions': []
		});

		renderWithQuery(SeasonGatePanel);

		expect(await screen.findByText(m.watchaware_gate_no_sonarr())).toBeTruthy();
	});

	it('boundary: empty held seasons renders em dash', async () => {
		const emptyHeldSeries: GateSeries = {
			...sampleSeries,
			held_seasons: []
		};

		stubGet({
			'/instances': [sonarrInstance],
			'/watchaware/gate/series': [emptyHeldSeries],
			'/watchaware/gate/decisions': []
		});

		renderWithQuery(SeasonGatePanel);

		expect(await screen.findByText('Gated Test Show')).toBeTruthy();
		expect(screen.getByText('—')).toBeTruthy();
	});
});
