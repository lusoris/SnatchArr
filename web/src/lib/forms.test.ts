// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { toast } from 'svelte-sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { deleteCallbacks, probeCallbacks, saveSuccess } from './forms.js';

describe('forms helper', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	describe('probeCallbacks', () => {
		it('positive: reports probe success message via toast', () => {
			const success = vi.spyOn(toast, 'success');
			const callbacks = probeCallbacks();

			callbacks.onSuccess({ app_name: 'Prowlarr', version: '1.30.0' });
			expect(success).toHaveBeenCalledWith(
				m.instance_probe_ok({ app: 'Prowlarr', version: '1.30.0' })
			);
		});

		it('negative: reports probe error with described detail', () => {
			const error = vi.spyOn(toast, 'error');
			const callbacks = probeCallbacks();

			callbacks.onError(new Error('Connection refused'));
			expect(error).toHaveBeenCalledWith(m.probe_failed({ detail: 'Connection refused' }));
		});

		it('boundary: handles undefined probe result cleanly', () => {
			const success = vi.spyOn(toast, 'success');
			const callbacks = probeCallbacks();

			callbacks.onSuccess(undefined);
			expect(success).toHaveBeenCalledWith(m.instance_probe_ok({ app: '', version: '' }));
		});
	});

	describe('deleteCallbacks', () => {
		it('positive: reports deletion and closes form when deleting active editing item', () => {
			const success = vi.spyOn(toast, 'success');
			const closeForm = vi.fn();
			const activeItem = { id: 'item-1', name: 'Item One' };
			const callbacks = deleteCallbacks(() => activeItem, closeForm);

			callbacks.onSuccess(activeItem);
			expect(success).toHaveBeenCalledWith(m.form_deleted({ name: 'Item One' }));
			expect(closeForm).toHaveBeenCalledTimes(1);
		});

		it('negative: reports deletion but leaves form open when deleting different item', () => {
			const success = vi.spyOn(toast, 'success');
			const closeForm = vi.fn();
			const activeItem = { id: 'item-1', name: 'Item One' };
			const otherItem = { id: 'item-2', name: 'Item Two' };
			const callbacks = deleteCallbacks(() => activeItem, closeForm);

			callbacks.onSuccess(otherItem);
			expect(success).toHaveBeenCalledWith(m.form_deleted({ name: 'Item Two' }));
			expect(closeForm).not.toHaveBeenCalled();
		});

		it('boundary: reports delete error without closing form when null active item', () => {
			const error = vi.spyOn(toast, 'error');
			const closeForm = vi.fn();
			const callbacks = deleteCallbacks(() => null, closeForm);

			callbacks.onError(new Error('deletion failed'));
			expect(error).toHaveBeenCalledWith('deletion failed');
			expect(closeForm).not.toHaveBeenCalled();
		});
	});

	describe('saveSuccess', () => {
		it('positive: reports saved item name and closes form', () => {
			const success = vi.spyOn(toast, 'success');
			const closeForm = vi.fn();
			const handler = saveSuccess(closeForm);

			handler({ name: 'Saved Name' });
			expect(success).toHaveBeenCalledWith(m.form_saved({ name: 'Saved Name' }));
			expect(closeForm).toHaveBeenCalledTimes(1);
		});

		it('boundary: handles null and undefined saved item gracefully', () => {
			const success = vi.spyOn(toast, 'success');
			const closeForm = vi.fn();
			const handler = saveSuccess(closeForm);

			handler(null);
			expect(success).toHaveBeenCalledWith(m.form_saved({ name: '' }));
			expect(closeForm).toHaveBeenCalledTimes(1);

			handler(undefined);
			expect(success).toHaveBeenCalledWith(m.form_saved({ name: '' }));
			expect(closeForm).toHaveBeenCalledTimes(2);
		});
	});
});
