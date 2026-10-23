<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { instanceQuery } from './instance-query.svelte.js';

	interface Props {
		id: string | (() => string);
	}

	const { id }: Props = $props();
	const query = instanceQuery(untrack(() => id));
</script>

{#if query.isPending}
	<span data-testid="status" data-state="loading"></span>
{:else if query.isError}
	<span data-testid="status" data-state="error">{String(query.error?.message ?? query.error)}</span>
{:else if query.data}
	<span data-testid="status" data-state="loaded">{query.data.name}</span>
{/if}
