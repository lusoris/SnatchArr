// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { docsUrl } from '#lib/docs.js';
import { getHelpSummary } from '#lib/help.js';
import { m } from '#lib/paraglide/messages.js';
import HelpSheet from './HelpSheet.svelte';

describe('HelpSheet', () => {
	it('positive: renders summary and guide link when open', () => {
		render(HelpSheet, {
			props: {
				open: true,
				summaryKey: 'help_overview',
				docsPath: 'concepts.md'
			}
		});

		const dialog = screen.getByRole('dialog');
		expect(dialog).toBeTruthy();
		expect(screen.getByText(getHelpSummary('help_overview'))).toBeTruthy();

		const link = screen.getByRole('link', { name: m.help_read_full_guide() });
		expect(link.getAttribute('href')).toBe(docsUrl('concepts.md'));
		expect(link.getAttribute('target')).toBe('_blank');
		expect(link.getAttribute('rel')).toContain('noopener');
	});

	it('negative: does not render dialog when closed', () => {
		render(HelpSheet, {
			props: {
				open: false,
				summaryKey: 'help_overview',
				docsPath: 'concepts.md'
			}
		});

		expect(screen.queryByRole('dialog')).toBeNull();
	});

	it('positive: closes on Escape and restores focus', async () => {
		const triggerButton = document.createElement('button');
		triggerButton.textContent = 'Trigger';
		document.body.appendChild(triggerButton);
		triggerButton.focus();
		expect(document.activeElement).toBe(triggerButton);

		render(HelpSheet, {
			props: {
				open: true,
				summaryKey: 'help_overview',
				docsPath: 'concepts.md'
			}
		});

		const dialog = screen.getByRole('dialog');
		expect(dialog).toBeTruthy();

		await fireEvent.keyDown(dialog, { key: 'Escape' });

		// Dialog should close, and focus returns to triggerButton
		expect(screen.queryByRole('dialog')).toBeNull();
		expect(document.activeElement).toBe(triggerButton);

		document.body.removeChild(triggerButton);
	});
});
