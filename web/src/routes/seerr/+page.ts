// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { redirect } from '@sveltejs/kit';
import { legacyTarget } from '#lib/legacyRedirect.js';
import type { PageLoad } from './$types';

export const load: PageLoad = ({ url }) => {
	redirect(307, legacyTarget(url.pathname, url));
};
