<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		id: string;
		label: string;
		hint?: string;
		children: Snippet;
	}

	const { id, label, hint, children }: Props = $props();
</script>

<div class="field flex flex-col gap-1">
	<label for={id} class="text-sm font-medium">{label}</label>
	{@render children()}
	{#if hint}
		<p id={`${id}-hint`} class="text-xs text-subtle">{hint}</p>
	{/if}
</div>
