// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';
import { m } from './paraglide/messages.js';

export type LogRecord = components['schemas']['LogRecord'];
export type LogLevel = components['schemas']['LogLevel'];

/** The filters of the log viewer, in the shape of the listLogs query. */
export interface LogFilter {
	level: LogLevel;
	q: string;
	source: string;
}

// Severity order of the OpenAPI enumeration: a level added there fails the type check here.
const rank = { debug: 0, info: 1, warn: 2, error: 3 } as const satisfies Record<LogLevel, number>;

export const logLevels = Object.keys(rank) as LogLevel[];

const levelLabels = {
	debug: m.logs_level_debug,
	info: m.logs_level_info,
	warn: m.logs_level_warn,
	error: m.logs_level_error
} as const satisfies Record<LogLevel, () => string>;

/** The level as the UI shows it. */
export function logLevelLabel(level: LogLevel): string {
	return levelLabels[level]();
}

/** The source filter that selects every worker; one worker's records carry `worker:<id>`. */
export const workersSource = 'worker';

/** Whether a record comes from the filtered source, with the API's rules. */
export function fromSource(rec: LogRecord, source: string): boolean {
	if (source === '' || rec.source === source) return true;
	return source === workersSource && rec.source.startsWith(`${workersSource}:`);
}

/**
 * Whether a record passes the filter, with the API's rules: level at or above the minimum, the
 * source (exact, or every worker for `worker`), and the text anywhere in the message or an
 * attribute key or value, ignoring case.
 */
export function matchesLog(rec: LogRecord, f: LogFilter): boolean {
	if (rank[rec.level] < rank[f.level]) return false;
	if (!fromSource(rec, f.source)) return false;
	const q = f.q.toLowerCase();
	if (q === '') return true;
	if (rec.message.toLowerCase().includes(q)) return true;
	return rec.attrs.some(
		(a) => a.key.toLowerCase().includes(q) || a.value.toLowerCase().includes(q)
	);
}

/** Parses one stream frame; a frame that is no record is skipped. */
export function parseLogFrame(data: string): LogRecord | undefined {
	try {
		const rec = JSON.parse(data) as Partial<LogRecord>;
		if (typeof rec.seq !== 'number' || typeof rec.message !== 'string') return undefined;
		if (typeof rec.level !== 'string' || !Object.hasOwn(rank, rec.level)) return undefined;
		return { ...rec, attrs: Array.isArray(rec.attrs) ? rec.attrs : [] } as LogRecord;
	} catch {
		return undefined;
	}
}

/**
 * Newest first: streamed records newer than the fetched page, then the page, at most `limit`.
 * The stream also replays records the page already holds; the sequence number drops those.
 */
export function mergeLogs(
	page: readonly LogRecord[],
	streamed: readonly LogRecord[],
	f: LogFilter,
	limit: number
): LogRecord[] {
	const newest = page[0]?.seq ?? 0;
	const bySeq = new Map(
		streamed.filter((r) => r.seq > newest && matchesLog(r, f)).map((r) => [r.seq, r])
	);
	const fresh = [...bySeq.values()].sort((a, b) => b.seq - a.seq);
	return [...fresh, ...page].slice(0, limit);
}

/** Sources to suggest in the filter: api, every worker, then each source the records show. */
export function logSources(records: readonly LogRecord[]): string[] {
	return [...new Set(['api', workersSource, ...records.map((r) => r.source)])];
}
