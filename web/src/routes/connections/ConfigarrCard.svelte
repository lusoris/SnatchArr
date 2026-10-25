<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import ConnectionHubCard from '#lib/components/ConnectionHubCard.svelte';
	import { deriveConfigarrStatus } from '#lib/connections.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	const status = createSentioQuery({
		queryKey: keys.configarr,
		queryFn: async () => (await api.GET('/configarr/status')).data
	});

	const summary = $derived(deriveConfigarrStatus(status.data, getLocale()));
	const href = '/connections/configarr';
</script>

<ConnectionHubCard
	icon="file-code"
	title={m.nav_configarr()}
	{href}
	description={m.connections_configarr_desc()}
	pending={status.isPending}
	error={status.error}
	{summary}
	showAdd={session.isAdmin && summary.counts.total === 0}
/>
