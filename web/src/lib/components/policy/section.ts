// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from '#lib/api/schema.js';

export type SnatchPolicy = components['schemas']['SnatchPolicy'];
export type AppKind = components['schemas']['AppKind'];

export interface PolicySectionProps {
	policy: SnatchPolicy;
	kind?: AppKind;
}

/**
 * Returns true if the application kind has dedicated policy controls.
 */
export function isAppSpecificKind(kind?: AppKind): boolean {
	return kind === 'radarr' || kind === 'sonarr' || kind === 'lidarr';
}
