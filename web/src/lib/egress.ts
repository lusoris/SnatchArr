// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// A dial that never completes is what a NetworkPolicy that drops the packets looks like from
// the API: Go reports `dial tcp <addr>: i/o timeout`, `connect: connection timed out` or a
// deadline that expired while dialling. A refused or reset connection, a TLS failure or an
// HTTP error status reached the far end, so no policy blocked it and no hint is shown.
const DIAL = /\bdial tcp[46]?\b/i;
const TIMEOUT = /\b(i\/o timeout|timed out|deadline exceeded)\b/i;

/** Reports whether an error text is a dial timeout, the sign of blocked egress. */
export function isDialTimeout(error: string | null | undefined): boolean {
	if (!error) return false;
	return DIAL.test(error) && TIMEOUT.test(error);
}
