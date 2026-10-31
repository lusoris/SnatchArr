// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';
import { ARR_KINDS, type ArrKind } from './kinds.js';

type PriorityPresetMap = components['schemas']['PriorityPresetMap'];

export interface PriorityRow {
	kind: ArrKind;
	tied: boolean;
}

/** Reads an order without changing stored ranks; equal ranks keep the canonical kind order. */
export function priorityRows(preset: PriorityPresetMap): PriorityRow[] {
	const kinds = [...ARR_KINDS].sort((a, b) => preset[a] - preset[b]);
	return kinds.map((kind, index) => ({
		kind,
		tied: index > 0 && preset[kind] === preset[kinds[index - 1]]
	}));
}

/** Each position uses ten ranks; an explicit tie inherits the preceding row's rank. */
export function ranksFromRows(rows: readonly PriorityRow[]): PriorityPresetMap {
	const ranks: PriorityPresetMap = {};
	let previous = 10;
	rows.forEach((row, index) => {
		const rank = index > 0 && row.tied ? previous : 10 * (index + 1);
		ranks[row.kind] = rank;
		previous = rank;
	});
	return ranks;
}

/** Moves one kind, clearing its tie; out-of-range moves leave the order alone. */
export function movePriorityKind(
	rows: readonly PriorityRow[],
	kind: string,
	offset: number
): PriorityRow[] {
	const index = rows.findIndex((row) => row.kind === kind);
	const target = index + offset;
	if (index < 0 || target < 0 || target >= rows.length) return [...rows];
	const next = rows.map((row) => ({ ...row }));
	const [moving] = next.splice(index, 1);
	next.splice(target, 0, { ...moving, tied: false });
	next[0].tied = false;
	return next;
}

/** Drops a kind at a position; like a keyboard move, the dropped row loses its tie. */
export function dropPriorityKind(
	rows: readonly PriorityRow[],
	kind: string,
	targetIndex: number
): PriorityRow[] {
	const index = rows.findIndex((row) => row.kind === kind);
	if (index < 0 || index === targetIndex || !Number.isInteger(targetIndex)) return [...rows];
	return movePriorityKind(rows, kind, targetIndex - index);
}

/** Sets an explicit tie, except at the first position where there is no preceding row. */
export function tiePriorityKind(
	rows: readonly PriorityRow[],
	kind: string,
	tied: boolean
): PriorityRow[] {
	return rows.map((row, index) =>
		row.kind === kind ? { ...row, tied: index > 0 && tied } : { ...row }
	);
}
