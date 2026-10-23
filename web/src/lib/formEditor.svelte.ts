// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { tick } from 'svelte';

/**
 * Coordinates edit state and remount generation for admin entity forms.
 * Owns the active item, unique remount keys, and focus management.
 */
export class FormEditor<T extends { id: string }> {
	/** The entity currently loaded in the form; null denotes a new entity. */
	editing = $state<T | null>(null);

	/** Remount generation bumped on close to reset unmanaged form state. */
	private generation = $state(0);

	private readonly focusId: string;
	private readonly reset: () => void;

	/**
	 * @param focusId Target element id focused after opening an item for edit.
	 * @param reset Lazy callback to reset mutation errors and state.
	 */
	constructor(focusId: string, reset: () => void) {
		this.focusId = focusId;
		this.reset = reset;
	}

	/**
	 * Remount key for `{#key}` blocks; changes on target switch and on every close.
	 */
	get key(): string {
		return `${this.editing?.id ?? 'new'}-${this.generation}`;
	}

	/**
	 * Closes the edit form, increments the remount generation, and resets mutation state.
	 */
	close = (): void => {
		this.editing = null;
		this.generation += 1;
		this.reset();
	};

	/**
	 * Opens an existing entity for editing, resets mutations, and focuses the target element.
	 */
	open = async (item: T): Promise<void> => {
		this.reset();
		this.editing = item;
		await tick();
		document.getElementById(this.focusId)?.focus();
	};
}
