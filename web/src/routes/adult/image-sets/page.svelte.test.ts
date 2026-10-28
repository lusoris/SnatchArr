// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor } from '@testing-library/svelte';
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
			'/adult/image-sets': [sampleSet],
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
			'/adult/image-sets': [sampleSet],
			'/adult/image-sets/{setId}': sampleDetail
		});

		renderWithQuery(AdultImageSetsPage);

		expect(await screen.findByText('Sample Image Set')).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.adult_image_sets_scan_now() })).toBeNull();
	});

	it('boundary: renders empty placeholder when no image sets exist', async () => {
		stubGet({
			'/adult/image-sets': []
		});

		renderWithQuery(AdultImageSetsPage);

		expect(await screen.findByText(m.adult_image_sets_none())).toBeTruthy();
	});
});
