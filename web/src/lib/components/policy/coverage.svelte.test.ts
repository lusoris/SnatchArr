// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import fs from 'node:fs';
import path from 'node:path';
import { screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import CapsPresetsForm from '#lib/components/CapsPresetsForm.svelte';
import InstancePolicyTab from '#lib/components/instance/InstancePolicyTab.svelte';
import QueuePolicy from '#lib/components/QueuePolicy.svelte';
import { m } from '#lib/paraglide/messages.js';
import {
	forkInstance,
	renderWithQuery,
	sampleFilterOptions,
	samplePolicy,
	sampleQueuePolicy,
	stubGet
} from '#lib/testing/harness.js';
import GeneralTab from '../../../routes/system/GeneralTab.svelte';

const openapiPath = path.resolve(import.meta.dirname, '../../../../../api/openapi/openapi.yaml');

/**
 * Line reader for schema properties from OpenAPI YAML.
 * Extracts properties for schemaName, omitting properties with readOnly: true and updated_at.
 */
export function getWritableProperties(yaml: string, schemaName: string): string[] {
	const lines = yaml.split('\n');
	let inSchema = false;
	let inProps = false;
	let currentProp: string | null = null;
	let isReadOnly = false;
	const props: string[] = [];

	for (const line of lines) {
		if (line === `    ${schemaName}:` || line.startsWith(`    ${schemaName}:`)) {
			inSchema = true;
			continue;
		}
		if (inSchema) {
			if (/^ {4}[A-Z]/.test(line)) {
				if (currentProp && !isReadOnly && currentProp !== 'updated_at') {
					props.push(currentProp);
					currentProp = null;
				}
				break;
			}
			if (line === '      properties:') {
				inProps = true;
				continue;
			}
			if (inProps) {
				const propMatch = line.match(/^ {8}([a-zA-Z0-9_]+):/);
				if (propMatch) {
					if (currentProp && !isReadOnly && currentProp !== 'updated_at') {
						props.push(currentProp);
					}
					currentProp = propMatch[1] ?? null;
					isReadOnly = false;
				} else if (line.includes('readOnly: true')) {
					isReadOnly = true;
				}
			}
		}
	}
	if (currentProp && !isReadOnly && currentProp !== 'updated_at') {
		props.push(currentProp);
	}
	return props;
}

/**
 * Checks that all expected properties exist as [data-field="<property>"] inside the container(s).
 * Returns array of missing properties.
 */
export function checkFieldCoverage(
	containers: HTMLElement | HTMLElement[],
	expectedProps: string[]
): string[] {
	const list = Array.isArray(containers) ? containers : [containers];
	const found = new Set<string>();
	for (const c of list) {
		for (const el of c.querySelectorAll('[data-field]')) {
			const field = el.getAttribute('data-field');
			if (field) found.add(field);
		}
	}
	return expectedProps.filter((p) => !found.has(p));
}

describe('Field coverage against openapi.yaml', () => {
	const yaml = fs.readFileSync(openapiPath, 'utf8');

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('covers every writable SnatchPolicy property across app kinds', async () => {
		const expectedProps = getWritableProperties(yaml, 'SnatchPolicy');
		expect(expectedProps.length).toBeGreaterThan(15);
		expect(expectedProps).not.toContain('updated_at');

		const kinds = ['radarr', 'sonarr', 'lidarr'] as const;
		const containers: HTMLElement[] = [];

		for (const kind of kinds) {
			const inst = { ...forkInstance, id: `inst-${kind}`, kind };
			stubGet({
				'/instances/{instanceId}': inst,
				'/instances/{instanceId}/policy': samplePolicy,
				'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
			});
			const { container } = renderWithQuery(InstancePolicyTab, { id: inst.id });
			await within(container).findByRole('heading', { level: 2 });
			containers.push(container);
		}

		const missing = checkFieldCoverage(containers, expectedProps);
		expect(missing).toEqual([]);
	});

	it('covers every writable QueuePolicy property', async () => {
		const expectedProps = getWritableProperties(yaml, 'QueuePolicy');
		expect(expectedProps.length).toBeGreaterThan(5);

		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});

		const { container } = renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });
		await screen.findByRole('heading', { level: 2 });

		const missing = checkFieldCoverage(container, expectedProps);
		expect(missing).toEqual([]);
	});

	it('covers every writable Settings property', async () => {
		const expectedProps = getWritableProperties(yaml, 'Settings');
		expect(expectedProps).toEqual([
			'history_retention_days',
			'user_agent',
			'global_hourly_cap',
			'search_priority_preset',
			'download_priority_preset',
			'llm_enabled',
			'llm_write_tools'
		]);

		const mediaFirst = {
			sonarr: 10,
			radarr: 10,
			lidarr: 20,
			readarr: 30,
			whisparr_v2: 40,
			whisparr_v3: 40
		};

		stubGet({
			'/settings': {
				history_retention_days: 30,
				user_agent: 'SnatchArr/1.0',
				global_hourly_cap: 100,
				search_priority_preset: mediaFirst,
				download_priority_preset: mediaFirst,
				llm_enabled: false,
				llm_write_tools: []
			},
			'/priority-presets': [
				{
					name: 'media_first',
					search: mediaFirst,
					download: mediaFirst
				}
			],
			'/notifications/channels': [],
			'/notifications/rules': []
		});

		const { container: generalContainer } = renderWithQuery(GeneralTab);
		await screen.findByRole('heading', { level: 2, name: m.settings_title() });

		const { container: capsContainer } = renderWithQuery(CapsPresetsForm);
		await screen.findByLabelText(m.settings_global_cap());

		const combined = document.createElement('div');
		combined.appendChild(generalContainer.cloneNode(true));
		combined.appendChild(capsContainer.cloneNode(true));

		const missing = checkFieldCoverage(combined, expectedProps);
		expect(missing).toEqual([]);
	});

	it('planted case: reports missing when a data-field is removed from rendered DOM', async () => {
		const expectedProps = getWritableProperties(yaml, 'SnatchPolicy');

		const inst = { ...forkInstance, id: 'inst-radarr', kind: 'radarr' as const };
		stubGet({
			'/instances/{instanceId}': inst,
			'/instances/{instanceId}/policy': samplePolicy,
			'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
		});
		const { container } = renderWithQuery(InstancePolicyTab, { id: inst.id });
		await screen.findByRole('heading', { level: 2 });

		// Planted defect: strip data-field from missing_per_cycle input
		const targetEl = container.querySelector('[data-field="missing_per_cycle"]');
		expect(targetEl).not.toBeNull();
		targetEl?.removeAttribute('data-field');

		// The checker must catch the defect and report missing_per_cycle missing
		const missing = checkFieldCoverage(container, expectedProps);
		expect(missing).toContain('missing_per_cycle');
	});
});
