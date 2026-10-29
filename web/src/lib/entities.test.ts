// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import EntityLink from '#lib/components/EntityLink.svelte';
import RelatedLinks from '#lib/components/RelatedLinks.svelte';
import {
	INSTANCE_TABS,
	createEntityGraph,
	entities,
	entityHref,
	entityHrefOrNull,
	pageHref,
	related,
	type PageRoute
} from './entities.js';

describe('entities graph: positive', () => {
	it('resolves every static page route and preserves search strings', () => {
		const routes: readonly PageRoute[] = [
			'/',
			'/activity',
			'/instances',
			'/automation',
			'/connections',
			'/connections/download-clients',
			'/connections/seerr',
			'/connections/prowlarr',
			'/connections/configarr',
			'/adult/sources',
			'/adult/image-sets',
			'/adult/performers',
			'/adult/studios',
			'/settings/requests',
			'/system',
			'/account',
			'/login',
			'/setup'
		];
		for (const route of routes) {
			expect(pageHref(route)).toBe(route);
		}
		expect(pageHref('/automation', '?tab=caps')).toBe('/automation?tab=caps');
		expect(pageHref('/system', 'tab=about')).toBe('/system?tab=about');
	});

	it('resolves instance detail href and every declared tab', () => {
		expect(entityHref('instance', 'inst-1')).toBe('/instances/inst-1');
		for (const tab of INSTANCE_TABS) {
			expect(entityHref('instance', 'inst-1', tab)).toBe(`/instances/inst-1/${tab}`);
		}
		expect(entityHref('instance', 'inst-1', 'activity', '?source=log')).toBe(
			'/instances/inst-1/activity?source=log'
		);
	});

	it('resolves related instance links for every instance-linked row type', () => {
		const rowTypes = ['event', 'run', 'schedule', 'download_client', 'seerr_request'] as const;
		for (const rowType of rowTypes) {
			expect(related(rowType, { id: 'r1', instance_id: 'inst-9' })).toEqual([
				{ type: 'instance', id: 'inst-9', href: '/instances/inst-9' }
			]);
		}
	});
});

describe('entities graph: negative and boundary', () => {
	it('returns no href for unknown tabs or unroutable entity types', () => {
		expect(entityHref('instance', 'inst-1', 'unknown' as never)).toBeUndefined();
		expect(entityHref('run', 'run-1')).toBeUndefined();
		expect(entityHref('event', 'ev-1')).toBeUndefined();
		expect(entityHrefOrNull('instance', 'inst-1', 'unknown')).toBeNull();
		expect(entityHrefOrNull('run', 'run-1')).toBeNull();
		expect(entityHrefOrNull('instance', '')).toBeNull();
		expect(entityHrefOrNull('instance', 'inst-1')).toBe('/instances/inst-1');
	});

	it('returns no related links when key is missing, null, or empty', () => {
		expect(related('download_client', {})).toEqual([]);
		expect(related('schedule', { id: 's1', instance_id: null })).toEqual([]);
		expect(related('seerr_request', { id: 'r1', instance_id: undefined })).toEqual([]);
		expect(related('run', { id: 'run-1', instance_id: '' })).toEqual([]);
		expect(related('event', null)).toEqual([]);
		expect(related('event', undefined)).toEqual([]);
		expect(entityHref('instance', '')).toBeUndefined();
	});
});

describe('EntityLink and RelatedLinks components', () => {
	it('renders RelatedLinks for a fixture graph with a new widget type without component edits', () => {
		const fixtureGraph = createEntityGraph(
			{
				instance: { route: '/instances/[id]', tabs: ['policy'] as const },
				widget: {}
			},
			[{ from: 'widget', to: 'instance', key: 'instance_id' }]
		);

		const { body } = render(RelatedLinks, {
			props: {
				graph: fixtureGraph,
				type: 'widget',
				row: { id: 'w-1', instance_id: 'inst-42' }
			}
		});
		expect(body).toContain('href="/instances/inst-42"');
		expect(body).toContain('>inst-42</a>');
	});

	it('renders nothing when RelatedLinks row has a null instance_id or EntityLink type has no route', () => {
		const emptyRelated = render(RelatedLinks, {
			props: {
				type: 'download_client',
				row: { id: 'dl-1', instance_id: null }
			}
		});
		expect(emptyRelated.body).not.toContain('<a');

		const unroutable = render(EntityLink, {
			props: {
				type: 'run',
				id: 'run-1',
				graph: entities
			}
		});
		expect(unroutable.body).not.toContain('<a');
	});
});
