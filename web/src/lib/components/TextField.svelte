<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props extends Omit<HTMLInputAttributes, 'id' | 'value' | 'class'> {
		id: string;
		label: string;
		value?: string | number | null;
		hint?: string;
		/** Field-level reason from an RFC 9457 `invalid-params` entry. */
		error?: string;
	}

	let { id, label, value = $bindable(), hint, error, ...rest }: Props = $props();

	const describedBy = $derived(
		[hint ? `${id}-hint` : '', error ? `${id}-error` : ''].filter(Boolean).join(' ') || undefined
	);
</script>

<div class="field flex flex-col gap-1">
	<label for={id} class="text-sm font-medium">{label}</label>
	<input
		{id}
		class="input"
		bind:value
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={describedBy}
		{...rest}
	/>
	{#if hint}
		<p id={`${id}-hint`} class="text-xs text-subtle">{hint}</p>
	{/if}
	{#if error}
		<p id={`${id}-error`} class="text-sm text-danger">{error}</p>
	{/if}
</div>
