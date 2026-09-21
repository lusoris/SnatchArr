// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Single-page app: the Go API serves index.html for every path and owns sessions, so
// nothing renders on a server (ADR-0005).
export const ssr = false;
export const prerender = false;
export const trailingSlash = 'never';
