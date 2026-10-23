<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { instanceQuery } from './instance-query.svelte.js';

	interface Props {
		id: string | (() => string);
	}

	const { id }: Props = $props();
	const query = instanceQuery(() => (typeof id === 'function' ? id() : id));
</script>

{#if query.isPending}
	<span data-testid="status">loading</span>
{:else if query.isError}
	<span data-testid="status"
		>error: {query.error instanceof Error ? query.error.message : 'failed'}</span
	>
{:else if query.data}
	<span data-testid="status">loaded: {query.data.name}</span>
{/if}
