// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import type { LogRecord } from '#lib/logs.js';
import { m } from '#lib/paraglide/messages.js';
import LogList from './LogList.svelte';

describe('LogList', () => {
	const sampleRecords: LogRecord[] = [
		{
			seq: 1,
			time: '2026-10-25T10:00:00Z',
			level: 'info',
			source: 'api',
			message: 'Server listening',
			attrs: [{ key: 'port', value: '8080' }]
		},
		{
			seq: 2,
			time: '2026-10-25T10:01:00Z',
			level: 'warn',
			source: 'worker',
			message: 'Worker slowed down',
			attrs: []
		}
	];

	it('positive: renders log records and toggles JSON detail on click', async () => {
		render(LogList, {
			props: { records: sampleRecords }
		});

		expect(screen.getByText('Server listening')).toBeTruthy();
		// The value renders through LinkText, so key and value are separate text nodes.
		expect(screen.getByText((_, el) => el?.textContent === 'port=8080')).toBeTruthy();
		expect(screen.getByText('Worker slowed down')).toBeTruthy();

		const rowBtn = screen.getByRole('button', { name: /Server listening/ });
		expect(rowBtn.getAttribute('aria-expanded')).toBe('false');
		expect(screen.queryByText(/"value": "8080"/)).toBeNull();

		await fireEvent.click(rowBtn);
		expect(rowBtn.getAttribute('aria-expanded')).toBe('true');
		expect(screen.getByText(/"value": "8080"/)).toBeTruthy();
	});

	it('negative: empty record list displays empty state message', () => {
		render(LogList, {
			props: { records: [] }
		});

		expect(screen.getByText(m.logs_empty())).toBeTruthy();
		expect(screen.queryByRole('button')).toBeNull();
	});

	it('boundary: log record with empty message and no attrs renders without error', () => {
		const minimalRecord: LogRecord = {
			seq: 99,
			time: '2026-10-25T10:00:00Z',
			level: 'debug',
			source: 'test',
			message: '',
			attrs: []
		};

		render(LogList, {
			props: { records: [minimalRecord] }
		});

		expect(screen.getByText('test')).toBeTruthy();
	});
});
