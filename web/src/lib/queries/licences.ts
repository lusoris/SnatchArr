// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { asset } from '$app/paths';
import { createSentioQuery } from '@sveltesentio/query';
import { keys } from '#lib/query.js';

export type Ecosystem = 'go' | 'rust' | 'npm';

export interface LicencePackage {
	ecosystem: Ecosystem;
	name: string;
	version: string;
	licence: string;
	url: string;
	text: string;
}

export interface LicencesData {
	generatedBy: {
		'cargo-about': string;
		'go-licenses': string;
		pnpm: string;
	};
	packages: LicencePackage[];
}

/**
 * Lazy query for third-party licences metadata and texts.
 * Fetches licences.json from the static asset root.
 */
export function licencesQuery() {
	return createSentioQuery<LicencesData>({
		queryKey: keys.licences,
		queryFn: async () => {
			const res = await fetch(asset('licences.json'));
			if (!res.ok) {
				throw new Error(`Failed to load licences (${res.status})`);
			}
			return (await res.json()) as LicencesData;
		}
	});
}
