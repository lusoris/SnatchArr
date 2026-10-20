// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import http from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { mockSignedInApi } from './mocks.js';

const instanceA = {
	id: '00000000-0000-0000-0000-0000000000a1',
	kind: 'sonarr',
	name: 'Sonarr Main',
	base_url: 'http://sonarr.local',
	enabled: true,
	source: 'manual',
	created_at: '2026-10-20T00:00:00Z',
	updated_at: '2026-10-20T00:00:00Z'
};
const instanceB = {
	...instanceA,
	id: '00000000-0000-0000-0000-0000000000b2',
	name: 'Radarr 4K',
	kind: 'radarr'
};
const pauseA = {
	id: '00000000-0000-0000-0000-0000000000c3',
	instance_id: instanceA.id,
	name: 'Night pause',
	days: [0, 1, 2, 3, 4, 5, 6],
	start: '00:00',
	end: '06:00',
	tz: 'UTC',
	action: 'pause',
	enabled: true,
	active: true,
	created_at: '2026-10-20T00:00:00Z',
	updated_at: '2026-10-20T00:00:00Z'
};

test.describe('live', () => {
	test('live backlog: mocked /events returns rows -> rows visible before any SSE frame', async ({
		page
	}) => {
		await mockSignedInApi(page);
		await page.route('**/api/v1/instances', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([instanceA])
			})
		);

		await page.route('**/api/v1/system/status', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					name: 'SnatchArr',
					version: '0.1.0',
					commit: 'abc1234',
					build_date: '2026-10-20',
					profile: 'dev',
					setup_complete: true,
					worker_last_contact_at: '2026-10-20T12:30:00Z'
				})
			})
		);

		await page.route('**/api/v1/schedules', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);

		await page.route('**/api/v1/events?*', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([
					{
						id: 42,
						ts: '2026-10-20T12:00:00Z',
						level: 'info',
						type: 'search_dispatched',
						instance_id: instanceA.id,
						title: 'Backlog Item Inception',
						detail: 'Searched 1 item'
					}
				])
			})
		);

		// Stream stalls and never responds / sends frames
		await page.route('**/api/v1/events/stream', async () => {
			await new Promise(() => {});
		});

		await page.goto('/live');

		const log = page.getByRole('log');
		await expect(log).toBeVisible({ timeout: 5000 });
		await expect(page.getByText('Backlog Item Inception')).toBeVisible();
		await expect(page.getByText('search_dispatched')).toBeVisible();
		const instLink = log.getByRole('link', { name: 'Sonarr Main' });
		await expect(instLink).toHaveAttribute('href', `/instances/${instanceA.id}`);
		await page.screenshot({ path: test.info().outputPath('live-backlog.png'), fullPage: true });
	});

	test('live empty state visible with empty backlog', async ({ page }) => {
		await mockSignedInApi(page);
		await page.route('**/api/v1/instances', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([instanceA, instanceB])
			})
		);

		await page.route('**/api/v1/system/status', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					name: 'SnatchArr',
					version: '0.1.0',
					commit: 'abc1234',
					build_date: '2026-10-20',
					profile: 'dev',
					setup_complete: true
				})
			})
		);

		await page.route('**/api/v1/schedules', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([pauseA])
			})
		);

		await page.route('**/api/v1/events?*', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);

		// Stream stalls and never responds / sends frames
		await page.route('**/api/v1/events/stream', async () => {
			await new Promise(() => {});
		});

		await page.goto('/live');

		await expect(
			page.getByRole('heading', {
				level: 2,
				name: /No live events yet|Noch keine Live-Ereignisse/i
			})
		).toBeVisible();
		await expect(page.getByText(/Worker status|Worker-Status/i)).toBeVisible();
		await expect(page.getByText(/Scheduled cycles|Geplante Zyklen/i)).toBeVisible();
		const cycles = page.getByRole('listitem').filter({ hasText: 'Sonarr Main' });
		await expect(cycles).toContainText(/Paused by schedule/);
		await expect(page.getByRole('listitem').filter({ hasText: 'Radarr 4K' })).toContainText(
			/Continuous/
		);
		const main = page.locator('#main');
		await expect(main.getByRole('link', { name: /Event history|Ereignisverlauf/i })).toBeVisible();
		await expect(main.getByRole('link', { name: /Schedules|Zeitpläne/i })).toBeVisible();
		await page.screenshot({ path: test.info().outputPath('live-empty.png'), fullPage: true });
	});

	test('reconnect: mocked stream that ends after 2 s -> client opens a second stream request within 10 s and status returns to connected', async ({
		page
	}) => {
		await mockSignedInApi(page);

		await page.route('**/api/v1/system/status', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					name: 'SnatchArr',
					version: '0.1.0',
					commit: 'abc1234',
					build_date: '2026-10-20',
					profile: 'dev',
					setup_complete: true
				})
			})
		);

		await page.route('**/api/v1/schedules', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);

		await page.route('**/api/v1/events?*', (route) =>
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify([])
			})
		);

		let streamRequests = 0;
		const sseServer = http.createServer((_req, res) => {
			streamRequests++;
			res.writeHead(200, {
				'Content-Type': 'text/event-stream',
				'Cache-Control': 'no-cache',
				Connection: 'keep-alive',
				'Access-Control-Allow-Origin': '*',
				'Access-Control-Allow-Credentials': 'true'
			});
			res.write(': connected\n\n');
			if (streamRequests === 1) {
				setTimeout(() => {
					res.end();
				}, 2000);
			}
		});

		await new Promise<void>((resolve) => sseServer.listen(0, '127.0.0.1', resolve));
		const port = (sseServer.address() as AddressInfo).port;

		try {
			await page.route('**/api/v1/events/stream', async (route) => {
				await route.continue({ url: `http://127.0.0.1:${port}/api/v1/events/stream` });
			});

			await page.goto('/live');

			// Initially connects
			const status = page.getByRole('status');
			await expect(status).toHaveText(/Connected|Verbunden/, { timeout: 5000 });

			// After 2s stream terminates -> status becomes Reconnecting...
			await expect(status).toHaveText(/Reconnecting…|Verbinde erneut …/, { timeout: 5000 });

			// Within 10s, client opens second stream request and status returns to Connected
			await expect.poll(() => streamRequests, { timeout: 10_000 }).toBeGreaterThanOrEqual(2);
			await expect(status).toHaveText(/Connected|Verbunden/, { timeout: 10_000 });
		} finally {
			sseServer.closeAllConnections();
			await new Promise<void>((resolve) => sseServer.close(() => resolve()));
		}
	});
});
