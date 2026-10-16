// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { describe, expect, it } from 'vitest';
import { fieldErrors, filledPart } from './problem.js';

describe('fieldErrors', () => {
	it('maps invalid-params to their member names', () => {
		const err = new ProblemError({
			type: 'about:blank',
			status: 422,
			invalidParams: [
				{ name: 'name', reason: 'string: len 0 less than minimum 1' },
				{ name: 'max_active', reason: 'int: value 5000 greater than maximum 1000' }
			]
		});
		expect(fieldErrors(err)).toEqual({
			name: 'string: len 0 less than minimum 1',
			max_active: 'int: value 5000 greater than maximum 1000'
		});
	});

	it('is empty for a problem without invalid-params and for other errors', () => {
		expect(fieldErrors(new ProblemError({ type: 'about:blank', status: 422 }))).toEqual({});
		expect(fieldErrors(new Error('boom'))).toEqual({});
		expect(fieldErrors(null)).toEqual({});
	});
});

describe('filledPart', () => {
	it('leaves an empty value out of the request body', () => {
		expect(filledPart('api_key', '')).toEqual({});
		expect(filledPart('instance_id', '')).toEqual({});
	});

	it('carries a filled value', () => {
		expect(filledPart('secret', 's3cret')).toEqual({ secret: 's3cret' });
		expect(filledPart('instance_id', 'abc')).toEqual({ instance_id: 'abc' });
	});
});
