// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import UrlText from './UrlText.svelte';

describe('UrlText', () => {
	it('positive: renders URL with soft-wrap opportunities and root data-url attribute', () => {
		const url = 'http://radarr-4k.media.svc.cluster.local:7878';
		const { container } = render(UrlText, { props: { url, class: 'test-class' } });

		const span = container.querySelector('[data-url]');
		expect(span).toBeTruthy();
		expect(span?.classList.contains('[overflow-wrap:break-word]')).toBe(true);
		expect(span?.classList.contains('test-class')).toBe(true);

		// Verifies accessible plain text and copy/paste yields exact URL without extra characters
		expect(span?.textContent).toBe(url);

		const wbrs = container.querySelectorAll('wbr');
		// 7 segments means 6 <wbr> elements
		expect(wbrs.length).toBe(6);

		// radarr-4k segment stays one nowrap span
		const nowrapSpans = container.querySelectorAll('span.whitespace-nowrap');
		expect(nowrapSpans.length).toBe(7);
		const radarrSpan = Array.from(nowrapSpans).find((s) => s.textContent === 'radarr-4k');
		expect(radarrSpan).toBeTruthy();
		expect(radarrSpan?.classList.contains('whitespace-nowrap')).toBe(true);
	});

	it('negative: renders plain host without hyphenated segments', () => {
		const url = 'http://sonarr.media.svc:8989';
		const { container } = render(UrlText, { props: { url } });

		const span = container.querySelector('[data-url]');
		expect(span).toBeTruthy();
		expect(span?.textContent).toBe(url);

		const nowrapSpans = container.querySelectorAll('span.whitespace-nowrap');
		expect(nowrapSpans.length).toBe(5);
		expect(Array.from(nowrapSpans).map((s) => s.textContent)).toEqual([
			'http://',
			'sonarr',
			'.media',
			'.svc',
			':8989'
		]);
	});

	it('boundary: handles empty url and ipv6 [::1]', () => {
		// Empty url boundary
		const empty = render(UrlText, { props: { url: '' } });
		const emptySpan = empty.container.querySelector('[data-url]');
		expect(emptySpan).toBeTruthy();
		expect(emptySpan?.textContent).toBe('');
		expect(empty.container.querySelectorAll('wbr').length).toBe(0);

		// IPv6 [::1] boundary
		const ipv6Url = 'http://[::1]:8080';
		const ipv6 = render(UrlText, { props: { url: ipv6Url } });
		const ipv6Span = ipv6.container.querySelector('[data-url]');
		expect(ipv6Span).toBeTruthy();
		expect(ipv6Span?.textContent).toBe(ipv6Url);
		const ipv6Nowraps = ipv6.container.querySelectorAll('span.whitespace-nowrap');
		expect(ipv6Nowraps.length).toBe(3);
		expect(ipv6Nowraps[1]?.textContent).toBe('[::1]');
		expect(ipv6Nowraps[1]?.classList.contains('whitespace-nowrap')).toBe(true);
	});
});
