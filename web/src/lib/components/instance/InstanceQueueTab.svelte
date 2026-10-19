<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import QueuePolicy from '#lib/components/QueuePolicy.svelte';
	import QueueStrikes from '#lib/components/QueueStrikes.svelte';

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);
</script>

<QueuePolicy {instanceId} />
<QueueStrikes {instanceId} />
