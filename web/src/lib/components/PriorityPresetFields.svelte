<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import { APP_KINDS, kindLabel } from '#lib/kinds.js';
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
		<div>
			<h2 class="text-base font-semibold">{m.settings_priority_presets_title()}</h2>
			<p class="text-xs text-subtle">{m.settings_priority_presets_hint()}</p>
		</div>
		<button type="button" class="btn" disabled={disabled || !canReset} onclick={onReset}>
			{m.settings_priority_reset()}
		</button>
	</div>

	<fieldset class="flex flex-col gap-2" data-field="search_priority_preset" {disabled}>
		<legend class="text-sm font-medium">{m.settings_search_priority_preset()}</legend>
		<div class="grid gap-3 sm:grid-cols-2 md:grid-cols-3">
			{#each APP_KINDS as kind (kind)}
				{#if searchPreset[kind] !== undefined}
					<Field
						id={`search-priority-${kind}`}
						label={`${m.settings_search_priority_preset()} (${kindLabel(kind)})`}
					>
						<input
							id={`search-priority-${kind}`}
							class="input"
							type="number"
							min="1"
							max="100"
							required
							{disabled}
							bind:value={searchPreset[kind]}
						/>
					</Field>
				{/if}
			{/each}
		</div>
	</fieldset>

	<fieldset class="flex flex-col gap-2" data-field="download_priority_preset" {disabled}>
		<legend class="text-sm font-medium">{m.settings_download_priority_preset()}</legend>
		<div class="grid gap-3 sm:grid-cols-2 md:grid-cols-3">
			{#each APP_KINDS as kind (kind)}
				{#if downloadPreset[kind] !== undefined}
					<Field
						id={`download-priority-${kind}`}
						label={`${m.settings_download_priority_preset()} (${kindLabel(kind)})`}
					>
						<input
							id={`download-priority-${kind}`}
							class="input"
							type="number"
							min="1"
							max="100"
							required
							{disabled}
							bind:value={downloadPreset[kind]}
						/>
					</Field>
				{/if}
			{/each}
		</div>
	</fieldset>
</div>
