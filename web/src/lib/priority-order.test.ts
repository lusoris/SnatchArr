// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { ARR_KINDS } from './kinds.js';
import {
	dropPriorityKind,
	movePriorityKind,
	priorityRows,
	ranksFromRows,
	tiePriorityKind
} from './priority-order.js';

const shipped = {
	sonarr: 10,
	radarr: 10,
	lidarr: 20,
	readarr: 30,
	whisparr_v2: 40,
	whisparr_v3: 40
};

describe('priority order', () => {
	it('drops Lidarr above Sonarr with positional ranks without mutating the input', () => {
		const rows = priorityRows(shipped);
		const before = structuredClone(rows);
		const dropped = dropPriorityKind(rows, 'lidarr', 0);
		expect(dropped.map((row) => row.kind)).toEqual([
			'lidarr',
			'sonarr',
			'radarr',
			'readarr',
			'whisparr_v2',
			'whisparr_v3'
		]);
		expect(ranksFromRows(dropped)).toMatchObject({ lidarr: 10, sonarr: 20, radarr: 20 });
		expect(rows).toEqual(before);
	});

	it('clears the dropped row tie, as a keyboard move does, and keeps the first row untied', () => {
		const rows = priorityRows(shipped);
		expect(dropPriorityKind(rows, 'radarr', 3)[3]).toEqual({ kind: 'radarr', tied: false });
		expect(dropPriorityKind(rows, 'radarr', 3)).toEqual(movePriorityKind(rows, 'radarr', 2));
		expect(dropPriorityKind(rows, 'radarr', 0)[0]).toEqual({ kind: 'radarr', tied: false });
		expect(dropPriorityKind(rows, 'sonarr', 5)[0]).toEqual({ kind: 'radarr', tied: false });
		expect(rows[1].tied).toBe(true);
	});

	it('drops a kind at the last index', () => {
		const dropped = dropPriorityKind(priorityRows(shipped), 'lidarr', 5);
		expect(dropped.at(-1)).toEqual({ kind: 'lidarr', tied: false });
		expect(ranksFromRows(dropped).lidarr).toBe(60);
	});

	it.each([
		['unknown', 1],
		['radarr', 1],
		['lidarr', -1],
		['lidarr', 6],
		['lidarr', 1.5],
		['lidarr', Number.NaN]
	] as const)(
		'returns an unchanged copy for an invalid or same-index drop: %s %s',
		(kind, index) => {
			const rows = priorityRows(shipped);
			const dropped = dropPriorityKind(rows, kind, index);
			expect(dropped).toEqual(rows);
			expect(dropped).not.toBe(rows);
		}
	);

	it('preserves stable kind order and explicit ties when reading a preset', () => {
		const rows = priorityRows(shipped);
		expect(rows.map((row) => row.kind)).toEqual(ARR_KINDS);
		expect(rows.map((row) => row.tied)).toEqual([false, true, false, false, false, true]);
	});

	it('moves Lidarr before Sonarr and saves ranks in steps of ten', () => {
		const rows = movePriorityKind(priorityRows(shipped), 'lidarr', -2);
		expect(rows[0].kind).toBe('lidarr');
		expect(ranksFromRows(rows)).toEqual({
			lidarr: 10,
			sonarr: 20,
			radarr: 20,
			readarr: 40,
			whisparr_v2: 50,
			whisparr_v3: 50
		});
		expect(shipped.lidarr).toBe(20);
	});

	it('ties share the rank directly above; untie restores the positional rank', () => {
		const rows = tiePriorityKind(priorityRows(shipped), 'lidarr', true);
		expect(ranksFromRows(rows).lidarr).toBe(10);
		expect(ranksFromRows(tiePriorityKind(rows, 'radarr', false))).toMatchObject({
			radarr: 20,
			lidarr: 20
		});
		expect(ranksFromRows(tiePriorityKind(rows, 'lidarr', false)).lidarr).toBe(30);
	});

	it.each([
		['sonarr', -1],
		['whisparr_v3', 1],
		['unknown', 1]
	] as const)('refuses boundary or unknown moves: %s %s', (kind, direction) => {
		const rows = priorityRows(shipped);
		expect(movePriorityKind(rows, kind, direction)).toEqual(rows);
	});

	it('first row cannot tie, moving a tied row clears its tie, ranks stay within bounds', () => {
		const rows = tiePriorityKind(priorityRows(shipped), 'sonarr', true);
		expect(rows[0].tied).toBe(false);
		const moved = movePriorityKind(rows, 'radarr', 1);
		expect(moved.find((row) => row.kind === 'radarr')?.tied).toBe(false);
		const ranks = Object.values(ranksFromRows(moved));
		expect(Math.min(...ranks)).toBeGreaterThanOrEqual(10);
		expect(Math.max(...ranks)).toBeLessThanOrEqual(60);
	});

	it('sorts boundary ranks 1 and 100 without renumbering until an edit', () => {
		const rows = priorityRows({ ...shipped, lidarr: 1, sonarr: 100 });
		expect(rows[0].kind).toBe('lidarr');
		expect(rows.at(-1)?.kind).toBe('sonarr');
		expect(ranksFromRows(rows).sonarr).toBe(60);
	});
});
