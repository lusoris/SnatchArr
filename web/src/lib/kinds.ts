// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';
import { m } from './paraglide/messages.js';

export type AppKind = components['schemas']['AppKind'];

// Every instance kind of the API, in the order the form offers them, and whether its upstream
// project is retired (ADR-0008). A kind added to the OpenAPI enumeration fails the type check
// here until it is listed.
const legacy = {
	sonarr: false,
	radarr: false,
	lidarr: false,
	readarr: true,
	whisparr_v2: false,
	whisparr_v3: false
} as const satisfies Record<AppKind, boolean>;

export const APP_KINDS = Object.keys(legacy) as AppKind[];

/** Reports whether a kind is kept as a frozen legacy target. An unknown kind is not. */
export function isLegacyKind(kind: string): boolean {
	return Object.hasOwn(legacy, kind) && legacy[kind as AppKind];
}

/** The kind as the UI shows it: a legacy kind carries the legacy label. */
export function kindLabel(kind: string): string {
	return isLegacyKind(kind) ? m.instance_kind_legacy({ kind }) : kind;
}
