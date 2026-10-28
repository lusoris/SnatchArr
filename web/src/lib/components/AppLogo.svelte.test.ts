// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import type { components } from '#lib/api/schema.js';
import logosData from '#lib/logos/logos.json';
import AppLogo from './AppLogo.svelte';

type AppKind = components['schemas']['AppKind'];
type DownloadClientKind = components['schemas']['DownloadClientKind'];
type NotificationChannelKind = Exclude<components['schemas']['NotificationChannelKind'], 'webhook'>;
type WatchawareSourceKind = components['schemas']['WatchawareSourceKind'];
type ExtraKind = 'prowlarr' | 'seerr' | 'configarr';

type IntegratedAppKind =
	AppKind | DownloadClientKind | NotificationChannelKind | WatchawareSourceKind | ExtraKind;

// Type-level check: compile-time assertion that logosData covers every IntegratedAppKind
const _manifestCoverage = logosData satisfies Record<IntegratedAppKind, unknown>;
void _manifestCoverage;

const APP_KINDS: readonly AppKind[] = [
	'sonarr',
	'radarr',
	'lidarr',
	'readarr',
	'whisparr_v2',
	'whisparr_v3',
	'stash'
];

const DOWNLOAD_CLIENT_KINDS: readonly DownloadClientKind[] = [
	'qbittorrent',
	'transmission',
	'deluge',
	'rtorrent',
	'sabnzbd',
	'nzbget'
];

const NOTIFICATION_CHANNEL_KINDS: readonly NotificationChannelKind[] = [
	'discord',
	'slack',
	'telegram',
	'ntfy',
	'gotify',
	'teams',
	'apprise'
];

const WATCHAWARE_SOURCE_KINDS: readonly WatchawareSourceKind[] = ['jellyfin', 'emby'];
const EXTRA_KINDS: readonly ExtraKind[] = ['prowlarr', 'seerr', 'configarr'];

const ALL_INTEGRATED_KINDS: readonly IntegratedAppKind[] = [
	...APP_KINDS,
	...DOWNLOAD_CLIENT_KINDS,
	...NOTIFICATION_CHANNEL_KINDS,
	...WATCHAWARE_SOURCE_KINDS,
	...EXTRA_KINDS
];

const EXCEPTION_KINDS = ['teams', 'slack', 'telegram', 'discord', 'rtorrent', 'emby'] as const;

describe('AppLogo', () => {
	it('positive: manifest covers every kind in the TS enum types', () => {
		expect(ALL_INTEGRATED_KINDS).toHaveLength(25);
		for (const kind of ALL_INTEGRATED_KINDS) {
			expect(logosData).toHaveProperty(kind);
		}
	});

	it('positive: logo kind renders img with src, alt="", width, height', async () => {
		const { container } = render(AppLogo, {
			props: { kind: 'sonarr', size: 24 }
		});

		const img = container.querySelector('img');
		expect(img).not.toBeNull();
		expect(img?.getAttribute('src')).toContain('logos/sonarr.svg');
		expect(img?.getAttribute('alt')).toBe('');
		expect(img?.getAttribute('width')).toBe('24');
		expect(img?.getAttribute('height')).toBe('24');
		expect(img?.getAttribute('loading')).toBe('lazy');
		expect(img?.getAttribute('decoding')).toBe('async');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it.each(EXCEPTION_KINDS)('positive: exception %s renders svg icon not img', (kind) => {
		const { container } = render(AppLogo, {
			props: { kind, size: 20 }
		});

		expect(container.querySelector('img')).toBeNull();
		const svg = container.querySelector('svg');
		expect(svg).not.toBeNull();
		expect(svg?.getAttribute('aria-hidden')).toBe('true');
	});

	it('boundary: unknown kind renders nothing', () => {
		const { container } = render(AppLogo, {
			props: { kind: 'unknown-service-abc' }
		});

		expect(container.querySelector('img')).toBeNull();
		expect(container.querySelector('svg')).toBeNull();
		expect(container.textContent?.trim()).toBe('');
	});
});
