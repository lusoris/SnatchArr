// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2
import { screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import InstanceLink from './InstanceLink.svelte';

describe('InstanceLink', () => {
	afterEach(() => vi.restoreAllMocks());

	it('labels the link with the instance name', async () => {
		stubGet({ '/instances': [forkInstance] });
		renderWithQuery(InstanceLink, { id: forkInstance.id });
		const link = await screen.findByRole('link', { name: forkInstance.name });
		expect(link.getAttribute('href')).toBe(`/instances/${forkInstance.id}`);
	});

	it('falls back to the id when the list fails to load', async () => {
		stubGet({
			'/instances': new ProblemError({ type: 'about:blank', status: 503, detail: 'down' })
		});
		renderWithQuery(InstanceLink, { id: forkInstance.id });
		expect(screen.getByRole('link', { name: forkInstance.id })).toBeTruthy();
	});

	it('keeps the id for an instance the list does not hold', async () => {
		const get = stubGet({ '/instances': [forkInstance] });
		renderWithQuery(InstanceLink, { id: 'gone' });
		await vi.waitFor(() => expect(get).toHaveBeenCalled());
		expect(screen.getByRole('link', { name: 'gone' }).getAttribute('href')).toBe('/instances/gone');
	});
});
