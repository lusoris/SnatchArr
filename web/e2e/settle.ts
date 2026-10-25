// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Waits until all svelte-sonner toasts settle before running accessibility sweeps.
// Partial toast opacity during CSS transitions produces transient contrast violations
// (measured 4.33:1 against the settled 6.6:1 of the success toast).
import type { Page } from '@playwright/test';

export interface SettleOptions {
	timeout?: number;
}

/**
 * Waits until every `[data-sonner-toast]` on the page has finished its CSS transitions
 * and animations (computed opacity reaches 1, or 0 when removed or stacked out of view, and all Web Animations are finished).
 */
export async function settleToasts(page: Page, options?: SettleOptions): Promise<void> {
	await page.waitForFunction(
		() => {
			const toasts = Array.from(document.querySelectorAll<HTMLElement>('[data-sonner-toast]'));
			if (toasts.length === 0) {
				return true;
			}
			return toasts.every((toast) => {
				const hidden =
					toast.getAttribute('data-removed') === 'true' ||
					toast.getAttribute('data-visible') === 'false';
				const targetOpacity = hidden ? '0' : '1';
				const opacity = window.getComputedStyle(toast).opacity;
				if (opacity !== targetOpacity) {
					return false;
				}
				const anims =
					typeof toast.getAnimations === 'function' ? toast.getAnimations({ subtree: true }) : [];
				return anims.every((anim) => anim.playState === 'finished' || anim.playState === 'idle');
			});
		},
		undefined,
		options?.timeout !== undefined ? { timeout: options.timeout } : undefined
	);
}
