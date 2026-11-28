// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { ZodError } from 'zod';
import {
	appendRulePage,
	compareStoredRules,
	LIBRARY_PROCESSING_MEDIA_TYPES,
	LIBRARY_PROCESSING_SCOPES,
	libraryProcessingRuleInputSchema,
	mergePinnedRule,
	parseRuleInput,
	toRuleInput,
	type LibraryProcessingRule,
	type LibraryProcessingScope
} from './library-processing-rule.js';

const ID = '3f2b8c1e-5d4a-4e7b-9c6d-1a2b3c4d5e6f';
const NIL = '00000000-0000-0000-0000-000000000000';

type Target = 'instance_id' | 'item_id' | 'user_language' | 'root_path' | 'media_type';
const TARGETS: Target[] = ['instance_id', 'item_id', 'user_language', 'root_path', 'media_type'];

const VALUES: Record<Target, string> = {
	instance_id: ID,
	item_id: 'tt0111161',
	user_language: 'de',
	root_path: '/media/movies',
	media_type: 'anime'
};

const REQUIRED: Record<LibraryProcessingScope, Target[]> = {
	item_override: ['instance_id', 'item_id'],
	user_language: ['user_language'],
	library: ['instance_id', 'root_path'],
	instance: ['instance_id'],
	media_type: ['media_type'],
	global_default: []
};

function rule(languages: unknown[] = ['en', 'de']) {
	return { track: { languages } };
}

function build(scope: LibraryProcessingScope, overrides: Record<string, unknown> = {}) {
	const targets = Object.fromEntries(REQUIRED[scope].map((key) => [key, VALUES[key]]));
	return { scope, ...targets, rule: rule(), ...overrides };
}

function failure(value: unknown) {
	const result = libraryProcessingRuleInputSchema.safeParse(value);
	if (result.success) throw new Error('expected validation failure');
	return result.error.issues;
}

function failedPaths(value: unknown): string[] {
	return [...new Set(failure(value).map((issue) => issue.path.join('.')))];
}

const withLanguages = (languages: unknown[]) => build('global_default', { rule: rule(languages) });

/** Distinct, genuinely assigned ISO 639-1 codes (80 entries). */
const COMMON_ISO_639_1: string[] = [
	'af',
	'am',
	'ar',
	'as',
	'az',
	'be',
	'bg',
	'bn',
	'bo',
	'br',
	'bs',
	'ca',
	'cs',
	'cy',
	'da',
	'de',
	'el',
	'en',
	'es',
	'et',
	'eu',
	'fa',
	'fi',
	'fo',
	'fr',
	'ga',
	'gd',
	'gl',
	'gu',
	'he',
	'hi',
	'hr',
	'hu',
	'hy',
	'id',
	'is',
	'it',
	'ja',
	'ka',
	'kk',
	'km',
	'kn',
	'ko',
	'ku',
	'ky',
	'la',
	'lo',
	'lt',
	'lv',
	'mk',
	'ml',
	'mn',
	'mr',
	'ms',
	'mt',
	'my',
	'nb',
	'ne',
	'nl',
	'nn',
	'no',
	'pa',
	'pl',
	'ps',
	'pt',
	'ro',
	'ru',
	'sk',
	'sl',
	'sq',
	'sr',
	'sv',
	'sw',
	'ta',
	'te',
	'tg',
	'th',
	'tr',
	'uk',
	'ur'
];

describe('language count bound (MaxRuleLanguages = 64)', () => {
	it('uses a fixture of distinct codes large enough for the boundary', () => {
		expect(new Set(COMMON_ISO_639_1).size).toBe(COMMON_ISO_639_1.length);
		expect(COMMON_ISO_639_1.length).toBeGreaterThanOrEqual(65);
	});

	it('accepts exactly 64 distinct valid codes', () => {
		const codes = COMMON_ISO_639_1.slice(0, 64);
		expect(codes).toHaveLength(64);
		expect(parseRuleInput(withLanguages(codes)).rule.track.languages).toEqual(codes);
	});

	it('rejects 65 distinct valid codes with a single too_big issue', () => {
		const codes = COMMON_ISO_639_1.slice(0, 65);
		expect(codes).toHaveLength(65);
		const issues = failure(withLanguages(codes));
		expect(issues).toHaveLength(1);
		expect(issues[0]).toMatchObject({
			code: 'too_big',
			maximum: 64,
			path: ['rule', 'track', 'languages']
		});
	});
});

describe('item_id control characters (parity with Go validateItemID)', () => {
	it('accepts C1 controls, as the server only rejects < 0x20 and 0x7f', () => {
		const result = libraryProcessingRuleInputSchema.safeParse(
			build('item_override', { item_id: 'tt\u009f01' })
		);
		expect(result.success).toBe(true);
	});
});

describe('scope targets', () => {
	it('declares exactly the six scopes and six media types', () => {
		expect(LIBRARY_PROCESSING_SCOPES).toEqual([
			'item_override',
			'user_language',
			'library',
			'instance',
			'media_type',
			'global_default'
		]);
		expect(LIBRARY_PROCESSING_MEDIA_TYPES).toEqual([
			'movie',
			'episode',
			'anime',
			'music',
			'audiobook',
			'stash_scene'
		]);
	});

	it.each(LIBRARY_PROCESSING_SCOPES)('accepts a minimal %s input unchanged', (scope) => {
		const input = build(scope);
		const out = parseRuleInput(input);
		expect(out).toStrictEqual(input);
		expect(out.rule).not.toBe(input.rule);
	});

	// global_default has no required targets, so that combination would assert nothing; skip it.
	it.each(LIBRARY_PROCESSING_SCOPES.filter((scope) => REQUIRED[scope].length > 0))(
		'%s reports each missing required target',
		(scope) => {
			for (const key of REQUIRED[scope]) {
				const value: Record<string, unknown> = build(scope);
				delete value[key];
				expect(failure(value).map((i) => [i.path.join('.'), i.message])).toEqual([
					[key, `${key} is required for scope ${scope}`]
				]);
			}
		}
	);

	it.each(LIBRARY_PROCESSING_SCOPES)('%s forbids every foreign target field', (scope) => {
		for (const key of TARGETS.filter((k) => !REQUIRED[scope].includes(k))) {
			const issues = failure(build(scope, { [key]: VALUES[key] }));
			expect(issues.map((i) => [i.path.join('.'), i.message])).toEqual([
				[key, `${key} is forbidden for scope ${scope}`]
			]);
		}
	});

	it.each(['Library', 'user', 'global', '', 'item', null, 3])('rejects scope %j', (scope) => {
		expect(failedPaths({ scope, rule: rule() })).toEqual(['scope']);
	});

	it('rejects a missing scope', () => {
		expect(failedPaths({ rule: rule() })).toEqual(['scope']);
	});
});

describe('media_type target', () => {
	it.each(LIBRARY_PROCESSING_MEDIA_TYPES)('accepts %s', (mediaType) => {
		const input = build('media_type', { media_type: mediaType });
		expect(parseRuleInput(input)).toStrictEqual(input);
	});

	it.each(['Movie', 'video', 'tv', 'stash', '', null, 1])('rejects %j', (mediaType) => {
		expect(failedPaths(build('media_type', { media_type: mediaType }))).toEqual(['media_type']);
	});
});

describe('nullable versus absent', () => {
	it('rejects null for an optional target instead of treating it as absent', () => {
		const issues = failure(build('global_default', { media_type: null }));
		expect(issues).toHaveLength(1);
		expect(issues[0]).toMatchObject({ code: 'invalid_value', path: ['media_type'] });
		expect(failedPaths(build('instance', { instance_id: null }))).toEqual(['instance_id']);
	});

	it('rejects a key explicitly set to undefined, even for a required target', () => {
		const issues = failure(build('instance', { instance_id: undefined }));
		expect(issues.map((i) => [i.path.join('.'), i.message])).toEqual([
			['instance_id', 'instance_id must be omitted, not set to undefined']
		]);
	});

	it('rejects null for the rule and its nested parts', () => {
		expect(failedPaths(build('global_default', { rule: null }))).toEqual(['rule']);
		expect(failedPaths(build('global_default', { rule: { track: null } }))).toEqual(['rule.track']);
		expect(failedPaths(withLanguages(null as never))).toEqual(['rule.track.languages']);
	});
});

describe('strict objects', () => {
	it('rejects unknown top-level keys', () => {
		const issues = failure(build('global_default', { extra: 1 }));
		expect(issues[0]).toMatchObject({ code: 'unrecognized_keys', keys: ['extra'], path: [] });
	});

	it('rejects server-managed fields on input', () => {
		const value = build('global_default', {
			id: ID,
			created_at: '2026-01-01T00:00:00Z',
			updated_at: '2026-01-01T00:00:00Z'
		});
		const issue = failure(value)[0] as unknown as { keys: string[] };
		expect([...issue.keys].sort()).toEqual(['created_at', 'id', 'updated_at']);
	});

	it('rejects unknown keys inside rule and track', () => {
		expect(failedPaths(build('global_default', { rule: { ...rule(), extra: true } }))).toEqual([
			'rule'
		]);
		expect(
			failedPaths(build('global_default', { rule: { track: { languages: ['en'], keep: 1 } } }))
		).toEqual(['rule.track']);
	});

	it.each([null, undefined, [], 'x', 42, true])('parseRuleInput throws for %j', (value) => {
		expect(() => parseRuleInput(value)).toThrow(ZodError);
	});
});

describe('nested producer rule', () => {
	it('requires rule, track and languages', () => {
		const missingRule = build('global_default');
		delete (missingRule as Record<string, unknown>).rule;
		expect(failedPaths(missingRule)).toEqual(['rule']);
		expect(failedPaths(build('global_default', { rule: {} }))).toEqual(['rule.track']);
		expect(failedPaths(build('global_default', { rule: { track: {} } }))).toEqual([
			'rule.track.languages'
		]);
	});

	it('requires at least one language', () => {
		const issues = failure(withLanguages([]));
		expect(issues[0]).toMatchObject({ code: 'too_small', path: ['rule', 'track', 'languages'] });
	});

	it('reports the second occurrence of an exact duplicate', () => {
		const issues = failure(withLanguages(['en', 'de', 'en']));
		expect(issues).toHaveLength(1);
		expect(issues[0].path).toEqual(['rule', 'track', 'languages', 2]);
		expect(issues[0].message).toBe('duplicate language en');
	});

	it('preserves language order and spelling', () => {
		const out = parseRuleInput(withLanguages(['de', 'en', 'ja']));
		expect(out.rule.track.languages).toEqual(['de', 'en', 'ja']);
	});

	it.each([false, 5, null, ['en'], {}])('rejects non-string language %j', (code) => {
		expect(failedPaths(withLanguages(['en', code]))).toEqual(['rule.track.languages.1']);
	});
});

describe('language codes', () => {
	it('accepts canonical aliases, 639-2 and 639-3 codes without a registry', () => {
		const codes = [
			'en',
			'eng',
			'iw',
			'heb',
			'in',
			'ger',
			'deu',
			'fre',
			'chi',
			'fil',
			'yue',
			'qua',
			'mul'
		];
		expect(parseRuleInput(withLanguages(codes)).rule.track.languages).toEqual(codes);
	});

	it('treats alias spellings as distinct strings, as the server does', () => {
		const codes = ['de', 'deu', 'ger'];
		expect(parseRuleInput(withLanguages(codes)).rule.track.languages).toEqual(codes);
	});

	it.each([
		'EN',
		'En',
		'e',
		'engl',
		'e1',
		'en-US',
		'en_US',
		' en',
		'en ',
		'',
		'él',
		'und',
		'qaa',
		'qtz'
	])('rejects rule language %j', (code) => {
		expect(failedPaths(withLanguages(['en', code]))).toEqual(['rule.track.languages.1']);
	});

	it.each(['en', 'eng', 'iw', 'ger', 'yue', 'qua'])('accepts user_language %s verbatim', (code) => {
		const input = build('user_language', { user_language: code });
		expect(parseRuleInput(input)).toStrictEqual(input);
	});

	it.each(['EN', 'e', 'engl', 'en ', ' en', '', 'und', 'qaa', 'qtz', null, 7])(
		'rejects user_language %j',
		(code) => {
			expect(failedPaths(build('user_language', { user_language: code }))).toEqual([
				'user_language'
			]);
		}
	);
});

describe('instance_id', () => {
	it('accepts upper-case and non-RFC-variant UUIDs unchanged', () => {
		for (const id of [ID.toUpperCase(), '12345678-1234-1234-1234-123456789abc']) {
			const input = build('instance', { instance_id: id });
			expect(parseRuleInput(input)).toStrictEqual(input);
		}
	});

	it('rejects the nil UUID with a specific message', () => {
		const issues = failure(build('instance', { instance_id: NIL }));
		expect(issues.map((i) => i.message)).toEqual(['instance_id must not be the nil UUID']);
	});

	it.each(['', 'not-a-uuid', `{${ID}}`, ID.replaceAll('-', ''), ` ${ID}`, 12])(
		'rejects %j',
		(id) => {
			expect(failedPaths(build('item_override', { instance_id: id }))).toEqual(['instance_id']);
		}
	);
});

describe('item_id', () => {
	it.each(['a', 'a'.repeat(128), '😀'.repeat(128), 'tt 0111161', 'Season 1/Episode 2'])(
		'accepts %#',
		(id) => {
			const input = build('item_override', { item_id: id });
			expect(parseRuleInput(input)).toStrictEqual(input);
		}
	);

	it.each([
		'',
		'a'.repeat(129),
		'😀'.repeat(129),
		' a',
		'a ',
		'\u00a0a',
		'a\u0000b',
		'a\tb',
		'\u007f',
		5,
		null
	])('rejects %#', (id) => {
		expect(failedPaths(build('item_override', { item_id: id }))).toEqual(['item_id']);
	});
});

describe('root_path', () => {
	const accepted = [
		'/',
		'/media',
		'/media/movies',
		'/a/...',
		'/a/. b',
		'/a/.hidden',
		'/' + 'a'.repeat(4095),
		'/' + 'é'.repeat(2047)
	];

	it.each(accepted)('accepts %#', (path) => {
		const input = build('library', { root_path: path });
		expect(parseRuleInput(input)).toStrictEqual(input);
	});

	const rejected = [
		'',
		'media',
		' /media',
		'C:\\media',
		'/a/',
		'//a',
		'/a//b',
		'/a/./b',
		'/a/../b',
		'/.',
		'/..',
		'/a/. /b',
		'/a/.. ',
		'/ ',
		'/a\u0000b',
		'/' + 'a'.repeat(4096),
		'/' + 'é'.repeat(2048),
		9,
		null
	];

	it.each(rejected)('rejects %#', (path) => {
		expect(failedPaths(build('library', { root_path: path }))).toEqual(['root_path']);
	});
});

describe('transport data', () => {
	it('accepts data that came through a JSON round trip', () => {
		const input = build('library');
		expect(parseRuleInput(JSON.parse(JSON.stringify(input)))).toStrictEqual(input);
	});

	it('rejects YAML 1.1 style booleans where a language string is required', () => {
		expect(failedPaths(build('user_language', { user_language: false }))).toEqual([
			'user_language'
		]);
		expect(failedPaths(withLanguages([false]))).toEqual(['rule.track.languages.0']);
	});
});

describe('toRuleInput', () => {
	const serverFields = {
		id: ID,
		created_at: '2026-01-01T00:00:00Z',
		updated_at: '2026-01-02T00:00:00Z'
	};

	function stored(scope: LibraryProcessingScope): LibraryProcessingRule {
		return { ...build(scope), ...serverFields } as unknown as LibraryProcessingRule;
	}

	it('strips only id, created_at and updated_at', () => {
		expect(toRuleInput(stored('library'))).toStrictEqual({
			scope: 'library',
			instance_id: ID,
			root_path: '/media/movies',
			rule: { track: { languages: ['en', 'de'] } }
		});
	});

	it('does not add keys for absent optional fields', () => {
		const out = toRuleInput(stored('global_default'));
		expect(Object.keys(out).sort()).toEqual(['rule', 'scope']);
	});

	it.each(LIBRARY_PROCESSING_SCOPES)(
		'round-trips a stored %s rule through parseRuleInput',
		(scope) => {
			const input = toRuleInput(stored(scope));
			expect(parseRuleInput(input)).toStrictEqual(input);
		}
	);

	it('returns an independent rule and leaves the stored value untouched', () => {
		const source = stored('instance');
		const out = toRuleInput(source);
		out.rule.track.languages.push('fr');
		expect(source.rule.track.languages).toEqual(['en', 'de']);
		expect(source.id).toBe(ID);
	});

	it('does not validate or normalize what it carries over', () => {
		const source = stored('global_default');
		source.rule.track.languages = ['EN'];
		expect(toRuleInput(source).rule.track.languages).toEqual(['EN']);
	});
});

describe('pagination ordering and selection helpers', () => {
	function makeStored(
		id: string,
		scope: LibraryProcessingScope,
		createdAt: string
	): LibraryProcessingRule {
		return {
			...build(scope),
			id,
			created_at: createdAt,
			updated_at: createdAt
		} as unknown as LibraryProcessingRule;
	}

	it('orders rules by scope precedence, microsecond created_at, then id ascending', () => {
		const global = makeStored(
			'018f0000-0000-7000-8000-000000000009',
			'global_default',
			'2026-01-01T00:00:00Z'
		);
		const libLater = makeStored(
			'018f0000-0000-7000-8000-000000000001',
			'library',
			'2026-01-01T00:00:00.000002Z'
		);
		const libEarlierB = makeStored(
			'018f0000-0000-7000-8000-000000000005',
			'library',
			'2026-01-01T00:00:00.000001Z'
		);
		const libEarlierA = makeStored(
			'018f0000-0000-7000-8000-000000000002',
			'library',
			'2026-01-01T00:00:00.000001Z'
		);
		const itemOverride = makeStored(
			'018f0000-0000-7000-8000-000000000099',
			'item_override',
			'2026-01-02T00:00:00Z'
		);

		const sorted = [global, libLater, libEarlierB, libEarlierA, itemOverride].sort(
			compareStoredRules
		);
		expect(sorted.map((r) => r.id)).toEqual([
			itemOverride.id,
			libEarlierA.id,
			libEarlierB.id,
			libLater.id,
			global.id
		]);
		expect(compareStoredRules(global, global)).toBe(0);
	});

	it('appends new page items in order while deduplicating existing IDs', () => {
		const a = makeStored(
			'018f0000-0000-7000-8000-000000000001',
			'item_override',
			'2026-01-01T00:00:00Z'
		);
		const b = makeStored('018f0000-0000-7000-8000-000000000002', 'library', '2026-01-01T00:00:00Z');
		const c = makeStored(
			'018f0000-0000-7000-8000-000000000003',
			'global_default',
			'2026-01-01T00:00:00Z'
		);

		expect(appendRulePage([a, b], [b, c]).map((r) => r.id)).toEqual([a.id, b.id, c.id]);
	});

	it('replaces a loaded rule by ID with the incoming row and re-sorts by precedence', () => {
		const first = makeStored(
			'018f0000-0000-7000-8000-000000000001',
			'item_override',
			'2026-01-01T00:00:00Z'
		);
		const stale = makeStored(
			'018f0000-0000-7000-8000-000000000002',
			'global_default',
			'2026-01-01T00:00:00Z'
		);
		const last = makeStored(
			'018f0000-0000-7000-8000-000000000003',
			'media_type',
			'2026-01-01T00:00:00Z'
		);
		const fresh = makeStored(stale.id, 'library', '2026-01-01T00:00:00Z');

		const merged = appendRulePage([first, stale, last], [fresh]);

		expect(merged.map((r) => r.id)).toEqual([first.id, fresh.id, last.id]);
		expect(merged[1]).toBe(fresh);
		expect(merged[0]).toBe(first);
		expect(merged[2]).toBe(last);
	});

	it('keeps IDs unique for repeated incoming rows and returns an untouched copy for an empty page', () => {
		const a = makeStored(
			'018f0000-0000-7000-8000-000000000001',
			'item_override',
			'2026-01-01T00:00:00Z'
		);
		const b = makeStored('018f0000-0000-7000-8000-000000000002', 'library', '2026-01-01T00:00:00Z');
		const existing = [b, a];

		expect(appendRulePage(existing, [a, a]).map((r) => r.id)).toEqual([a.id, b.id]);
		const empty = appendRulePage(existing, []);
		expect(empty).toEqual(existing);
		expect(empty).not.toBe(existing);
	});

	it('merges an off-page pinned rule at its precedence slot without duplicating loaded rules', () => {
		const first = makeStored(
			'018f0000-0000-7000-8000-000000000001',
			'item_override',
			'2026-01-01T00:00:00Z'
		);
		const middle = makeStored(
			'018f0000-0000-7000-8000-000000000002',
			'library',
			'2026-01-01T00:00:00Z'
		);
		const last = makeStored(
			'018f0000-0000-7000-8000-000000000003',
			'global_default',
			'2026-01-01T00:00:00Z'
		);

		expect(mergePinnedRule([first, last], middle).map((r) => r.id)).toEqual([
			first.id,
			middle.id,
			last.id
		]);
		expect(mergePinnedRule([first, middle], last).map((r) => r.id)).toEqual([
			first.id,
			middle.id,
			last.id
		]);
		expect(mergePinnedRule([first, middle], first).map((r) => r.id)).toEqual([first.id, middle.id]);
		expect(mergePinnedRule([first], null).map((r) => r.id)).toEqual([first.id]);
	});
});
