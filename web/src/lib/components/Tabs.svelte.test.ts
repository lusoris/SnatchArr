// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import Tabs, { type Tab } from './Tabs.svelte';

const sampleTabs: Tab[] = [
	{ id: 'first', label: 'First Tab' },
	{ id: 'second', label: 'Second Tab' },
	{ id: 'third', label: 'Third Tab' }
];

describe('Tabs', () => {
	it('positive: click switches panel and sets aria wiring correctly', async () => {
		const onchange = vi.fn();
		const { container } = render(Tabs, {
			props: {
				tabs: sampleTabs,
				active: 'first',
				label: 'Test Tabs',
				onchange
			}
		});

		const tablist = screen.getByRole('tablist', { name: 'Test Tabs' });
		expect(tablist).toBeTruthy();

		const firstTab = screen.getByRole('tab', { name: 'First Tab' });
		const secondTab = screen.getByRole('tab', { name: 'Second Tab' });
		const panel = screen.getByRole('tabpanel');

		expect(firstTab.getAttribute('aria-selected')).toBe('true');
		expect(firstTab.getAttribute('tabindex')).toBe('0');
		expect(firstTab.getAttribute('aria-controls')).toBe('panel-first');
		expect(secondTab.getAttribute('aria-selected')).toBe('false');
		expect(secondTab.getAttribute('tabindex')).toBe('-1');
		expect(panel.getAttribute('aria-labelledby')).toBe('tab-first');
		expect(panel.getAttribute('id')).toBe('panel-first');

		await fireEvent.click(secondTab);
		expect(onchange).toHaveBeenCalledWith('second');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('positive: arrow keys switch panel and update active tab', async () => {
		const onchange = vi.fn();
		render(Tabs, {
			props: {
				tabs: sampleTabs,
				active: 'first',
				onchange
			}
		});

		const firstTab = screen.getByRole('tab', { name: 'First Tab' });
		firstTab.focus();

		await fireEvent.keyDown(firstTab, { key: 'ArrowRight' });
		expect(onchange).toHaveBeenCalledWith('second');

		await fireEvent.keyDown(firstTab, { key: 'ArrowDown' });
		expect(onchange).toHaveBeenCalledWith('second');
	});

	it('negative: disabled and hidden tabs cannot be focused or selected via keys', async () => {
		const onchange = vi.fn();
		const mixedTabs: Tab[] = [
			{ id: 't1', label: 'Tab 1' },
			{ id: 't2', label: 'Tab 2', disabled: true },
			{ id: 't3', label: 'Tab 3', hidden: true },
			{ id: 't4', label: 'Tab 4' }
		];

		render(Tabs, {
			props: {
				tabs: mixedTabs,
				active: 't1',
				onchange
			}
		});

		// Hidden tab is not rendered
		expect(screen.queryByRole('tab', { name: 'Tab 3' })).toBeNull();

		// Disabled tab is present but disabled and has tabindex -1
		const disabledTab = screen.getByRole('tab', { name: 'Tab 2' });
		expect(disabledTab.hasAttribute('disabled')).toBe(true);
		expect(disabledTab.getAttribute('tabindex')).toBe('-1');

		// ArrowRight from Tab 1 skips Tab 2 (disabled) and Tab 3 (hidden) to land on Tab 4
		const tab1 = screen.getByRole('tab', { name: 'Tab 1' });
		tab1.focus();
		await fireEvent.keyDown(tab1, { key: 'ArrowRight' });
		expect(onchange).toHaveBeenCalledWith('t4');
	});

	it('boundary: arrow navigation wraps around first and last tabs', async () => {
		const onchange = vi.fn();
		render(Tabs, {
			props: {
				tabs: sampleTabs,
				active: 'first',
				onchange
			}
		});

		const firstTab = screen.getByRole('tab', { name: 'First Tab' });
		firstTab.focus();

		// ArrowLeft on first tab wraps to last tab (third)
		await fireEvent.keyDown(firstTab, { key: 'ArrowLeft' });
		expect(onchange).toHaveBeenCalledWith('third');

		// ArrowUp on first tab wraps to last tab (third)
		await fireEvent.keyDown(firstTab, { key: 'ArrowUp' });
		expect(onchange).toHaveBeenCalledWith('third');
	});

	it('boundary: ArrowRight on last tab wraps to first tab', async () => {
		const onchange = vi.fn();
		render(Tabs, {
			props: {
				tabs: sampleTabs,
				active: 'third',
				onchange
			}
		});

		const thirdTab = screen.getByRole('tab', { name: 'Third Tab' });
		thirdTab.focus();

		await fireEvent.keyDown(thirdTab, { key: 'ArrowRight' });
		expect(onchange).toHaveBeenCalledWith('first');
	});

	it('boundary: Home and End keys jump to first and last enabled tabs', async () => {
		const onchange = vi.fn();
		render(Tabs, {
			props: {
				tabs: sampleTabs,
				active: 'second',
				onchange
			}
		});

		const secondTab = screen.getByRole('tab', { name: 'Second Tab' });
		secondTab.focus();

		await fireEvent.keyDown(secondTab, { key: 'Home' });
		expect(onchange).toHaveBeenCalledWith('first');

		await fireEvent.keyDown(secondTab, { key: 'End' });
		expect(onchange).toHaveBeenCalledWith('third');
	});

	it('positive: tabpanel does not suppress visible focus indicator', () => {
		render(Tabs, {
			props: {
				tabs: sampleTabs,
				active: 'first'
			}
		});

		const panel = screen.getByRole('tabpanel');
		expect(panel.getAttribute('tabindex')).toBe('0');
		expect(Array.from(panel.classList).some((cls) => cls.includes('outline-none'))).toBe(false);
		expect(panel.className).not.toContain('outline-none');
	});
});
