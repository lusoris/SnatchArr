<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type QueuePolicyType = components['schemas']['QueuePolicy'];
	type QueueRules = Required<components['schemas']['QueueRules']>;
	type RuleKey = keyof QueueRules;

	// The API always returns not_an_upgrade, duplicate and id_matched; this
	// mirrors their shared default for the type.
	const optionalRuleDefault: components['schemas']['QueueRuleSettings'] = {
		mode: 'observe',
		threshold: 1,
		blocklist: false
	};
	const optionalImportFixableDefault: components['schemas']['QueueRuleSettings'] = {
		mode: 'observe',
		threshold: 1,
		blocklist: false
	};
	const optionalImportBrokenDefault: components['schemas']['QueueRuleSettings'] = {
		mode: 'observe',
		threshold: 2,
		blocklist: true
	};
	const optionalImportNotWantedDefault: components['schemas']['QueueRuleSettings'] = {
		mode: 'observe',
		threshold: 1,
		blocklist: true
	};

	const { instanceId: id }: { instanceId: string } = $props();
	const instanceId = untrack(() => id);

	const query = createSentioQuery<QueuePolicyType | undefined>({
		queryKey: keys.queuePolicy(instanceId),
		queryFn: async () =>
			(
				await api.GET('/instances/{instanceId}/queue-policy', {
					params: { path: { instanceId } }
				})
			).data
	});

	interface FormState {
		enabled: boolean;
		rules: QueueRules;
		grace_minutes: number;
		min_bytes_per_second: number;
		min_custom_format_score: number | null;
		max_removals_per_sweep: number;
		max_imports_per_sweep: number;
		match_fixable_min_percent: number;
		match_broken_below_percent: number;
		track_fixable_min_percent: number;
		track_broken_below_percent: number;
	}

	let draft = $state<FormState | null>(null);

	$effect(() => {
		if (query.data && !draft) {
			draft = {
				enabled: query.data.enabled,
				rules: {
					stalled: { ...query.data.rules.stalled },
					slow: { ...query.data.rules.slow },
					failed: { ...query.data.rules.failed },
					id_matched: { ...(query.data.rules.id_matched ?? optionalRuleDefault) },
					import_fixable: { ...(query.data.rules.import_fixable ?? optionalImportFixableDefault) },
					import_broken: { ...(query.data.rules.import_broken ?? optionalImportBrokenDefault) },
					import_not_wanted: {
						...(query.data.rules.import_not_wanted ?? optionalImportNotWantedDefault)
					},
					import_blocked: { ...query.data.rules.import_blocked },
					unwanted_score: { ...query.data.rules.unwanted_score },
					not_an_upgrade: { ...(query.data.rules.not_an_upgrade ?? optionalRuleDefault) },
					duplicate: { ...(query.data.rules.duplicate ?? optionalRuleDefault) },
					unwanted_unmonitored: { ...query.data.rules.unwanted_unmonitored }
				},
				grace_minutes: Math.round(query.data.grace_seconds / 60),
				min_bytes_per_second: query.data.min_bytes_per_second,
				min_custom_format_score: query.data.min_custom_format_score ?? null,
				max_removals_per_sweep: query.data.max_removals_per_sweep,
				max_imports_per_sweep: query.data.max_imports_per_sweep ?? 25,
				match_fixable_min_percent: query.data.match_fixable_min_percent ?? 75,
				match_broken_below_percent: query.data.match_broken_below_percent ?? 40,
				track_fixable_min_percent: query.data.track_fixable_min_percent ?? 50,
				track_broken_below_percent: query.data.track_broken_below_percent ?? 40
			};
		}
	});

	const save = createSentioMutation({
		mutationFn: async (payload: QueuePolicyType) =>
			(
				await api.PUT('/instances/{instanceId}/queue-policy', {
					params: { path: { instanceId } },
					body: payload
				})
			).data,
		invalidates: [keys.queuePolicy(instanceId)],
		onSuccess: () => toast.success(m.queue_policy_saved()),
		onError: (err) => toast.error(describeError(err))
	});

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!draft) return;
		const payload: QueuePolicyType = {
			enabled: draft.enabled,
			rules: draft.rules,
			grace_seconds: Math.round(draft.grace_minutes * 60),
			min_bytes_per_second: draft.min_bytes_per_second,
			min_custom_format_score:
				draft.min_custom_format_score === null || Number.isNaN(draft.min_custom_format_score)
					? undefined
					: draft.min_custom_format_score,
			max_removals_per_sweep: draft.max_removals_per_sweep,
			max_imports_per_sweep: draft.max_imports_per_sweep,
			match_fixable_min_percent: draft.match_fixable_min_percent,
			match_broken_below_percent: draft.match_broken_below_percent,
			track_fixable_min_percent: draft.track_fixable_min_percent,
			track_broken_below_percent: draft.track_broken_below_percent
		};
		save.mutate(payload);
	}

	const ruleKeys: { key: RuleKey; label: () => string }[] = [
		{ key: 'stalled', label: m.queue_rule_stalled },
		{ key: 'slow', label: m.queue_rule_slow },
		{ key: 'failed', label: m.queue_rule_failed },
		{ key: 'id_matched', label: m.queue_rule_id_matched },
		{ key: 'import_fixable', label: m.queue_rule_import_fixable },
		{ key: 'import_broken', label: m.queue_rule_import_broken },
		{ key: 'import_not_wanted', label: m.queue_rule_import_not_wanted },
		{ key: 'import_blocked', label: m.queue_rule_import_blocked },
		{ key: 'unwanted_score', label: m.queue_rule_unwanted_score },
		{ key: 'not_an_upgrade', label: m.queue_rule_not_an_upgrade },
		{ key: 'duplicate', label: m.queue_rule_duplicate },
		{ key: 'unwanted_unmonitored', label: m.queue_rule_unwanted_unmonitored }
	];
</script>

<section class="mt-6" aria-labelledby="queue-policy-title">
	{#if query.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if query.isError}
		<ProblemNote error={query.error} />
	{:else if draft}
		<form
			class="card grid max-w-3xl gap-4 md:grid-cols-2"
			onsubmit={handleSubmit}
			aria-labelledby="queue-policy-title"
		>
			<h2 id="queue-policy-title" class="text-lg font-semibold md:col-span-2">
				{m.queue_policy_title()}
			</h2>

			{#if save.isError}
				<div class="md:col-span-2">
					<ProblemNote error={save.error} />
				</div>
			{/if}

			<div class="md:col-span-2">
				<label class="flex items-center gap-2">
					<input type="checkbox" data-field="enabled" bind:checked={draft.enabled} />
					{m.queue_policy_enabled()}
				</label>
			</div>

			<fieldset class="border-border rounded border p-4 md:col-span-2" data-field="rules">
				<legend class="px-2 text-sm font-semibold">{m.queue_policy_rules_title()}</legend>
				<div class="overflow-x-auto">
					<table class="w-full text-left text-sm">
						<thead>
							<tr class="border-border border-b">
								<th scope="col" class="py-2 pr-4">{m.queue_strike_rule()}</th>
								<th scope="col" class="py-2 pr-4">{m.queue_rule_mode()}</th>
								<th scope="col" class="py-2 pr-4">{m.queue_rule_threshold()}</th>
								<th scope="col" class="py-2">{m.queue_rule_blocklist()}</th>
							</tr>
						</thead>
						<tbody>
							{#each ruleKeys as r (r.key)}
								<tr class="border-border border-b last:border-b-0">
									<td class="py-2 pr-4 font-medium">{r.label()}</td>
									<td class="py-2 pr-4">
										<select
											class="select"
											aria-label={`${r.label()} ${m.queue_rule_mode()}`}
											bind:value={draft.rules[r.key].mode}
										>
											<option value="observe">{m.queue_rule_mode_observe()}</option>
											<option value="act">{m.queue_rule_mode_act()}</option>
										</select>
									</td>
									<td class="py-2 pr-4">
										<input
											class="input w-24"
											type="number"
											min="1"
											max="100"
											aria-label={`${r.label()} ${m.queue_rule_threshold()}`}
											bind:value={draft.rules[r.key].threshold}
										/>
									</td>
									<td class="py-2">
										<!-- id_matched and import_fixable import and never remove, so they have nothing to blocklist. -->
										{#if r.key !== 'id_matched' && r.key !== 'import_fixable'}
											<input
												type="checkbox"
												aria-label={`${r.label()} ${m.queue_rule_blocklist()}`}
												bind:checked={draft.rules[r.key].blocklist}
											/>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</fieldset>

			<Field id="queue-grace" label={m.queue_policy_grace()} hint={m.queue_policy_grace_hint()}>
				<input
					id="queue-grace"
					data-field="grace_seconds"
					class="input"
					type="number"
					min="0"
					max="1440"
					aria-describedby="queue-grace-hint"
					bind:value={draft.grace_minutes}
				/>
			</Field>

			<Field
				id="queue-min-bps"
				label={m.queue_policy_min_bps()}
				hint={m.queue_policy_min_bps_hint()}
			>
				<input
					id="queue-min-bps"
					data-field="min_bytes_per_second"
					class="input"
					type="number"
					min="0"
					aria-describedby="queue-min-bps-hint"
					bind:value={draft.min_bytes_per_second}
				/>
			</Field>

			<Field
				id="queue-min-cfs"
				label={m.queue_policy_min_cfs()}
				hint={m.queue_policy_min_cfs_hint()}
			>
				<input
					id="queue-min-cfs"
					data-field="min_custom_format_score"
					class="input"
					type="number"
					aria-describedby="queue-min-cfs-hint"
					value={draft.min_custom_format_score ?? ''}
					oninput={(e) => {
						const val = (e.currentTarget as HTMLInputElement).value.trim();
						draft!.min_custom_format_score = val === '' ? null : parseInt(val, 10);
					}}
				/>
			</Field>

			<Field
				id="queue-max-removals"
				label={m.queue_policy_max_removals()}
				hint={m.queue_policy_max_removals_hint()}
			>
				<input
					id="queue-max-removals"
					data-field="max_removals_per_sweep"
					class="input"
					type="number"
					min="1"
					max="100"
					aria-describedby="queue-max-removals-hint"
					bind:value={draft.max_removals_per_sweep}
				/>
			</Field>

			<Field
				id="queue-max-imports"
				label={m.queue_policy_max_imports()}
				hint={m.queue_policy_max_imports_hint()}
			>
				<input
					id="queue-max-imports"
					data-field="max_imports_per_sweep"
					class="input"
					type="number"
					min="1"
					max="100"
					aria-describedby="queue-max-imports-hint"
					bind:value={draft.max_imports_per_sweep}
				/>
			</Field>

			<Field
				id="queue-match-fixable"
				label={m.queue_policy_match_fixable()}
				hint={m.queue_policy_match_fixable_hint()}
			>
				<input
					id="queue-match-fixable"
					data-field="match_fixable_min_percent"
					class="input"
					type="number"
					min="0"
					max="100"
					step="any"
					aria-describedby="queue-match-fixable-hint"
					bind:value={draft.match_fixable_min_percent}
				/>
			</Field>

			<Field
				id="queue-match-broken"
				label={m.queue_policy_match_broken()}
				hint={m.queue_policy_match_broken_hint()}
			>
				<input
					id="queue-match-broken"
					data-field="match_broken_below_percent"
					class="input"
					type="number"
					min="0"
					max="100"
					step="any"
					aria-describedby="queue-match-broken-hint"
					bind:value={draft.match_broken_below_percent}
				/>
			</Field>

			<Field
				id="queue-track-fixable"
				label={m.queue_policy_track_fixable()}
				hint={m.queue_policy_track_fixable_hint()}
			>
				<input
					id="queue-track-fixable"
					data-field="track_fixable_min_percent"
					class="input"
					type="number"
					min="0"
					max="100"
					step="any"
					aria-describedby="queue-track-fixable-hint"
					bind:value={draft.track_fixable_min_percent}
				/>
			</Field>

			<Field
				id="queue-track-broken"
				label={m.queue_policy_track_broken()}
				hint={m.queue_policy_track_broken_hint()}
			>
				<input
					id="queue-track-broken"
					data-field="track_broken_below_percent"
					class="input"
					type="number"
					min="0"
					max="100"
					step="any"
					aria-describedby="queue-track-broken-hint"
					bind:value={draft.track_broken_below_percent}
				/>
			</Field>

			<div class="md:col-span-2">
				<button type="submit" class="btn btn-primary" disabled={save.isPending}>
					{m.queue_policy_save()}
				</button>
			</div>
		</form>
	{/if}
</section>
