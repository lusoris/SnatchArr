// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { afterEach, describe, expect, it, vi } from 'vitest';
import { FormEditor } from './formEditor.svelte.js';

interface TestItem {
	id: string;
	name: string;
}

describe('FormEditor', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: open sets editing, focuses element with focusId, calls reset', async () => {
		const input = document.createElement('input');
		input.id = 'target-input';
		document.body.appendChild(input);
		const focusSpy = vi.spyOn(input, 'focus');

		const reset = vi.fn();
		const editor = new FormEditor<TestItem>('target-input', reset);

		expect(editor.editing).toBeNull();
		expect(editor.key).toBe('new-0');
		expect(reset).not.toHaveBeenCalled();

		const item: TestItem = { id: 'item-1', name: 'Alpha' };
		await editor.open(item);

		expect(editor.editing).toEqual(item);
		expect(editor.key).toBe('item-1-0');
		expect(reset).toHaveBeenCalledTimes(1);
		expect(focusSpy).toHaveBeenCalledTimes(1);
		expect(document.activeElement).toBe(input);

		input.remove();
	});

	it('negative: open with missing focus element does not throw; close calls reset, clears editing', async () => {
		const reset = vi.fn();
		const editor = new FormEditor<TestItem>('missing-element', reset);

		const item: TestItem = { id: 'item-2', name: 'Beta' };
		await expect(editor.open(item)).resolves.toBeUndefined();
		expect(editor.editing).toEqual(item);
		expect(reset).toHaveBeenCalledTimes(1);

		editor.close();
		expect(editor.editing).toBeNull();
		expect(reset).toHaveBeenCalledTimes(2);
	});

	it('boundary: key changes on every close even when editing already null: "new-0" -> "new-1" -> "new-2"; key uses item id', async () => {
		const reset = vi.fn();
		const editor = new FormEditor<TestItem>('target', reset);

		expect(editor.key).toBe('new-0');

		editor.close();
		expect(editor.key).toBe('new-1');

		editor.close();
		expect(editor.key).toBe('new-2');

		await editor.open({ id: 'item-99', name: 'Omega' });
		expect(editor.key).toBe('item-99-2');

		editor.close();
		expect(editor.key).toBe('new-3');
	});
});
