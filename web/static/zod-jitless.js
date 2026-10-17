// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Runs before the app's modules: Zod probes `new Function` when the first object schema is
// built at load time, and the Content-Security-Policy (no 'unsafe-eval') reports that probe
// as a violation on every page. jitless skips the probe; Zod reads this global config.
globalThis.__zod_globalConfig = Object.assign(globalThis.__zod_globalConfig ?? {}, {
	jitless: true
});
