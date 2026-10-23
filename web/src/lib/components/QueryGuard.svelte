<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		query: {
			isPending: boolean;
			isError: boolean;
			error: unknown;
			data: T | undefined | null;
		};
		children: Snippet<[T]>;
	}

	const { query, children }: Props = $props();
</script>

{#if query.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if query.isError}
	<ProblemNote error={query.error} />
{:else if query.data}
	{@render children(query.data)}
{/if}
