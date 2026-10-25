// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { assertLocaleParity } from '@sveltesentio/testing/locale-parity';
import { describe, expect, it } from 'vitest';
import { findDuplicateKeys } from './json-duplicate-keys.js';

const webDir = path.resolve(import.meta.dirname, '../..');
const messagesDir = path.join(webDir, 'messages');
const settings = JSON.parse(
	fs.readFileSync(path.join(webDir, 'project.inlang/settings.json'), 'utf8')
) as { baseLocale?: string };
const base = settings.baseLocale ?? 'en';

const catalogs: Record<string, Record<string, unknown>> = Object.fromEntries(
	fs
		.readdirSync(messagesDir)
		.filter((file) => file.endsWith('.json'))
		.sort()
		.map((file) => [
			path.basename(file, '.json'),
			JSON.parse(fs.readFileSync(path.join(messagesDir, file), 'utf8')) as Record<string, unknown>
		])
);

describe('messages/*.json parity', () => {
	it('holds the base locale and at least one other locale', () => {
		expect(catalogs[base]).toBeDefined();
		expect(Object.keys(catalogs).length).toBeGreaterThan(1);
	});

	it('matches every locale to the base keys, placeholders and non-empty values', () => {
		expect(() => assertLocaleParity(catalogs, { base })).not.toThrow();
	});

	it('refuses a planted locale with a dropped key and an extra key', () => {
		const baseCatalog = catalogs[base] ?? {};
		const keys = Object.keys(baseCatalog).filter((key) => key !== '$schema');
		const dropped = keys[0] ?? '';
		const planted: Record<string, unknown> = { ...baseCatalog };
		delete planted[dropped];
		planted['planted_extra'] = 'Hello {renamed}';
		const run = () => assertLocaleParity({ ...catalogs, zz: planted }, { base });
		expect(run).toThrow(`[zz] missing keys: ${dropped}`);
		expect(run).toThrow('[zz] extra keys: planted_extra');
	});

	it('refuses duplicate keys in every catalog file', () => {
		const files = fs
			.readdirSync(messagesDir)
			.filter((file) => file.endsWith('.json'))
			.sort();
		for (const file of files) {
			const raw = fs.readFileSync(path.join(messagesDir, file), 'utf8');
			expect(findDuplicateKeys(raw), file).toEqual([]);
		}
	});

	it('detects a planted duplicate key in the base catalog', () => {
		const raw = fs.readFileSync(path.join(messagesDir, `${base}.json`), 'utf8');
		const lines = raw.split('\n');
		const lineIdx = lines.findIndex((line) => /^\s*"([^"]+)":/.test(line));
		expect(lineIdx).toBeGreaterThanOrEqual(0);
		const keyMatch = lines[lineIdx]?.match(/"([^"]+)":/);
		const expectedKey = keyMatch ? keyMatch[1] : '';
		const planted = [
			...lines.slice(0, lineIdx + 1),
			lines[lineIdx],
			...lines.slice(lineIdx + 1)
		].join('\n');
		expect(findDuplicateKeys(planted)).toEqual([expectedKey]);
	});
});
