// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

/**
 * Returns the absolute URL to a documentation page or section on the official docs site.
 * Follows MkDocs directory URLs: `page.md#anchor` -> `<base>page/#anchor`.
 */
export function docsUrl(path = ''): string {
	const rawBase = import.meta.env.PUBLIC_DOCS_URL || 'https://lusoris.github.io/SnatchArr/';
	const base = rawBase.endsWith('/') ? rawBase : `${rawBase}/`;

	if (!path) {
		return base;
	}

	const clean = path.startsWith('/') ? path.slice(1) : path;
	const hashIndex = clean.indexOf('#');
	const filePath = hashIndex === -1 ? clean : clean.slice(0, hashIndex);
	const anchor = hashIndex === -1 ? '' : clean.slice(hashIndex);

	if (!filePath || filePath === 'index.md') {
		return `${base}${anchor}`;
	}

	const pagePath = filePath.replace(/\.md$/, '');
	return `${base}${pagePath}/${anchor}`;
}
