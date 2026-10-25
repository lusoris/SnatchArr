<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import ActivityFeed from '#lib/components/ActivityFeed.svelte';
	import PreviewPanel from '#lib/components/PreviewPanel.svelte';
	import RunList from '#lib/components/RunList.svelte';

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);
</script>

<PreviewPanel {instanceId} />
<RunList {instanceId} />
<ActivityFeed fixedInstanceId={instanceId} />
