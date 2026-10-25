// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import EmptyFleetOnboarding from './EmptyFleetOnboarding.svelte';

describe('EmptyFleetOnboarding component', () => {
	it('positive: renders 3 onboarding steps with titles and actions', () => {
		render(EmptyFleetOnboarding);

		expect(
			screen.getByRole('heading', { level: 2, name: m.overview_onboarding_title() })
		).toBeTruthy();
		expect(screen.getByText(m.overview_onboarding_subtitle())).toBeTruthy();

		// Step 1: Add instance
		expect(
			screen.getByRole('heading', { level: 3, name: m.overview_onboarding_step1_title() })
		).toBeTruthy();
		const step1Link = screen.getByRole('link', { name: m.overview_onboarding_step1_action() });
		expect(step1Link.getAttribute('href')).toBe('/instances');

		// Step 2: Link Prowlarr
		expect(
			screen.getByRole('heading', { level: 3, name: m.overview_onboarding_step2_title() })
		).toBeTruthy();
		const step2Link = screen.getByRole('link', { name: m.overview_onboarding_step2_action() });
		expect(step2Link.getAttribute('href')).toBe('/connections/prowlarr');

		// Step 3: Open docs
		expect(
			screen.getByRole('heading', { level: 3, name: m.overview_onboarding_step3_title() })
		).toBeTruthy();
		const step3Link = screen.getByRole('link', { name: m.overview_onboarding_step3_action() });
		expect(step3Link.getAttribute('href')).toBe('https://lusoris.github.io/SnatchArr/');
		expect(step3Link.getAttribute('target')).toBe('_blank');
		expect(step3Link.getAttribute('rel')).toContain('noopener');
	});
});
