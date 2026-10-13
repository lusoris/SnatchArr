// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

/** Matches {name} placeholders, including formatters like {count: number}. */
const PLACEHOLDER_PATTERN = /\{\s*([a-zA-Z0-9_]+)(?::[^}]*)?\s*\}/g;

export interface PlaceholderMismatch {
	key: string;
	expected: string[];
	actual: string[];
}

export interface ParityResult {
	missingKeys: string[];
	extraKeys: string[];
	emptyValues: string[];
	placeholderMismatches: PlaceholderMismatch[];
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Upper bound on the nodes one catalog walk visits (HISS-02). */
const MAX_NODES = 100_000;

interface Entry {
	path: string;
	value: unknown;
}

function childEntries(record: Record<string, unknown>, prefix: string): Entry[] {
	return Object.entries(record)
		.filter(([key]) => prefix !== '' || key !== '$schema')
		.map(([key, value]) => ({ path: prefix ? `${prefix}.${key}` : key, value }));
}

/** Lists every leaf and every empty object of a catalog with its dotted path, without recursion. */
function leaves(record: Record<string, unknown>): Entry[] {
	const found: Entry[] = [];
	const stack = childEntries(record, '');
	for (let visited = 0; stack.length > 0; visited++) {
		if (visited >= MAX_NODES) {
			throw new Error(`message catalog exceeds ${MAX_NODES} nodes`);
		}
		const entry = stack.pop() as Entry;
		if (isPlainObject(entry.value) && Object.keys(entry.value).length > 0) {
			stack.push(...childEntries(entry.value, entry.path));
		} else {
			found.push(entry);
		}
	}
	return found;
}

/** Extracts all keys from a message catalog, flattening nested objects. */
export function keySet(record: Record<string, unknown>): Set<string> {
	return new Set(leaves(record).map((entry) => entry.path));
}

/** Returns sorted list of keys present in expected but missing from actual. */
export function missing(expected: Set<string>, actual: Set<string>): string[] {
	const result: string[] = [];
	for (const key of expected) {
		if (!actual.has(key)) {
			result.push(key);
		}
	}
	return result.sort();
}

/** Extracts placeholder variable names from a string, variant array, or object. */
export function placeholders(value: unknown): Set<string> {
	const result = new Set<string>();
	const stack: unknown[] = [value];
	for (let visited = 0; stack.length > 0; visited++) {
		if (visited >= MAX_NODES) {
			throw new Error(`message value exceeds ${MAX_NODES} nodes`);
		}
		const item = stack.pop();
		if (typeof item === 'string') {
			for (const match of item.matchAll(PLACEHOLDER_PATTERN)) {
				result.add(match[1]);
			}
		} else if (Array.isArray(item)) {
			stack.push(...(item as unknown[]));
		} else if (isPlainObject(item)) {
			stack.push(...Object.values(item));
		}
	}
	return result;
}

/** Finds all keys with empty or whitespace-only string values. */
export function findEmptyValues(record: Record<string, unknown>): string[] {
	return leaves(record)
		.filter((entry) => typeof entry.value === 'string' && entry.value.trim() === '')
		.map((entry) => entry.path)
		.sort();
}

/** Resolves a key or dot-separated path in a record. */
export function getValue(record: Record<string, unknown>, keyPath: string): unknown {
	if (Object.hasOwn(record, keyPath)) {
		return record[keyPath];
	}
	const parts = keyPath.split('.');
	let current: unknown = record;
	for (const part of parts) {
		if (!isPlainObject(current)) {
			return undefined;
		}
		current = current[part];
	}
	return current;
}

function arraysEqual(a: string[], b: string[]): boolean {
	if (a.length !== b.length) {
		return false;
	}
	return a.every((val, index) => val === b[index]);
}

/** Compares target locale catalog against base locale catalog. */
export function compareLocales(
	base: Record<string, unknown>,
	target: Record<string, unknown>
): ParityResult {
	const baseKeys = keySet(base);
	const targetKeys = keySet(target);

	const missingKeys = missing(baseKeys, targetKeys);
	const extraKeys = missing(targetKeys, baseKeys);
	const emptyValues = findEmptyValues(target);

	const placeholderMismatches: PlaceholderMismatch[] = [];
	for (const key of baseKeys) {
		if (!targetKeys.has(key)) {
			continue;
		}
		const basePh = Array.from(placeholders(getValue(base, key))).sort();
		const targetPh = Array.from(placeholders(getValue(target, key))).sort();
		if (!arraysEqual(basePh, targetPh)) {
			placeholderMismatches.push({
				key,
				expected: basePh,
				actual: targetPh
			});
		}
	}

	placeholderMismatches.sort((a, b) => a.key.localeCompare(b.key));

	return {
		missingKeys,
		extraKeys,
		emptyValues,
		placeholderMismatches
	};
}
