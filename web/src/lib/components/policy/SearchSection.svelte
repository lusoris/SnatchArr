<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Field from '#lib/components/Field.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import type { PolicySectionProps } from './section.js';

	let { policy = $bindable(), kind }: PolicySectionProps = $props();
</script>

<fieldset class="card flex flex-col gap-4">
	<legend class="px-1 text-sm font-semibold">{m.policy_group_search()}</legend>
	<p class="text-xs text-subtle">{m.policy_group_search_desc()}</p>

	<div class="grid gap-4 sm:grid-cols-2">
		<Field
			id="missing-per-cycle"
			label={m.policy_missing_per_cycle()}
			hint={m.policy_missing_per_cycle_hint()}
		>
			<input
				id="missing-per-cycle"
				data-field="missing_per_cycle"
				class="input"
				type="number"
				min="0"
				max="100"
				aria-describedby="missing-per-cycle-hint"
				bind:value={policy.missing_per_cycle}
			/>
		</Field>

		<Field
			id="upgrade-per-cycle"
			label={m.policy_upgrade_per_cycle()}
			hint={m.policy_upgrade_per_cycle_hint()}
		>
			<input
				id="upgrade-per-cycle"
				data-field="upgrade_per_cycle"
				class="input"
				type="number"
				min="0"
				max="100"
				aria-describedby="upgrade-per-cycle-hint"
				bind:value={policy.upgrade_per_cycle}
			/>
		</Field>
	</div>

	<div class="grid gap-4 sm:grid-cols-2">
		<Field id="release-delay" label={m.policy_release_delay()} hint={m.policy_release_delay_hint()}>
			<input
				id="release-delay"
				data-field="release_delay_h"
				class="input"
				type="number"
				min="0"
				max="8760"
				aria-describedby="release-delay-hint"
				bind:value={policy.release_delay_h}
			/>
		</Field>

		<Field id="selection" label={m.policy_selection()} hint={m.policy_selection_hint()}>
			<select
				id="selection"
				data-field="selection"
				class="select"
				aria-describedby="selection-hint"
				bind:value={policy.selection}
			>
				<option value="random"
					>{m.policy_selection_random()} - {m.policy_selection_random_hint()}</option
				>
				<option value="sequential"
					>{m.policy_selection_sequential()} - {m.policy_selection_sequential_hint()}</option
				>
				<option value="recent"
					>{m.policy_selection_recent()} - {m.policy_selection_recent_hint()}</option
				>
			</select>
		</Field>
	</div>

	<div class="flex flex-col gap-3 pt-1">
		<div class="flex flex-col gap-0.5">
			<label class="flex items-center gap-2 text-sm font-medium">
				<input
					id="monitored-only"
					data-field="monitored_only"
					type="checkbox"
					aria-describedby="monitored-only-hint"
					bind:checked={policy.monitored_only}
				/>
				{m.policy_monitored_only()}
			</label>
			<p id="monitored-only-hint" class="text-xs text-subtle">
				{m.policy_monitored_only_hint()}
			</p>
		</div>

		<div class="flex flex-col gap-0.5">
			<label class="flex items-center gap-2 text-sm font-medium">
				<input
					id="skip-future"
					data-field="skip_future_releases"
					type="checkbox"
					aria-describedby="skip-future-hint"
					bind:checked={policy.skip_future_releases}
				/>
				{m.policy_skip_future()}
			</label>
			<p id="skip-future-hint" class="text-xs text-subtle">
				{m.policy_skip_future_hint()}
			</p>
		</div>

		{#if kind === 'radarr'}
			<div class="flex flex-col gap-0.5">
				<label class="flex items-center gap-2 text-sm font-medium">
					<input
						id="skip-unavailable"
						data-field="skip_unavailable_movies"
						type="checkbox"
						aria-describedby="skip-unavailable-hint"
						bind:checked={policy.skip_unavailable_movies}
					/>
					{m.policy_skip_unavailable()}
				</label>
				<p id="skip-unavailable-hint" class="text-xs text-subtle">
					{m.policy_skip_unavailable_hint()}
				</p>
			</div>
		{/if}
	</div>
</fieldset>
