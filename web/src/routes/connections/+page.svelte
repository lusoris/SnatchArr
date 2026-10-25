<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';
	import ConfigarrCard from './ConfigarrCard.svelte';
	import DownloadClientsCard from './DownloadClientsCard.svelte';
	import ProwlarrCard from './ProwlarrCard.svelte';
	import SeerrCard from './SeerrCard.svelte';
</script>

<PageHeader title={m.nav_connections()} description={m.connections_description()} documentTitle />

<section aria-labelledby="connections-heading" class="w-full">
	<h2 id="connections-heading" class="sr-only">{m.nav_connections()}</h2>
	<div class="grid grid-cols-1 items-stretch gap-4 md:grid-cols-2 xl:grid-cols-4">
		{#if session.isAdmin}
			<DownloadClientsCard />
			<SeerrCard />
		{/if}
		<ProwlarrCard />
		{#if session.isAdmin}
			<ConfigarrCard />
		{/if}
	</div>
</section>
