// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	MAX_RULE_DOCUMENT_CHARS,
	readRuleDocument,
	writeRuleDocument
} from './library-processing-document.js';
import {
	toRuleInput,
	type LibraryProcessingRule,
	type LibraryProcessingRuleInput
} from './library-processing-rule.js';

const stored: LibraryProcessingRule = {
	id: '123e4567-e89b-12d3-a456-426614174000',
	scope: 'user_language',
	user_language: 'no',
	rule: { track: { languages: ['no', 'en'] } },
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-02T00:00:00Z'
};

const validInput: LibraryProcessingRuleInput = {
	scope: 'user_language',
	user_language: 'no',
	rule: { track: { languages: ['no', 'en'] } }
};

const formats = ['json', 'yaml'] as const;

describe('library processing rule documents', () => {
	describe.each(formats)('%s roundtrip', (format) => {
		it('roundtrips a producer-valid user_language rule', () => {
			const text = writeRuleDocument(stored, format);
			const parsed = readRuleDocument(text, format);
			expect(parsed).toEqual(toRuleInput(stored));
			expect(parsed.user_language).toBe('no');
			expect(parsed.rule.track.languages).toEqual(['no', 'en']);
			expect(parsed.media_type).toBeUndefined();
		});

		it('rejects oversized documents', () => {
			expect(() => readRuleDocument('x'.repeat(MAX_RULE_DOCUMENT_CHARS + 1), format)).toThrow();
		});
	});

	it('keeps the string "no" quoted in YAML output', () => {
		const text = writeRuleDocument(stored, 'yaml');
		expect(text).toContain("user_language: 'no'");
		expect(text).toContain("- 'no'");
	});

	it('does not write stored-only fields', () => {
		expect(writeRuleDocument(stored, 'json')).not.toContain('created_at');
		expect(writeRuleDocument(stored, 'yaml')).not.toContain('created_at');
	});

	describe('invalid scope combinations', () => {
		const invalid: LibraryProcessingRuleInput[] = [
			{ ...validInput, user_language: undefined },
			{ ...validInput, instance_id: '123e4567-e89b-12d3-a456-426614174000' },
			{ ...validInput, root_path: '/media' },
			{ ...validInput, scope: 'library' },
			{ ...validInput, scope: 'media_type' }
		];

		it.each(invalid)('rejects JSON %#', (input) => {
			expect(() => readRuleDocument(JSON.stringify(input), 'json')).toThrow();
		});

		it('rejects YAML with a forbidden field for the scope', () => {
			const text = [
				'scope: user_language',
				"user_language: 'no'",
				"root_path: '/media'",
				'rule:',
				'  track:',
				'    languages:',
				"      - 'no'",
				''
			].join('\n');
			expect(() => readRuleDocument(text, 'yaml')).toThrow();
		});
	});

	describe('duplicate fields', () => {
		it('rejects duplicate JSON keys', () => {
			const text =
				'{"scope":"user_language","scope":"user_language","user_language":"no",' +
				'"rule":{"track":{"languages":["no"]}}}';
			expect(() => readRuleDocument(text, 'json')).toThrow();
		});

		it('rejects duplicate nested JSON keys', () => {
			const text =
				'{"scope":"user_language","user_language":"no",' +
				'"rule":{"track":{"languages":["no"],"languages":["en"]}}}';
			expect(() => readRuleDocument(text, 'json')).toThrow();
		});

		it('rejects duplicate YAML keys', () => {
			const text = [
				'scope: user_language',
				"user_language: 'no'",
				"user_language: 'en'",
				'rule:',
				'  track:',
				'    languages:',
				"      - 'no'",
				''
			].join('\n');
			expect(() => readRuleDocument(text, 'yaml')).toThrow();
		});
	});

	describe('multiple documents', () => {
		it('rejects multiple YAML documents', () => {
			const one = writeRuleDocument(stored, 'yaml');
			expect(() => readRuleDocument(`${one}---\n${one}`, 'yaml')).toThrow();
		});

		it('rejects trailing JSON content', () => {
			const one = JSON.stringify(validInput);
			expect(() => readRuleDocument(`${one}${one}`, 'json')).toThrow();
		});

		it('rejects an empty YAML document', () => {
			expect(() => readRuleDocument('', 'yaml')).toThrow();
		});
	});

	describe('parse failures', () => {
		it('rejects malformed JSON', () => {
			expect(() => readRuleDocument('{"scope":', 'json')).toThrow();
		});

		it('rejects malformed YAML', () => {
			expect(() => readRuleDocument('scope: [user_language', 'yaml')).toThrow();
		});

		it('rejects non-object roots', () => {
			expect(() => readRuleDocument('[]', 'json')).toThrow();
			expect(() => readRuleDocument('- a', 'yaml')).toThrow();
		});
	});
});
