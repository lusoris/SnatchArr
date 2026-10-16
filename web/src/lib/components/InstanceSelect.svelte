<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';

	type Instance = components['schemas']['Instance'];

	interface Props {
		id: string;
		label: string;
		/** Instance id, or '' for the none option. */
		value: string;
		instances: readonly Instance[];
		/** Label of the '' option: what no instance means here. */
		noneLabel: string;
		error?: string;
	}

	let { id, label, value = $bindable(), instances, noneLabel, error }: Props = $props();
</script>

<div class="field flex flex-col gap-1">
	<label for={id} class="text-sm font-medium">{label}</label>
	<select
		{id}
		class="select"
		bind:value
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={error ? `${id}-error` : undefined}
	>
		<option value="">{noneLabel}</option>
		{#each instances as inst (inst.id)}
			<option value={inst.id}>{inst.name}</option>
		{/each}
	</select>
	{#if error}
		<p id={`${id}-error`} class="text-sm text-danger">{error}</p>
	{/if}
</div>
