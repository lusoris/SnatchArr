<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import ActivityFeed from '#lib/components/ActivityFeed.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<div class="page-column flex flex-col md:h-[calc(100dvh-6.5rem)] lg:h-[calc(100dvh-3rem)]">
	<div class="shrink-0">
		<PageHeader title={m.nav_activity()} documentTitle />
	</div>
	<ActivityFeed />
</div>
