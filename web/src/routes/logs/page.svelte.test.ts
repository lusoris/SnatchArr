// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import { MockEventSource } from '#lib/testing/sse.js';
import LogsPage from './+page.svelte';

type LogPage = components['schemas']['LogPage'];

const samplePage: LogPage = {
	capacity: 2000,
	dropped: 3,
	records: [
		{
			seq: 2,
			time: '2026-10-17T12:00:01Z',
			level: 'error',
			source: 'api',
			message: 'sonarr unreachable',
			attrs: [{ key: 'api_key', value: '[REDACTED]' }]
		},
		{
			seq: 1,
			time: '2026-10-17T12:00:00Z',
			level: 'info',
			source: 'api',
			message: 'planner tick',
			attrs: []
		}
	]
};

function asRole(role: 'admin' | 'viewer') {
	session.user = { id: 'u1', username: 'u1', role } as never;
}

describe('logs', () => {
	beforeEach(() => {
		MockEventSource.instances = [];
	});

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('positive lists the page and adds streamed records on top', async () => {
		asRole('admin');
		stubGet({ '/logs': samplePage });
		const { container } = renderWithQuery(LogsPage);

		const log = await screen.findByRole('log', { name: m.logs_title() });
		expect(within(log).getByText('sonarr unreachable')).toBeTruthy();
		expect(within(log).getByText('api_key=[REDACTED]')).toBeTruthy();
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
		expect(screen.getByText(m.logs_window({ shown: 2, capacity: 2000, dropped: 3 }))).toBeTruthy();

		const es = MockEventSource.instances[0];
		expect(es?.url).toBe('/api/v1/logs/stream');
		es.emit('open', {});
		es.emit('message', {
			type: 'message',
			data: JSON.stringify({
				seq: 3,
				time: '2026-10-17T12:00:02Z',
				level: 'warn',
				source: 'api',
				message: 'budget at 80 %',
				attrs: []
			})
		});
		// A replayed record the page already holds and one below the minimum level stay out.
		es.emit('message', { type: 'message', data: JSON.stringify(samplePage.records[0]) });
		es.emit('message', {
			type: 'message',
			data: JSON.stringify({
				seq: 4,
				time: 't',
				level: 'debug',
				source: 'api',
				message: 'cache warm',
				attrs: []
			})
		});

		await waitFor(() => expect(within(log).getByText('budget at 80 %')).toBeTruthy());
		const items = within(log).getAllByRole('listitem');
		expect(items).toHaveLength(3);
		expect(items[0].textContent).toContain('budget at 80 %');
		expect(within(log).queryByText('cache warm')).toBeNull();
	});

	it('positive asks the API again with the chosen minimum level', async () => {
		asRole('admin');
		const get = stubGet({ '/logs': samplePage });
		renderWithQuery(LogsPage);
		await screen.findByRole('log');

		await fireEvent.change(screen.getByLabelText(m.logs_level()), { target: { value: 'error' } });

		await waitFor(() =>
			expect(get).toHaveBeenLastCalledWith('/logs', {
				params: { query: { level: 'error', q: undefined, source: undefined, limit: 500 } }
			})
		);
	});

	it('negative shows the problem document when the API refuses', async () => {
		asRole('admin');
		stubGet({
			'/logs': new ProblemError({ type: 'about:blank', status: 403, detail: 'admin role required' })
		});
		renderWithQuery(LogsPage);

		expect((await screen.findByRole('alert')).textContent).toContain('admin role required');
		expect(screen.queryByRole('log')).toBeNull();
	});

	it('negative a viewer gets a note, no request and no stream', () => {
		asRole('viewer');
		const get = stubGet({});
		renderWithQuery(LogsPage);

		expect(screen.getByRole('alert').textContent).toContain(m.logs_admin_only());
		expect(get).not.toHaveBeenCalled();
		expect(MockEventSource.instances).toHaveLength(0);
	});

	it('boundary an empty ring says no record matches', async () => {
		asRole('admin');
		stubGet({ '/logs': { capacity: 100, dropped: 0, records: [] } });
		renderWithQuery(LogsPage);

		expect(await screen.findByText(m.logs_empty())).toBeTruthy();
		expect(within(screen.getByRole('log')).queryAllByRole('listitem')).toHaveLength(0);
	});
});
