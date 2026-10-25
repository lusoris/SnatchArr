// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import UrlText from './UrlText.svelte';

describe('UrlText', () => {
	it('positive: renders URL with soft-wrap opportunities and root data-url attribute', () => {
		const url = 'http://sonarr-anime.media.svc.cluster.local:8989';
		const { container } = render(UrlText, { props: { url, class: 'test-class' } });

		const span = container.querySelector('[data-url]');
		expect(span).toBeTruthy();
		expect(span?.classList.contains('[overflow-wrap:break-word]')).toBe(true);
		expect(span?.classList.contains('test-class')).toBe(true);

		// Verifies accessible plain text and copy/paste yields exact URL without extra characters
		expect(span?.textContent).toBe(url);
		expect(span?.getAttribute('title')).toBe(url);

		const wbrs = container.querySelectorAll('wbr');
		// 7 segments means 6 <wbr> elements
		expect(wbrs.length).toBe(6);
	});

	it('positive: custom title overrides default URL title attribute', () => {
		const url = 'http://127.0.0.1:9091';
		const { container } = render(UrlText, { props: { url, title: 'Custom Tooltip' } });

		const span = container.querySelector('[data-url]');
		expect(span?.getAttribute('title')).toBe('Custom Tooltip');
	});

	it('negative: handles empty URL without throwing', () => {
		const { container } = render(UrlText, { props: { url: '' } });
		const span = container.querySelector('[data-url]');
		expect(span).toBeTruthy();
		expect(span?.textContent).toBe('');
		expect(container.querySelectorAll('wbr').length).toBe(0);
	});
});
