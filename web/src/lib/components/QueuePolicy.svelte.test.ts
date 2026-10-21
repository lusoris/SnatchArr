// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import {
	forkInstance,
	renderWithQuery,
	sampleQueuePolicy,
	stubGet,
	type QueuePolicy as QueuePolicyType
} from '#lib/testing/harness.js';
import QueuePolicy from './QueuePolicy.svelte';

describe('QueuePolicy', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders default queue policy settings', async () => {
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		expect(
			await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() })
		).toBeTruthy();
		const graceInput = screen.getByLabelText(m.queue_policy_grace()) as HTMLInputElement;
		expect(graceInput.value).toBe('60');
		const minBpsInput = screen.getByLabelText(m.queue_policy_min_bps()) as HTMLInputElement;
		expect(minBpsInput.value).toBe('1024');
		const maxRemovalsInput = screen.getByLabelText(
			m.queue_policy_max_removals()
		) as HTMLInputElement;
		expect(maxRemovalsInput.value).toBe('5');
		const maxImportsInput = screen.getByLabelText(m.queue_policy_max_imports()) as HTMLInputElement;
		expect(maxImportsInput.value).toBe('10');
	});

	it('sends the id-matched rule and the import bound, without a blocklist switch', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		expect(
			screen.queryByLabelText(`${m.queue_rule_id_matched()} ${m.queue_rule_blocklist()}`)
		).toBeNull();
		const mode = screen.getByLabelText(
			`${m.queue_rule_id_matched()} ${m.queue_rule_mode()}`
		) as HTMLSelectElement;
		await fireEvent.change(mode, { target: { value: 'act' } });
		await fireEvent.input(screen.getByLabelText(m.queue_policy_max_imports()), {
			target: { value: '100' }
		});

		await fireEvent.click(screen.getByRole('button', { name: m.queue_policy_save() }));

		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		expect(body.rules.id_matched).toEqual({ mode: 'act', threshold: 1, blocklist: false });
		expect(body.max_imports_per_sweep).toBe(100);
	});

	it('saves the edited payload converting grace minutes to seconds', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		const graceInput = screen.getByLabelText(m.queue_policy_grace()) as HTMLInputElement;
		await fireEvent.input(graceInput, { target: { value: '120' } });

		const saveButton = screen.getByRole('button', { name: m.queue_policy_save() });
		await fireEvent.click(saveButton);

		expect(put).toHaveBeenCalledOnce();
		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		expect(body.grace_seconds).toBe(7200);
	});

	it('sends the per-rule blocklist switch with the policy', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		const failed = screen.getByLabelText(
			`${m.queue_rule_failed()} ${m.queue_rule_blocklist()}`
		) as HTMLInputElement;
		const unmonitored = screen.getByLabelText(
			`${m.queue_rule_unwanted_unmonitored()} ${m.queue_rule_blocklist()}`
		) as HTMLInputElement;
		expect(failed.checked).toBe(true);
		expect(unmonitored.checked).toBe(false);
		await fireEvent.click(failed);
		await fireEvent.click(unmonitored);

		await fireEvent.click(screen.getByRole('button', { name: m.queue_policy_save() }));

		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		expect(body.rules.failed.blocklist).toBe(false);
		expect(body.rules.unwanted_unmonitored.blocklist).toBe(true);
		expect(body.rules.stalled.blocklist).toBe(true);
	});

	it('sends the not-an-upgrade rule with the policy', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		const threshold = screen.getByLabelText(
			`${m.queue_rule_not_an_upgrade()} ${m.queue_rule_threshold()}`
		) as HTMLInputElement;
		const blocklist = screen.getByLabelText(
			`${m.queue_rule_not_an_upgrade()} ${m.queue_rule_blocklist()}`
		) as HTMLInputElement;
		expect(threshold.value).toBe('1');
		expect(blocklist.checked).toBe(false);
		await fireEvent.input(threshold, { target: { value: '4' } });

		await fireEvent.click(screen.getByRole('button', { name: m.queue_policy_save() }));

		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		expect(body.rules.not_an_upgrade).toEqual({ mode: 'observe', threshold: 4, blocklist: false });
	});

	it('sends the duplicate rule with the policy', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		const blocklist = screen.getByLabelText(
			`${m.queue_rule_duplicate()} ${m.queue_rule_blocklist()}`
		) as HTMLInputElement;
		expect(blocklist.checked).toBe(false);
		await fireEvent.click(blocklist);

		await fireEvent.click(screen.getByRole('button', { name: m.queue_policy_save() }));

		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		expect(body.rules.duplicate).toEqual({ mode: 'observe', threshold: 1, blocklist: true });
	});

	it('sends the metadata-stuck rule with the policy', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		const mode = screen.getByLabelText(
			`${m.queue_rule_metadata_stuck()} ${m.queue_rule_mode()}`
		) as HTMLSelectElement;
		const threshold = screen.getByLabelText(
			`${m.queue_rule_metadata_stuck()} ${m.queue_rule_threshold()}`
		) as HTMLInputElement;
		const blocklist = screen.getByLabelText(
			`${m.queue_rule_metadata_stuck()} ${m.queue_rule_blocklist()}`
		) as HTMLInputElement;
		expect(threshold.value).toBe('180');
		expect(blocklist.checked).toBe(true);

		await fireEvent.change(mode, { target: { value: 'act' } });
		await fireEvent.input(threshold, { target: { value: '120' } });

		await fireEvent.click(screen.getByRole('button', { name: m.queue_policy_save() }));

		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		expect(body.rules.metadata_stuck).toEqual({ mode: 'act', threshold: 120, blocklist: true });
	});

	it('falls back to the default for optional rules the policy lacks', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		const olderRules = { ...sampleQueuePolicy.rules };
		delete olderRules.metadata_stuck;
		delete olderRules.not_an_upgrade;
		delete olderRules.duplicate;
		delete olderRules.id_matched;
		const older = { ...sampleQueuePolicy, rules: olderRules };
		delete older.max_imports_per_sweep;
		stubGet({
			'/instances/{instanceId}/queue-policy': older
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		await fireEvent.click(screen.getByRole('button', { name: m.queue_policy_save() }));

		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		const fallback = { mode: 'observe', threshold: 1, blocklist: false };
		expect(body.rules.metadata_stuck).toEqual({ mode: 'observe', threshold: 180, blocklist: true });
		expect(body.rules.not_an_upgrade).toEqual(fallback);
		expect(body.rules.duplicate).toEqual(fallback);
		expect(body.rules.id_matched).toEqual(fallback);
		expect(body.max_imports_per_sweep).toBe(25);
	});

	it('shows a problem error when saving fails with 422', async () => {
		vi.spyOn(api, 'PUT').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 422,
				detail: 'grace_seconds must be between 0 and 86400'
			})
		);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		const saveButton = screen.getByRole('button', { name: m.queue_policy_save() });
		await fireEvent.click(saveButton);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('grace_seconds must be between 0 and 86400');
	});

	it('edits and sends the match percentage floor fields', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleQueuePolicy } as never);
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy
		});
		renderWithQuery(QueuePolicy, { instanceId: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		const matchFixable = screen.getByLabelText(m.queue_policy_match_fixable()) as HTMLInputElement;
		expect(matchFixable.value).toBe('75');
		await fireEvent.input(matchFixable, { target: { value: '80' } });

		await fireEvent.click(screen.getByRole('button', { name: m.queue_policy_save() }));

		const body = (put.mock.calls[0]![1] as { body: QueuePolicyType }).body;
		expect(body.match_fixable_min_percent).toBe(80);
	});
});
