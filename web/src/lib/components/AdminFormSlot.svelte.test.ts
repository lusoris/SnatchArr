// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { createRawSnippet, flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { session } from '#lib/session.svelte.js';
import AdminFormSlot, { type FormSlotProps, type MutationLike } from './AdminFormSlot.svelte';

interface TestBody {
	name: string;
}

describe('AdminFormSlot', () => {
	const admin = { id: 'u1', username: 'admin', role: 'admin' } as never;

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('positive: renders form snippet for admin, passes mapped props, forwards actions and passes WCAG', async () => {
		session.user = admin;
		let captured: FormSlotProps<TestBody> | undefined;
		const formSnippet = createRawSnippet<[FormSlotProps<TestBody>]>((getProps) => {
			captured = getProps();
			return {
				render: () =>
					'<form aria-label="Test Form"><label for="fn">Name</label><input id="fn" /><button type="submit">Save</button></form>'
			};
		});

		const save: MutationLike<TestBody> = {
			isPending: false,
			error: 'save-error',
			mutate: vi.fn()
		};
		const probe: MutationLike<TestBody> = {
			isPending: true,
			error: null,
			mutate: vi.fn()
		};
		const oncancel = vi.fn();

		const { container } = render(AdminFormSlot<TestBody>, {
			props: {
				key: 'form-1',
				save,
				probe,
				oncancel,
				form: formSnippet
			}
		});

		expect(screen.getByRole('form', { name: 'Test Form' })).toBeTruthy();
		expect(captured).toBeDefined();
		expect(captured?.pending).toBe(false);
		expect(captured?.error).toBe('save-error');
		expect(captured?.testing).toBe(true);

		captured?.onsave({ name: 'item-1' });
		expect(save.mutate).toHaveBeenCalledWith({ name: 'item-1' });

		captured?.ontest?.({ name: 'item-test' });
		expect(probe.mutate).toHaveBeenCalledWith({ name: 'item-test' });

		captured?.oncancel();
		expect(oncancel).toHaveBeenCalledTimes(1);

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('negative: does not render snippet for a signed-out session', () => {
		session.user = null;
		let rendered = false;
		const formSnippet = createRawSnippet<[FormSlotProps<TestBody>]>(() => {
			rendered = true;
			return {
				render: () => '<div data-testid="hidden">Hidden</div>'
			};
		});

		const save: MutationLike<TestBody> = {
			isPending: false,
			error: null,
			mutate: vi.fn()
		};

		render(AdminFormSlot<TestBody>, {
			props: {
				key: 'form-neg',
				save,
				oncancel: vi.fn(),
				form: formSnippet
			}
		});

		expect(rendered).toBe(false);
		expect(screen.queryByTestId('hidden')).toBeNull();
	});

	it('boundary: omits ontest and testing when probe mutation is absent', () => {
		session.user = admin;
		let captured: FormSlotProps<TestBody> | undefined;
		const formSnippet = createRawSnippet<[FormSlotProps<TestBody>]>((getProps) => {
			captured = getProps();
			return {
				render: () => '<div data-testid="no-probe">No Probe</div>'
			};
		});

		const save: MutationLike<TestBody> = {
			isPending: true,
			error: null,
			mutate: vi.fn()
		};

		render(AdminFormSlot<TestBody>, {
			props: {
				key: 'form-no-probe',
				save,
				oncancel: vi.fn(),
				form: formSnippet
			}
		});

		expect(screen.getByTestId('no-probe')).toBeTruthy();
		expect(captured?.pending).toBe(true);
		expect(captured?.ontest).toBeUndefined();
		expect(captured?.testing).toBeUndefined();
	});

	it('boundary: follows session.isAdmin as the role changes', () => {
		session.user = null;
		const formSnippet = createRawSnippet<[FormSlotProps<TestBody>]>(() => ({
			render: () => '<div data-testid="dynamic">Dynamic</div>'
		}));

		const save: MutationLike<TestBody> = {
			isPending: false,
			error: null,
			mutate: vi.fn()
		};

		render(AdminFormSlot<TestBody>, {
			props: {
				key: 'dyn-1',
				save,
				oncancel: vi.fn(),
				form: formSnippet
			}
		});

		expect(screen.queryByTestId('dynamic')).toBeNull();

		session.user = admin;
		flushSync();
		expect(screen.getByTestId('dynamic')).toBeTruthy();

		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		flushSync();
		expect(screen.queryByTestId('dynamic')).toBeNull();
	});
});
