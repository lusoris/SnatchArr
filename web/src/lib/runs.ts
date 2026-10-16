// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';
import { m } from './paraglide/messages.js';

type RunStatus = components['schemas']['Run']['status'];
type SnatchKind = components['schemas']['SnatchKind'];

// One label per value of the OpenAPI enumerations: a value added there fails the type check here.
const statusLabels = {
	queued: m.run_status_queued,
	leased: m.run_status_leased,
	done: m.run_status_done,
	failed: m.run_status_failed,
	cancelled: m.run_status_cancelled
} as const satisfies Record<RunStatus, () => string>;

const kindLabels = {
	missing: m.snatch_kind_missing,
	upgrade: m.snatch_kind_upgrade
} as const satisfies Record<SnatchKind, () => string>;

/** The run status as the UI shows it; an unknown status shows as sent. */
export function runStatusLabel(status: string): string {
	return Object.hasOwn(statusLabels, status) ? statusLabels[status as RunStatus]() : status;
}

/** The snatch kind as the UI shows it; an unknown kind shows as sent. */
export function snatchKindLabel(kind: string): string {
	return Object.hasOwn(kindLabels, kind) ? kindLabels[kind as SnatchKind]() : kind;
}
