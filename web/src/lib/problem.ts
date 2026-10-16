// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';

/** Field-level reasons of an RFC 9457 problem (`invalid-params`), keyed by request member name. */
export function fieldErrors(err: unknown): Record<string, string> {
	if (!(err instanceof ProblemError) || !err.invalidParams) return {};
	return Object.fromEntries(err.invalidParams.map((p) => [p.name, p.reason]));
}

/**
 * Leaves an empty form value out of a request body. A write-only secret left empty means "keep
 * the stored value"; an optional reference left empty means "none".
 */
export function filledPart<K extends string>(key: K, value: string): Partial<Record<K, string>> {
	return value === '' ? {} : ({ [key]: value } as Record<K, string>);
}
