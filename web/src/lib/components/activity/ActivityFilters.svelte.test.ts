// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import ActivityFilters from './ActivityFilters.svelte';

describe('ActivityFilters', () => {
	const defaultProps = {
		activeSource: 'events' as const,
		effectiveInstance: '',
		fixedInstanceId: undefined,
		filterLevel: '',
		filterType: '',
		filterFrom: '',
		filterTo: '',
		filterQ: '',
		instances: [{ id: 'inst-1', name: 'Sonarr' }],
		hasActiveFilters: false,
		onupdate: vi.fn(),
		onclear: vi.fn()
	};

	it('positive: changing filter input triggers onupdate callback', async () => {
		const onupdate = vi.fn();
		render(ActivityFilters, {
			props: { ...defaultProps, onupdate }
		});

		const levelSelect = screen.getByLabelText(m.activity_filter_level());
		await fireEvent.change(levelSelect, { target: { value: 'error' } });
		expect(onupdate).toHaveBeenCalledWith({ level: 'error' });

		const searchInput = screen.getByPlaceholderText(m.activity_filter_search());
		await fireEvent.input(searchInput, { target: { value: 'test' } });
		expect(onupdate).toHaveBeenCalledWith({ q: 'test' }, true);
	});

	it('positive: filter disclosure opens on click and sets aria-expanded true', async () => {
		render(ActivityFilters, { props: defaultProps });

		const toggleBtn = screen.getByRole('button', { name: new RegExp(m.activity_filter_toggle()) });
		expect(toggleBtn.getAttribute('aria-expanded')).toBe('false');

		await fireEvent.click(toggleBtn);
		expect(toggleBtn.getAttribute('aria-expanded')).toBe('true');
		const disclosed = screen.getByTestId('disclosed-filters');
		expect(disclosed.className).toContain('flex');
		expect(disclosed.className).not.toContain('hidden');
	});

	it('negative: filter disclosure is closed by default on phone', () => {
		render(ActivityFilters, { props: defaultProps });

		const toggleBtn = screen.getByRole('button', { name: new RegExp(m.activity_filter_toggle()) });
		expect(toggleBtn.getAttribute('aria-expanded')).toBe('false');
		const disclosed = screen.getByTestId('disclosed-filters');
		expect(disclosed.className).toContain('hidden');
	});

	it('boundary: active filter count badge is hidden at 0 and visible at 1', () => {
		const { rerender } = render(ActivityFilters, {
			props: { ...defaultProps, filterLevel: '' }
		});

		expect(screen.queryByTestId('filter-badge')).toBeNull();

		rerender({ ...defaultProps, filterLevel: 'warn' });
		const badge = screen.getByTestId('filter-badge');
		expect(badge).toBeTruthy();
		expect(badge.textContent?.trim()).toBe('1');
	});

	it('negative: clear filters button is hidden when hasActiveFilters is false', () => {
		render(ActivityFilters, {
			props: { ...defaultProps, hasActiveFilters: false }
		});

		expect(screen.queryByRole('button', { name: m.activity_clear_filters() })).toBeNull();
	});

	it('boundary: clear filters button is visible when hasActiveFilters is true and calls onclear', async () => {
		const onclear = vi.fn();
		render(ActivityFilters, {
			props: { ...defaultProps, hasActiveFilters: true, onclear }
		});

		const clearBtn = screen.getByRole('button', { name: m.activity_clear_filters() });
		expect(clearBtn).toBeTruthy();

		await fireEvent.click(clearBtn);
		expect(onclear).toHaveBeenCalledOnce();
	});
});
