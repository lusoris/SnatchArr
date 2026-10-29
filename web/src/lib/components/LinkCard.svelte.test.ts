// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { createRawSnippet, type ComponentProps } from 'svelte';
import { axe } from 'vitest-axe';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import LinkCard, { type LinkItem } from './LinkCard.svelte';

const sampleLink: LinkItem = {
	name: 'Test Prowlarr',
	base_url: 'http://prowlarr.local:9696',
	last_seen_version: '1.2.3',
	last_check_at: '2026-09-01T12:00:00Z',
	last_error: null
};

const statusExtraSnippet = createRawSnippet(() => ({
	render: () => '<span data-testid="status-extra"> · extra status</span>'
}));

const extraActionsSnippet = createRawSnippet(() => ({
	render: () => '<button type="button" class="btn" data-testid="extra-action">Extra</button>'
}));

function signIn(role: 'admin' | 'viewer') {
	session.user = { id: 'u1', username: role, role } as never;
}

function renderCard(props: ComponentProps<typeof LinkCard>) {
	const list = document.createElement('ul');
	document.body.appendChild(list);
	return render(LinkCard, {
		target: list,
		props
	});
}

describe('LinkCard', () => {
	afterEach(() => {
		session.user = null;
		document.body.innerHTML = '';
	});

	it('positive: renders link details, triggers edit/test and passes WCAG 2.2 AA', async () => {
		const onedit = vi.fn();
		const ontest = vi.fn();
		const ondelete = vi.fn();

		signIn('admin');
		const { container } = renderCard({
			link: sampleLink,
			onedit,
			ontest,
			ondelete,
			statusExtra: statusExtraSnippet,
			extraActions: extraActionsSnippet
		});

		expect(screen.getByRole('heading', { level: 3, name: /Test Prowlarr/ })).toBeTruthy();
		expect(
			screen.getByText(
				(_, el) =>
					el?.tagName === 'SPAN' &&
					el.hasAttribute('data-url') &&
					el.textContent === 'http://prowlarr.local:9696'
			)
		).toBeTruthy();
		expect(screen.getByText('1.2.3')).toBeTruthy();
		expect(screen.getByTestId('status-extra')).toBeTruthy();
		expect(screen.getByTestId('extra-action')).toBeTruthy();
		expect(container.querySelector('[data-status="ok"]')).toBeTruthy();

		const card = container.querySelector('li') as HTMLElement;
		expect(card.classList.contains('target')).toBe(true);
		const editBtn = screen.getByRole('button', { name: m.action_edit() });
		expect(editBtn.classList.contains('target-link')).toBe(true);

		await fireEvent.click(editBtn);
		expect(onedit).toHaveBeenCalledTimes(1);

		await fireEvent.click(screen.getByRole('button', { name: m.action_test() }));
		expect(ontest).toHaveBeenCalledTimes(1);

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('negative: hides admin controls for a viewer and shows never tested when check_at is missing', () => {
		const linkWithoutCheck: LinkItem = {
			name: 'Untested Link',
			base_url: 'http://untested.local:9696',
			last_check_at: null,
			last_error: null
		};

		signIn('viewer');
		const { container } = renderCard({
			link: linkWithoutCheck,
			onedit: vi.fn(),
			ontest: vi.fn(),
			ondelete: vi.fn()
		});

		expect(screen.getAllByText(m.instance_never_tested()).length).toBeGreaterThanOrEqual(1);
		expect(container.querySelector('[data-status="idle"]')).toBeTruthy();
		expect(container.querySelector('.target')).toBeNull();
		expect(screen.queryByRole('button', { name: m.action_edit() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.action_test() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.action_delete() })).toBeNull();
	});

	it('boundary: handles minimal link with error and verifies pending button states', () => {
		const errorLink: LinkItem = {
			name: '',
			base_url: '',
			last_error: 'connection timeout'
		};

		signIn('admin');
		const { container } = renderCard({
			link: errorLink,
			onedit: vi.fn(),
			ontest: vi.fn(),
			ondelete: vi.fn(),
			testPending: true,
			deletePending: true
		});

		expect(screen.getAllByText('connection timeout').length).toBeGreaterThanOrEqual(1);
		expect(container.querySelector('[data-status="danger"]')).toBeTruthy();
		const testBtn = screen.getByRole('button', { name: m.action_test() }) as HTMLButtonElement;
		expect(testBtn.disabled).toBe(true);
		const deleteBtn = screen.getByRole('button', { name: m.action_delete() }) as HTMLButtonElement;
		expect(deleteBtn.disabled).toBe(true);
	});
});
