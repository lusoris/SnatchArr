<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { session } from '#lib/session.svelte.js';

	interface Props {
		heading: string;
		onsubmit: () => void;
		children: Snippet;
		admin?: boolean;
	}

	const { heading, onsubmit, children, admin }: Props = $props();
	// A prop fallback is read once; $derived keeps following the session as it loads.
	const show = $derived(admin ?? session.isAdmin);

	function handleSubmit(e: SubmitEvent): void {
		e.preventDefault();
		onsubmit();
	}
</script>

{#if show}
	<form class="card flex max-w-xl flex-col gap-4" onsubmit={handleSubmit}>
		<h4 class="font-medium">{heading}</h4>
		{@render children()}
	</form>
{/if}
