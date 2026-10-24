// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import LinkText from './LinkText.svelte';

describe('LinkText', () => {
	it('positive: renders an embedded URL through UrlText and keeps the text exact', () => {
		const text = 'post "http://127.0.0.1:9091/transmission/rpc": connection refused';
		const { container } = render(LinkText, { props: { text, class: 'x' } });

		const url = container.querySelector('[data-url]');
		expect(url?.textContent).toBe('http://127.0.0.1:9091/transmission/rpc');
		expect(container.querySelectorAll('wbr').length).toBeGreaterThan(0);
		expect(container.querySelector('.x')?.textContent).toBe(text);
	});

	it('negative: text without a URL renders no data-url element', () => {
		const { container } = render(LinkText, { props: { text: 'search dispatched' } });
		expect(container.querySelector('[data-url]')).toBeNull();
		expect(container.textContent).toBe('search dispatched');
	});

	it('boundary: empty text and a URL as the whole text', () => {
		const empty = render(LinkText, { props: { text: '' } });
		expect(empty.container.textContent).toBe('');

		const only = render(LinkText, { props: { text: 'https://a.example/b' } });
		expect(only.container.querySelectorAll('[data-url]').length).toBe(1);
		expect(only.container.textContent).toBe('https://a.example/b');
	});
});
