<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { page } from '$app/state';
	import InstanceDetail from '$lib/components/InstanceDetail.svelte';
</script>

<!-- Remount per id so the queries never hold a stale key across navigations. -->
{#key page.params.id}
	<InstanceDetail id={page.params.id ?? ''} />
{/key}
