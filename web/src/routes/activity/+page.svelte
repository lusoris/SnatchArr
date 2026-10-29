<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import ActivityFeed from '#lib/components/ActivityFeed.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
</script>

<!-- min-h keeps the feed usable on short landscape screens; the page scrolls instead. -->
<div
	class="page-column flex flex-col md:h-[calc(100dvh-6.5rem)] md:min-h-[28rem] lg:h-[calc(100dvh-3rem)]"
>
	<div class="shrink-0">
		<PageHeader title={m.nav_activity()} documentTitle help={HELP_MAP['/activity']} />
	</div>
	<ActivityFeed />
</div>
