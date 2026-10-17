// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Colour scheme before first paint: the class "dark" on <html> follows prefers-color-scheme
// (sveltesentio htmlClassFor: dark -> "dark", light -> none, matching the .dark rules of
// src/lib/theme.css). A blocking external script, so a strict script-src 'self' policy holds
// and the page never paints light first. The SPA has no server runtime to stamp the class
// (ADR-0005), so the sveltesentio cookie hook does not apply.
(function () {
	var query = window.matchMedia('(prefers-color-scheme: dark)');
	function apply() {
		document.documentElement.classList.toggle('dark', query.matches);
	}
	apply();
	query.addEventListener('change', apply);
})();
