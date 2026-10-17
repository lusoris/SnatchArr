// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	logLevelLabel,
	logLevels,
	matchesLog,
	mergeLogs,
	parseLogFrame,
	type LogFilter,
	type LogRecord
} from './logs.js';

function rec(seq: number, over: Partial<LogRecord> = {}): LogRecord {
	return {
		seq,
		time: '2026-10-17T12:00:00Z',
		level: 'info',
		source: 'api',
		message: `m${seq}`,
		attrs: [],
		...over
	};
}

const all: LogFilter = { level: 'debug', q: '', source: '' };

describe('matchesLog', () => {
	it.each([
		['positive level at the minimum', rec(1, { level: 'warn' }), { ...all, level: 'warn' }, true],
		[
			'positive level above the minimum',
			rec(1, { level: 'error' }),
			{ ...all, level: 'warn' },
			true
		],
		[
			'negative level below the minimum',
			rec(1, { level: 'info' }),
			{ ...all, level: 'warn' },
			false
		],
		[
			'positive exact source',
			rec(1, { source: 'worker:w1' }),
			{ ...all, source: 'worker:w1' },
			true
		],
		['negative other source', rec(1, { source: 'api' }), { ...all, source: 'worker:w1' }, false],
		[
			'positive text in message ignores case',
			rec(1, { message: 'Sonarr down' }),
			{ ...all, q: 'SONARR' },
			true
		],
		[
			'positive text in attribute value',
			rec(1, { attrs: [{ key: 'run_id', value: 'R-42' }] }),
			{ ...all, q: 'r-42' },
			true
		],
		[
			'positive text in attribute key',
			rec(1, { attrs: [{ key: 'indexer', value: 'x' }] }),
			{ ...all, q: 'index' },
			true
		],
		['negative text nowhere', rec(1), { ...all, q: 'radarr' }, false]
	] as const)('%s', (_name, r, f, want) => {
		expect(matchesLog(r, f)).toBe(want);
	});
});

describe('parseLogFrame', () => {
	it('positive reads a record and defaults missing attributes', () => {
		const r = parseLogFrame(
			JSON.stringify({ seq: 3, time: 't', level: 'warn', source: 'api', message: 'x' })
		);
		expect(r?.seq).toBe(3);
		expect(r?.attrs).toEqual([]);
	});

	it.each([
		['negative not JSON', 'not json'],
		['negative no sequence', JSON.stringify({ level: 'info', message: 'x' })],
		['negative unknown level', JSON.stringify({ seq: 1, level: 'trace', message: 'x' })],
		['boundary empty frame', '']
	])('%s', (_name, data) => {
		expect(parseLogFrame(data)).toBeUndefined();
	});
});

describe('mergeLogs', () => {
	it('positive puts newer streamed records first, filtered and without duplicates', () => {
		const page = [rec(5), rec(4)];
		const streamed = [rec(5), rec(6, { level: 'debug' }), rec(7), rec(7), rec(8)];
		const got = mergeLogs(page, streamed, { ...all, level: 'info' }, 10).map((r) => r.seq);
		expect(got).toEqual([8, 7, 5, 4]);
	});

	it('boundary cuts at the limit', () => {
		expect(mergeLogs([rec(2), rec(1)], [rec(3)], all, 2).map((r) => r.seq)).toEqual([3, 2]);
	});

	it('boundary empty page keeps every streamed record', () => {
		expect(mergeLogs([], [rec(1), rec(2)], all, 10).map((r) => r.seq)).toEqual([2, 1]);
	});
});

describe('logLevelLabel', () => {
	it('has a label for every level', () => {
		for (const level of logLevels) expect(logLevelLabel(level)).not.toBe('');
		expect(logLevels).toEqual(['debug', 'info', 'warn', 'error']);
	});
});
