// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { toast } from 'svelte-sonner';
import { describeError } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { probeMessage } from '#lib/products.js';

type ProbeResult = Parameters<typeof probeMessage>[0];

/** Reports a failed connection probe via toast. */
export function probeFailed(err: unknown) {
	toast.error(m.probe_failed({ detail: describeError(err) }));
}

/** Callbacks for connection probe mutations that report results via toast. */
export function probeCallbacks() {
	return {
		onSuccess: (r: ProbeResult | undefined) => toast.success(probeMessage(r)),
		onError: probeFailed
	};
}

/** Callbacks for delete mutations that report deletion and close the active edit form if matching. */
export function deleteCallbacks<T extends { id: string; name: string }>(
	getEditing: () => { id: string } | null,
	closeForm: () => void
) {
	return {
		onSuccess: (item: T) => {
			toast.success(m.form_deleted({ name: item.name }));
			if (getEditing()?.id === item.id) closeForm();
		},
		onError: (err: unknown) => toast.error(describeError(err))
	};
}

/** Callback for save mutations that reports success and closes the edit form. */
export function saveSuccess(closeForm: () => void) {
	return (item?: { name?: string } | null) => {
		toast.success(m.form_saved({ name: item?.name ?? '' }));
		closeForm();
	};
}
