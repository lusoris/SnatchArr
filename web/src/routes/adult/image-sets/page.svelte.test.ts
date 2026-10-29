// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { toast } from 'svelte-sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import AdultImageSetsPage from './+page.svelte';

type AdultImageSet = components['schemas']['AdultImageSet'];
type AdultImageSetDetail = components['schemas']['AdultImageSetDetail'];

function makeSets(count: number): AdultImageSet[] {
	return Array.from({ length: count }, (_, i) => ({
		id: `018f0000-0000-7000-8000-${String(i + 1).padStart(12, '0')}`,
		title: `Image Set ${i + 1}`,
		directory_path: `/media/images/set-${i + 1}`,
		item_count: 1,
		total_size_bytes: 2048,
		created_at: '2026-10-27T10:00:00Z',
		updated_at: '2026-10-27T10:00:00Z'
	}));
}

const sampleSet: AdultImageSet = {
	id: '018f0000-0000-7000-8000-000000000010',
	title: 'Sample Image Set',
	directory_path: '/media/images/sample-image-set',
	item_count: 1,
	total_size_bytes: 2048,
	created_at: '2026-10-27T10:00:00Z',
	updated_at: '2026-10-27T10:00:00Z'
};

const sampleDetail: AdultImageSetDetail = {
	...sampleSet,
	ignore_file_present: true,
	items: [
		{
			id: '018f0000-0000-7000-8000-000000000020',
			image_set_id: sampleSet.id,
			file_path: '/media/images/sample-image-set/001.png',
			file_name: '001.png',
			file_size_bytes: 2048,
			mime_type: 'image/png',
			sha256_checksum: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
			sort_order: 0,
			created_at: '2026-10-27T10:00:00Z'
		}
	]
};

describe('adult image sets (/adult/image-sets)', () => {
	afterEach(() => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		vi.restoreAllMocks();
	});

	it('positive: renders image sets list, ignore file status, triggers scan, and renders item thumbnails', async () => {
		stubGet({
			'/adult/image-sets': { items: [sampleSet], total: 1 },
			'/adult/image-sets/{setId}': sampleDetail
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({
			data: { sets_found: 1, items_found: 1, ignore_file_present: true }
		} as never);
		const toastSpy = vi.spyOn(toast, 'success');

		renderWithQuery(AdultImageSetsPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.adult_image_sets_title() })
		).toBeTruthy();
		expect(await screen.findByText('Sample Image Set')).toBeTruthy();
		expect(await screen.findByText(m.adult_image_sets_ignore_file_present())).toBeTruthy();

		// Check thumbnail image
		const img = await screen.findByRole('img', { name: '001.png' });
		expect(img).toBeTruthy();
		expect(img.getAttribute('src')).toBe(
			`/api/v1/adult/image-items/${sampleDetail.items[0].id}/file`
		);

		// Trigger scan
		const scanBtn = screen.getByRole('button', { name: m.adult_image_sets_scan_now() });
		await fireEvent.click(scanBtn);

		await waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/adult/scan');
		});
		expect(toastSpy).toHaveBeenCalledWith(m.adult_image_sets_scanned_toast({ sets: 1, items: 1 }));
	});

	it('negative: hides scan admin button for viewer user', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		stubGet({
			'/adult/image-sets': { items: [sampleSet], total: 1 },
			'/adult/image-sets/{setId}': sampleDetail
		});

		renderWithQuery(AdultImageSetsPage);

		expect(await screen.findByText('Sample Image Set')).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.adult_image_sets_scan_now() })).toBeNull();
	});

	it('boundary: renders empty placeholder when no image sets exist', async () => {
		stubGet({
			'/adult/image-sets': { items: [], total: 0 }
		});

		renderWithQuery(AdultImageSetsPage);

		expect(await screen.findByText(m.adult_image_sets_none())).toBeTruthy();
	});

	it('positive: paginates image sets (>50 items) with load more button and limit/offset query', async () => {
		const sets51 = makeSets(51);
		const getSpy = vi.spyOn(api, 'GET').mockImplementation(async (path: string, opts?: unknown) => {
			const options = opts as
				{ params?: { query?: { offset?: number; limit?: number } } } | undefined;
			if (path === '/adult/image-sets') {
				const offset = options?.params?.query?.offset ?? 0;
				const limit = options?.params?.query?.limit ?? 50;
				return {
					data: {
						items: sets51.slice(offset, offset + limit),
						total: sets51.length
					}
				} as never;
			}
			if (path === '/adult/image-sets/{setId}') {
				return { data: sampleDetail } as never;
			}
			throw new Error(`unexpected GET ${path}`);
		});

		renderWithQuery(AdultImageSetsPage);

		// First render shows 50 sets + "50 of 51" + button
		expect(await screen.findByText('Image Set 1')).toBeTruthy();
		expect(screen.getByText('Image Set 50')).toBeTruthy();
		expect(screen.queryByText('Image Set 51')).toBeNull();
		expect(screen.getByText(m.adult_image_sets_shown({ shown: 50, total: 51 }))).toBeTruthy();

		const loadMoreBtn = screen.getByRole('button', { name: m.adult_image_sets_load_more() });
		expect(loadMoreBtn).toBeTruthy();

		await fireEvent.click(loadMoreBtn);

		// Click button -> 51st set title visible, "51 of 51", button gone
		expect(await screen.findByText('Image Set 51')).toBeTruthy();
		expect(screen.getByText(m.adult_image_sets_shown({ shown: 51, total: 51 }))).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.adult_image_sets_load_more() })).toBeNull();

		// Assert GET called with offset 0 then offset 50, limit 50
		expect(getSpy).toHaveBeenCalledWith('/adult/image-sets', {
			params: { query: { limit: 50, offset: 0 } }
		});
		expect(getSpy).toHaveBeenCalledWith('/adult/image-sets', {
			params: { query: { limit: 50, offset: 50 } }
		});
	});

	it('boundary: total 50 shows 50 of 50 and no load more button', async () => {
		const sets50 = makeSets(50);
		vi.spyOn(api, 'GET').mockImplementation(async (path: string, opts?: unknown) => {
			const options = opts as
				{ params?: { query?: { offset?: number; limit?: number } } } | undefined;
			if (path === '/adult/image-sets') {
				const offset = options?.params?.query?.offset ?? 0;
				const limit = options?.params?.query?.limit ?? 50;
				return {
					data: {
						items: sets50.slice(offset, offset + limit),
						total: sets50.length
					}
				} as never;
			}
			if (path === '/adult/image-sets/{setId}') {
				return { data: sampleDetail } as never;
			}
			throw new Error(`unexpected GET ${path}`);
		});

		renderWithQuery(AdultImageSetsPage);

		expect(await screen.findByText('Image Set 1')).toBeTruthy();
		expect(screen.getByText('Image Set 50')).toBeTruthy();
		expect(screen.getByText(m.adult_image_sets_shown({ shown: 50, total: 50 }))).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.adult_image_sets_load_more() })).toBeNull();
	});

	it('positive: renders total shown line with aria-live="polite"', async () => {
		const sets = makeSets(10);
		vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
			if (path === '/adult/image-sets') {
				return { data: { items: sets, total: 10 } } as never;
			}
			if (path === '/adult/image-sets/{setId}') {
				return { data: sampleDetail } as never;
			}
			throw new Error(`unexpected GET ${path}`);
		});

		renderWithQuery(AdultImageSetsPage);

		const line = await screen.findByText(m.adult_image_sets_shown({ shown: 10, total: 10 }));
		expect(line).toBeTruthy();
		expect(line.getAttribute('aria-live')).toBe('polite');
	});

	it('negative: second page request error keeps first 50 rendered and surfaces error', async () => {
		const sets51 = makeSets(51);
		vi.spyOn(api, 'GET').mockImplementation(async (path: string, opts?: unknown) => {
			const options = opts as
				{ params?: { query?: { offset?: number; limit?: number } } } | undefined;
			if (path === '/adult/image-sets') {
				const offset = options?.params?.query?.offset ?? 0;
				if (offset > 0) {
					throw new ProblemError({
						type: 'about:blank',
						status: 500,
						detail: 'second page failure'
					});
				}
				return {
					data: {
						items: sets51.slice(0, 50),
						total: sets51.length
					}
				} as never;
			}
			if (path === '/adult/image-sets/{setId}') {
				return { data: sampleDetail } as never;
			}
			throw new Error(`unexpected GET ${path}`);
		});

		renderWithQuery(AdultImageSetsPage);

		expect(await screen.findByText('Image Set 1')).toBeTruthy();
		expect(screen.getByText('Image Set 50')).toBeTruthy();
		const loadMoreBtn = screen.getByRole('button', { name: m.adult_image_sets_load_more() });

		await fireEvent.click(loadMoreBtn);

		// First 50 stay rendered
		expect(await screen.findByText('Image Set 1')).toBeTruthy();
		expect(screen.getByText('Image Set 50')).toBeTruthy();
		// Error surfaced in ProblemNote
		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('second page failure');
	});
});
