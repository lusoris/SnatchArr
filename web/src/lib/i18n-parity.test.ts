// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import {
	compareLocales,
	findEmptyValues,
	getValue,
	keySet,
	missing,
	placeholders
} from './i18n-parity.js';

describe('pure in-memory parity checks', () => {
	describe('positive cases', () => {
		it('accepts identical maps with matching placeholders and formatters', () => {
			const base = {
				app_name: 'SnatchArr',
				greeting: 'Hello {name}!',
				metrics: '{count: number} items processed'
			};
			const target = {
				app_name: 'SnatchArr',
				greeting: 'Hallo {name}!',
				metrics: '{count: number} Einträge verarbeitet'
			};

			const result = compareLocales(base, target);
			expect(result.missingKeys).toEqual([]);
			expect(result.extraKeys).toEqual([]);
			expect(result.emptyValues).toEqual([]);
			expect(result.placeholderMismatches).toEqual([]);
		});
	});

	describe('negative cases', () => {
		it('detects missing keys', () => {
			const base = { first: 'First', second: 'Second' };
			const target = { first: 'Erste' };

			const baseKeys = keySet(base);
			const targetKeys = keySet(target);
			expect(missing(baseKeys, targetKeys)).toEqual(['second']);

			const result = compareLocales(base, target);
			expect(result.missingKeys).toEqual(['second']);
		});

		it('detects extra keys', () => {
			const base = { first: 'First' };
			const target = { first: 'Erste', unexpected: 'Unerwartet' };

			const baseKeys = keySet(base);
			const targetKeys = keySet(target);
			expect(missing(targetKeys, baseKeys)).toEqual(['unexpected']);

			const result = compareLocales(base, target);
			expect(result.extraKeys).toEqual(['unexpected']);
		});

		it('detects empty and whitespace-only string values', () => {
			const target = {
				valid: 'Valid',
				empty: '',
				whitespace: '   ',
				num: 42
			};

			expect(findEmptyValues(target)).toEqual(['empty', 'whitespace']);

			const base = { valid: 'Valid', empty: 'Empty', whitespace: 'Space', num: 42 };
			const result = compareLocales(base, target);
			expect(result.emptyValues).toEqual(['empty', 'whitespace']);
		});

		it('detects renamed and mismatched placeholders', () => {
			const base = {
				user_msg: 'Hello {name}',
				stat_msg: '{used} of {cap}',
				extra_ph: 'Welcome {user}'
			};
			const target = {
				user_msg: 'Hallo {username}',
				stat_msg: '{used} von {total}',
				extra_ph: 'Willkommen {user} ({role})'
			};

			const result = compareLocales(base, target);
			expect(result.placeholderMismatches).toEqual([
				{
					key: 'extra_ph',
					expected: ['user'],
					actual: ['role', 'user']
				},
				{
					key: 'stat_msg',
					expected: ['cap', 'used'],
					actual: ['total', 'used']
				},
				{
					key: 'user_msg',
					expected: ['name'],
					actual: ['username']
				}
			]);
		});
	});

	describe('boundary cases', () => {
		it('handles empty maps gracefully', () => {
			const base = {};
			const target = {};

			expect(Array.from(keySet(base))).toEqual([]);
			expect(missing(keySet(base), keySet(target))).toEqual([]);

			const result = compareLocales(base, target);
			expect(result.missingKeys).toEqual([]);
			expect(result.extraKeys).toEqual([]);
			expect(result.emptyValues).toEqual([]);
			expect(result.placeholderMismatches).toEqual([]);
		});

		it('handles single key maps', () => {
			const base = { solo: 'Only one' };
			const target = { solo: 'Nur eins' };

			expect(Array.from(keySet(base))).toEqual(['solo']);
			const result = compareLocales(base, target);
			expect(result.missingKeys).toEqual([]);
			expect(result.extraKeys).toEqual([]);
		});

		it('retains empty nested object keys', () => {
			const base = { group: {} };
			expect(Array.from(keySet(base))).toEqual(['group']);
		});

		it('flattens and validates nested objects per format specifications', () => {
			const base = {
				nav: {
					dashboard: 'Dashboard',
					settings: {
						title: 'Settings',
						save: 'Save {target}'
					}
				}
			};
			const target = {
				nav: {
					dashboard: 'Übersicht',
					settings: {
						title: 'Einstellungen',
						save: 'Speichere {target}'
					}
				}
			};

			expect(Array.from(keySet(base)).sort()).toEqual([
				'nav.dashboard',
				'nav.settings.save',
				'nav.settings.title'
			]);

			const result = compareLocales(base, target);
			expect(result.missingKeys).toEqual([]);
			expect(result.extraKeys).toEqual([]);
			expect(result.emptyValues).toEqual([]);
			expect(result.placeholderMismatches).toEqual([]);
		});

		it('detects nested missing keys and empty values', () => {
			const base = {
				group: {
					a: 'Alpha',
					b: 'Beta'
				}
			};
			const target = {
				group: {
					a: '   '
				}
			};

			const result = compareLocales(base, target);
			expect(result.missingKeys).toEqual(['group.b']);
			expect(result.emptyValues).toEqual(['group.a']);
		});

		it('ignores $schema key at root', () => {
			const catalog = {
				$schema: 'https://inlang.com/schema/inlang-message-format',
				greeting: 'Hello'
			};

			expect(Array.from(keySet(catalog))).toEqual(['greeting']);
			expect(findEmptyValues(catalog)).toEqual([]);
		});

		it('extracts placeholders from array of message variants and nested structures', () => {
			const variants = [
				{ match: { count: '1' }, value: 'One item for {user}' },
				{ match: { count: '*' }, value: '{count: number} items for {user}' }
			];

			expect(Array.from(placeholders(variants)).sort()).toEqual(['count', 'user']);
			expect(Array.from(placeholders({ sub: 'nested {name}' }))).toEqual(['name']);
			expect(Array.from(placeholders(123))).toEqual([]);
		});

		it('refuses a catalog or value past the node cap', () => {
			const huge = Object.fromEntries(Array.from({ length: 100_001 }, (_, i) => [`k${i}`, 'v']));
			expect(() => keySet(huge)).toThrow(/exceeds 100000 nodes/);
			expect(() => placeholders(Array.from({ length: 100_000 }, () => 'v'))).toThrow(
				/exceeds 100000 nodes/
			);
			expect(keySet(Object.fromEntries([['only', 'v']])).size).toBe(1);
		});

		it('evaluates getValue across flat and nested paths', () => {
			const record = {
				flat: 'value',
				nested: { child: 'deep' }
			};

			expect(getValue(record, 'flat')).toBe('value');
			expect(getValue(record, 'nested.child')).toBe('deep');
			expect(getValue(record, 'nested.missing')).toBeUndefined();
			expect(getValue(record, 'nonexistent.child')).toBeUndefined();
			expect(getValue(record, 'flat.child')).toBeUndefined();
		});
	});
});

describe('filesystem messages/*.json parity', () => {
	const webDir = path.resolve(import.meta.dirname, '../..');
	const settingsPath = path.join(webDir, 'project.inlang/settings.json');
	const messagesDir = path.join(webDir, 'messages');

	const settings = JSON.parse(fs.readFileSync(settingsPath, 'utf8')) as {
		baseLocale?: string;
	};
	const baseLocale = settings.baseLocale ?? 'en';

	const messageFiles = fs
		.readdirSync(messagesDir)
		.filter((file) => file.endsWith('.json'))
		.sort();

	const messagesByLocale = new Map<string, Record<string, unknown>>();
	for (const file of messageFiles) {
		const locale = path.basename(file, '.json');
		const content = JSON.parse(fs.readFileSync(path.join(messagesDir, file), 'utf8')) as Record<
			string,
			unknown
		>;
		messagesByLocale.set(locale, content);
	}

	it(`loads baseLocale "${baseLocale}" message file`, () => {
		expect(messagesByLocale.has(baseLocale)).toBe(true);
	});

	it(`has no empty string values in base locale "${baseLocale}"`, () => {
		const baseContent = messagesByLocale.get(baseLocale) ?? {};
		expect(findEmptyValues(baseContent)).toEqual([]);
	});

	const otherLocales = Array.from(messagesByLocale.keys()).filter((loc) => loc !== baseLocale);

	for (const locale of otherLocales) {
		describe(`locale: ${locale} vs ${baseLocale}`, () => {
			it(`matches base key set exactly`, () => {
				const baseContent = messagesByLocale.get(baseLocale) ?? {};
				const localeContent = messagesByLocale.get(locale) ?? {};
				const result = compareLocales(baseContent, localeContent);
				expect(result.missingKeys, `Missing keys in ${locale}`).toEqual([]);
				expect(result.extraKeys, `Extra keys in ${locale}`).toEqual([]);
			});

			it(`has no empty string values`, () => {
				const localeContent = messagesByLocale.get(locale) ?? {};
				expect(findEmptyValues(localeContent), `Empty values in ${locale}`).toEqual([]);
			});

			it(`matches base placeholders for every shared key`, () => {
				const baseContent = messagesByLocale.get(baseLocale) ?? {};
				const localeContent = messagesByLocale.get(locale) ?? {};
				const result = compareLocales(baseContent, localeContent);
				expect(result.placeholderMismatches, `Placeholder mismatches in ${locale}`).toEqual([]);
			});
		});
	}
});
