// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { expectNoAxeViolations, renderWithQuery } from '#lib/testing/harness.js';
import AdultCatalogList, { type AdultCatalogItem } from './AdultCatalogList.svelte';

interface TestItem extends AdultCatalogItem {
	id: string;
	name: string;
	is_favorite: boolean;
	ref_count?: number;
	tag?: string;
}

const sampleItems: TestItem[] = [
	{
		id: '018f0000-0000-7000-8000-000000000001',
		name: 'Alpha Item',
		is_favorite: true,
		ref_count: 5,
		tag: 'Special'
	},
	{
		id: '018f0000-0000-7000-8000-000000000002',
		name: 'Beta Item',
		is_favorite: false,
		ref_count: 0
	}
];

const extraTextSnippet = createRawSnippet<[TestItem]>((getItem) => ({
	render: () => {
		const item = getItem();
		if (!item.tag) return '<span></span>';
		return `<span data-testid="tag">(${item.tag})</span>`;
	}
}));

function createTestProps(
	overrides: Partial<Parameters<typeof AdultCatalogList<TestItem>>[0]> = {}
) {
	return {
		title: 'Catalog Title',
		description: 'Catalog Description',
		favoriteOnly: 'Favorite Only',
		none: 'No catalog items found',
		favoriteBadge: 'Favorite',
		refCount: (count: number) => `${count} references`,
		filterInputId: 'test-filter-favorites',
		queryKey: (fav?: boolean) => ['test-catalog', fav],
		fetch: vi.fn(async (fav?: boolean) => {
			if (fav) return sampleItems.filter((i) => i.is_favorite);
			return sampleItems;
		}),
		...overrides
	};
}

describe('AdultCatalogList', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: renders items, favorite badge, ref count, extraText, filters, and passes axe audit', async () => {
		const props = createTestProps({ extraText: extraTextSnippet });
		const { container } = renderWithQuery(AdultCatalogList<TestItem>, props);

		expect(await screen.findByRole('heading', { level: 1, name: 'Catalog Title' })).toBeTruthy();
		expect(await screen.findByText('Alpha Item')).toBeTruthy();
		expect(screen.getByText('(Special)')).toBeTruthy();
		expect(screen.getByText('Beta Item')).toBeTruthy();
		expect(screen.getByText('Favorite')).toBeTruthy();
		expect(screen.getByText('5 references')).toBeTruthy();
		expect(screen.getByText('0 references')).toBeTruthy();

		// Filter checkbox exists and has accessible label
		const checkbox = screen.getByRole('checkbox', { name: 'Favorite Only' });
		expect(checkbox).toBeTruthy();
		await fireEvent.click(checkbox);

		await expectNoAxeViolations(container);
	});

	it('negative: renders empty state when no items exist and passes axe audit', async () => {
		const props = createTestProps({
			fetch: vi.fn(async () => [])
		});
		const { container } = renderWithQuery(AdultCatalogList<TestItem>, props);

		expect(await screen.findByText('No catalog items found')).toBeTruthy();

		await expectNoAxeViolations(container);
	});

	it('boundary: renders 0 references when ref_count is undefined and passes axe audit', async () => {
		const itemMissingRefCount: TestItem = {
			id: '018f0000-0000-7000-8000-000000000003',
			name: 'Gamma Item',
			is_favorite: false
			// ref_count is omitted / undefined
		};

		const props = createTestProps({
			fetch: vi.fn(async () => [itemMissingRefCount])
		});
		const { container } = renderWithQuery(AdultCatalogList<TestItem>, props);

		expect(await screen.findByText('Gamma Item')).toBeTruthy();
		expect(screen.getByText('0 references')).toBeTruthy();

		await expectNoAxeViolations(container);
	});
});
