// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { isDialTimeout } from './egress.js';

describe('isDialTimeout', () => {
	it.each([
		'Get "https://catalogue.example/graphql": dial tcp 203.0.113.7:443: i/o timeout',
		'dial tcp [2001:db8::7]:443: connect: connection timed out',
		'dial tcp4 203.0.113.7:443: i/o timeout',
		'dial tcp6 [2001:db8::7]:443: i/o timeout',
		'Post "https://catalogue.example/graphql": dial tcp 203.0.113.7:443: context deadline exceeded'
	])('names a dial timeout: %s', (error) => {
		expect(isDialTimeout(error)).toBe(true);
	});

	it.each([
		'dial tcp 203.0.113.7:443: connect: connection refused',
		'dial udp 203.0.113.7:53: i/o timeout',
		'Get "https://catalogue.example/graphql": context deadline exceeded (Client.Timeout exceeded while awaiting headers)',
		'catalogue answered 401 Unauthorized',
		'tls: failed to verify certificate'
	])('leaves other errors alone: %s', (error) => {
		expect(isDialTimeout(error)).toBe(false);
	});

	it.each([null, undefined, ''])('is false without an error: %s', (error) => {
		expect(isDialTimeout(error)).toBe(false);
	});
});
