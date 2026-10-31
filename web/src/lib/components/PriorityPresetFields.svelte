<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import PriorityOrderFields from '#lib/components/PriorityOrderFields.svelte';
	import { m } from '#lib/paraglide/messages.js';

	type PriorityPresetMap = components['schemas']['PriorityPresetMap'];

	interface Props {
		searchPreset: PriorityPresetMap;
		downloadPreset: PriorityPresetMap;
		canReset?: boolean;
		onReset: () => void;
		disabled?: boolean;
	}

	let {
		searchPreset = $bindable(),
		downloadPreset = $bindable(),
		canReset = false,
		onReset,
		disabled = false
	}: Props = $props();
</script>

<div class="flex flex-col gap-4 rounded-lg border border-line p-4">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0 flex-1 basis-60">
			<h2 class="text-base font-semibold">{m.settings_priority_presets_title()}</h2>
			<p class="text-xs text-subtle">{m.settings_priority_presets_hint()}</p>
		</div>
		<button type="button" class="btn" disabled={disabled || !canReset} onclick={onReset}>
			{m.settings_priority_reset()}
		</button>
	</div>

	<div class="grid min-w-0 gap-4 sm:grid-cols-2">
		<PriorityOrderFields bind:preset={searchPreset} order="search" {disabled} />
		<PriorityOrderFields bind:preset={downloadPreset} order="download" {disabled} />
	</div>
</div>
