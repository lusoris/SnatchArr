// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

const DELIMITERS = new Set(['.', ':', '/', '?', '&', '#']);

function skipIpv6(rest: string): { host: string; nextIndex: number } | null {
	if (!rest.startsWith('[')) return null;
	const closeBracket = rest.indexOf(']');
	if (closeBracket === -1) return null;
	return { host: rest.slice(0, closeBracket + 1), nextIndex: closeBracket + 1 };
}

/**
 * Splits a URL into text segments for soft-wrapping with <wbr>.
 * Soft-wrap opportunities occur after "://" and before each '.', ':', '/', '?', '&', '#'.
 * IPv6 bracketed hosts [::1] are preserved as single units.
 */
export function splitUrl(url: string): string[] {
	if (!url) return [''];

	const segments: string[] = [];
	let rest = url;
	const schemeMatch = url.match(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//);
	if (schemeMatch) {
		segments.push(schemeMatch[0]);
		rest = url.slice(schemeMatch[0].length);
		if (!rest) return segments;
	}

	let current = '';
	let i = 0;
	const ipv6 = skipIpv6(rest);
	if (ipv6) {
		current = ipv6.host;
		i = ipv6.nextIndex;
	}

	while (i < rest.length) {
		const ch = rest[i];
		if (DELIMITERS.has(ch) && (current.length > 0 || segments.length > 0)) {
			if (current.length > 0) segments.push(current);
			current = ch;
		} else {
			current += ch;
		}
		i++;
	}

	if (current.length > 0) segments.push(current);
	return segments.length > 0 ? segments : [''];
}

export interface TextPart {
	text: string;
	url: boolean;
}

const URL_IN_TEXT = /https?:\/\/[^\s"'<>]+/g;

/**
 * Splits free text (log messages, event details) into plain and URL parts so a
 * URL renders through UrlText and wraps only at its delimiters.
 */
export function splitTextUrls(text: string): TextPart[] {
	const parts: TextPart[] = [];
	let last = 0;
	for (const match of text.matchAll(URL_IN_TEXT)) {
		const start = match.index;
		if (start > last) parts.push({ text: text.slice(last, start), url: false });
		parts.push({ text: match[0], url: true });
		last = start + match[0].length;
	}
	if (last < text.length) parts.push({ text: text.slice(last), url: false });
	return parts;
}
