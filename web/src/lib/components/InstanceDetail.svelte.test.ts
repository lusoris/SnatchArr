// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
	failedInstance,
	forkInstance,
	renderWithQuery,
	samplePolicy,
	stubGet,
	untestedInstance,
	type Instance
} from '#lib/testing/harness.js';
import InstanceDetail from './InstanceDetail.svelte';

/** Renders the view for one instance and returns the line under its heading. */
async function subtitleOf(instance: Instance): Promise<string | undefined> {
	stubGet({ '/instances/{instanceId}': instance, '/instances/{instanceId}/policy': samplePolicy });
	renderWithQuery(InstanceDetail, { id: instance.id });
	const heading = await screen.findByRole('heading', { level: 1, name: instance.name });
	return heading.nextElementSibling?.textContent?.trim();
}

describe('InstanceDetail', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows name, kind with the fork and address of the instance', async () => {
		expect(await subtitleOf(forkInstance)).toBe('readarr · Bookshelf · http://readarr.local:8787');
	});

	// The view does not show the state of the last connection test yet (#186), so these two cases
	// are held to what it shows today.
	it('shows an instance whose connection test failed', async () => {
		expect(await subtitleOf(failedInstance)).toBe('sonarr · http://sonarr.local:8989');
	});

	it('shows an instance no connection test has reached', async () => {
		expect(await subtitleOf(untestedInstance)).toBe('radarr · http://radarr.local:7878');
	});

	it('announces the problem document when the instance does not exist', async () => {
		stubGet({
			'/instances/{instanceId}': new ProblemError({
				type: 'about:blank',
				status: 404,
				detail: 'Instance not found'
			}),
			'/instances/{instanceId}/policy': samplePolicy
		});
		renderWithQuery(InstanceDetail, { id: 'missing-id' });

		expect((await screen.findByRole('alert')).textContent).toContain('Instance not found');
		expect(screen.queryByRole('heading', { level: 1 })).toBeNull();
	});

	it('announces the problem document when the policy is refused', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/policy': new ProblemError({
				type: 'about:blank',
				status: 503,
				detail: 'store down'
			})
		});
		renderWithQuery(InstanceDetail, { id: forkInstance.id });

		expect((await screen.findByRole('alert')).textContent).toContain('store down');
		expect(screen.queryByRole('heading', { level: 1 })).toBeNull();
	});
});
