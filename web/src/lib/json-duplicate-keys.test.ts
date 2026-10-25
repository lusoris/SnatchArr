// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { findDuplicateKeys } from './json-duplicate-keys.js';

describe('findDuplicateKeys', () => {
	describe('positive tests', () => {
		it('detects a flat duplicate key', () => {
			const json = JSON.stringify({ a: 1 }) + '\n';
			const text = json.replace('"a":1', '"nav_configarr":"one","nav_configarr":"two"');
			expect(findDuplicateKeys(text)).toEqual(['nav_configarr']);
		});

		it('detects a nested duplicate key "a.b"', () => {
			const text = '{"a": {"b": 1, "b": 2}}';
			expect(findDuplicateKeys(text)).toEqual(['a.b']);
		});

		it('detects escaped-equal keys "a" vs "\\u0061"', () => {
			const text = '{"a": 1, "\\u0061": 2}';
			expect(findDuplicateKeys(text)).toEqual(['a']);
		});

		it('detects duplicate keys in an object inside an array', () => {
			const text = '{"items": [{"x": 1, "x": 2}]}';
			expect(findDuplicateKeys(text)).toEqual(['items.x']);

			const rootArray = '[{"x": 1, "x": 2}]';
			expect(findDuplicateKeys(rootArray)).toEqual(['x']);
		});
	});

	describe('negative tests', () => {
		it('does not treat the same key in two sibling objects as a duplicate', () => {
			const text = '{"first": {"k": 1}, "second": {"k": 2}}';
			expect(findDuplicateKeys(text)).toEqual([]);

			const arrayText = '[{"k": 1}, {"k": 2}]';
			expect(findDuplicateKeys(arrayText)).toEqual([]);
		});

		it('does not treat key text appearing inside a string value as a key', () => {
			const text = '{"k": "k", "nav_configarr": "nav_configarr"}';
			expect(findDuplicateKeys(text)).toEqual([]);
		});
	});

	describe('boundary tests', () => {
		it('accepts an empty object', () => {
			expect(findDuplicateKeys('{}')).toEqual([]);
			expect(findDuplicateKeys('  {  }  ')).toEqual([]);
		});

		it('accepts an empty array', () => {
			expect(findDuplicateKeys('[]')).toEqual([]);
			expect(findDuplicateKeys('  [  ]  ')).toEqual([]);
		});

		it('throws on malformed inputs', () => {
			expect(() => findDuplicateKeys('')).toThrow();
			expect(() => findDuplicateKeys('   ')).toThrow();
			expect(() => findDuplicateKeys('{"a": 1')).toThrow();
			expect(() => findDuplicateKeys('{"a": [1}')).toThrow();
			expect(() => findDuplicateKeys('[1, 2}')).toThrow();
			expect(() => findDuplicateKeys('{"a": 1}}')).toThrow();
			expect(() => findDuplicateKeys('{"a": 1,}')).toThrow();
			expect(() => findDuplicateKeys('{"a": "unterminated')).toThrow();
			expect(() => findDuplicateKeys('{"unterminated')).toThrow();
			expect(() => findDuplicateKeys('{} 123')).toThrow();
		});
	});
});
