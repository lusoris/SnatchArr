<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		description?: string;
		status?: Snippet;
		actions?: Snippet;
	}

	const { title, description, status, actions }: Props = $props();
</script>

<header class="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
	<div>
		<div class="flex flex-wrap items-center gap-2.5">
			<h1 class="text-2xl font-semibold tracking-tight text-fg">{title}</h1>
			{#if status}
				{@render status()}
			{/if}
		</div>
		{#if description}
			<p class="mt-1 text-sm text-subtle">{description}</p>
		{/if}
	</div>
	{#if actions}
		<div class="flex items-center gap-2">
			{@render actions()}
		</div>
	{/if}
</header>
