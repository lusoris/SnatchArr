<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import ResetMemory from '#lib/components/ResetMemory.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { instanceQuery } from './instance-query.svelte.js';

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);
	const query = instanceQuery(instanceId);
</script>

<QueryGuard {query}>
	{#snippet children(instance)}
		<ResetMemory instance={{ id: instanceId, name: instance.name }} />
	{/snippet}
</QueryGuard>
