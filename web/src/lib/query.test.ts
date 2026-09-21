// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { keys, queryClient } from './query.js';

describe('query keys', () => {
	it('nest per-instance keys under the list key so one invalidation covers both', () => {
		expect(keys.instance('a')).toEqual([...keys.instances, 'a']);
		expect(keys.policy('a').slice(0, 2)).toEqual(keys.instance('a'));
		expect(keys.dlstatus.slice(0, 1)).toEqual(keys.dlclients);
	});

	it('keeps server state fresh for 15 seconds', () => {
		expect(queryClient.getDefaultOptions().queries?.staleTime).toBe(15_000);
	});
});
