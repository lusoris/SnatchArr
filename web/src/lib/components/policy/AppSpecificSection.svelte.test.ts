// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { samplePolicy, type SnatchPolicy } from '#lib/testing/harness.js';
import AppSpecificSection from './AppSpecificSection.svelte';

describe('AppSpecificSection', () => {
	it('renders radarr_release_type when kind is radarr', async () => {
		const policy: SnatchPolicy = { ...samplePolicy, radarr_release_type: 'physical' };
		render(AppSpecificSection, { policy, kind: 'radarr' });

		expect(screen.getByText(m.policy_group_app_specific())).toBeTruthy();
		const select = screen.getByLabelText(m.policy_radarr_release_type()) as HTMLSelectElement;
		expect(select.value).toBe('physical');

		await fireEvent.change(select, { target: { value: 'digital' } });
		expect(policy.radarr_release_type).toBe('digital');
	});

	it('renders sonarr_missing_mode and sonarr_upgrade_mode when kind is sonarr', async () => {
		const policy: SnatchPolicy = {
			...samplePolicy,
			sonarr_missing_mode: 'episodes',
			sonarr_upgrade_mode: 'episodes'
		};
		render(AppSpecificSection, { policy, kind: 'sonarr' });

		expect(screen.getByText(m.policy_group_app_specific())).toBeTruthy();
		const missingSelect = screen.getByLabelText(
			m.policy_sonarr_missing_mode()
		) as HTMLSelectElement;
		const upgradeSelect = screen.getByLabelText(
			m.policy_sonarr_upgrade_mode()
		) as HTMLSelectElement;

		await fireEvent.change(missingSelect, { target: { value: 'season_packs' } });
		expect(policy.sonarr_missing_mode).toBe('season_packs');

		await fireEvent.change(upgradeSelect, { target: { value: 'season_packs' } });
		expect(policy.sonarr_upgrade_mode).toBe('season_packs');
	});

	it('renders lidarr_missing_mode when kind is lidarr', async () => {
		const policy: SnatchPolicy = { ...samplePolicy, lidarr_missing_mode: 'artist' };
		render(AppSpecificSection, { policy, kind: 'lidarr' });

		expect(screen.getByText(m.policy_group_app_specific())).toBeTruthy();
		const select = screen.getByLabelText(m.policy_lidarr_missing_mode()) as HTMLSelectElement;

		await fireEvent.change(select, { target: { value: 'album' } });
		expect(policy.lidarr_missing_mode).toBe('album');
	});

	it('renders nothing when kind is not supported (readarr, whisparr, or undefined)', () => {
		const policy: SnatchPolicy = { ...samplePolicy };
		const { unmount } = render(AppSpecificSection, { policy, kind: 'readarr' });
		expect(screen.queryByText(m.policy_group_app_specific())).toBeNull();
		unmount();

		render(AppSpecificSection, { policy, kind: 'whisparr_v2' });
		expect(screen.queryByText(m.policy_group_app_specific())).toBeNull();
	});
});
