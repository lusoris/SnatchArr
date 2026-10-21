<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import BrandMark from '#lib/components/BrandMark.svelte';

	interface Props {
		title: string;
		intro?: string;
		notice?: Snippet;
		children: Snippet;
	}

	const { title, intro, notice, children }: Props = $props();
</script>

<div class="mx-auto w-full max-w-sm">
	<div class="text-brand mb-6 flex justify-center">
		<BrandMark size={56} />
	</div>
	<h1 class="{intro ? 'mb-2' : 'mb-4'} text-center text-2xl font-semibold">{title}</h1>
	{#if intro}
		<p class="mb-4 text-center text-sm text-subtle">{intro}</p>
	{/if}
	{#if notice}
		{@render notice()}
	{/if}
	{@render children()}
</div>
