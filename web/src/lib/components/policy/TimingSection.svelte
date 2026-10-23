<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Field from '#lib/components/Field.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import type { PolicySectionProps } from './section.js';

	let { policy = $bindable() }: PolicySectionProps = $props();

	function handleIntervalInput(e: Event) {
		const target = e.currentTarget as HTMLInputElement;
		const minutes = Number(target.value);
		if (!Number.isNaN(minutes)) {
			policy.cycle_interval_s = Math.round(minutes * 60);
		}
	}
</script>

<fieldset class="card flex flex-col gap-4">
	<legend class="px-1 text-sm font-semibold">{m.policy_group_timing()}</legend>
	<p class="text-xs text-subtle">{m.policy_group_timing_desc()}</p>

	<div class="grid gap-4 sm:grid-cols-2">
		<Field
			id="cycle-interval"
			label={m.policy_cycle_interval()}
			hint={m.policy_cycle_interval_hint()}
		>
			<input
				id="cycle-interval"
				data-field="cycle_interval_s"
				class="input"
				type="number"
				min="1"
				max="10080"
				aria-describedby="cycle-interval-hint"
				value={Math.round((policy.cycle_interval_s || 60) / 60)}
				oninput={handleIntervalInput}
			/>
		</Field>

		<Field id="processed-ttl" label={m.policy_processed_ttl()} hint={m.policy_processed_ttl_hint()}>
			<input
				id="processed-ttl"
				data-field="processed_ttl_h"
				class="input"
				type="number"
				min="1"
				max="8760"
				aria-describedby="processed-ttl-hint"
				bind:value={policy.processed_ttl_h}
			/>
		</Field>
	</div>

	<div class="grid gap-4 sm:grid-cols-2">
		<Field
			id="recent-grab-window"
			label={m.policy_recent_grab_window()}
			hint={m.policy_recent_grab_window_hint()}
		>
			<input
				id="recent-grab-window"
				data-field="recent_grab_window_h"
				class="input"
				type="number"
				min="0"
				max="720"
				aria-describedby="recent-grab-window-hint"
				bind:value={policy.recent_grab_window_h}
			/>
		</Field>

		<Field id="afterglow-max" label={m.policy_afterglow_max()} hint={m.policy_afterglow_max_hint()}>
			<input
				id="afterglow-max"
				data-field="afterglow_max_h"
				class="input"
				type="number"
				min="1"
				max="8760"
				aria-describedby="afterglow-max-hint"
				bind:value={policy.afterglow_max_h}
			/>
		</Field>
	</div>
</fieldset>
