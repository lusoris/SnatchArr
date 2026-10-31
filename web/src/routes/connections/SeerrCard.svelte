<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import ConnectionHubCard from '#lib/components/ConnectionHubCard.svelte';
	import { deriveServiceStatus } from '#lib/connections.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	const links = createSentioQuery({
		queryKey: keys.seerrLinks,
		queryFn: async () => (await api.GET('/seerr')).data ?? []
	});

	const summary = $derived.by(() => {
		const linkList = links.data ?? [];
		const healthItems = linkList.map((l) => ({
			error: l.last_error,
			checkedAt: l.last_check_at
		}));
		return deriveServiceStatus(healthItems);
	});

	const href = '/connections/seerr';
</script>

<ConnectionHubCard
	kind="seerr"
	icon="film"
	title={m.nav_seerr()}
	{href}
	description={m.connections_seerr_desc()}
	pending={links.isPending}
	error={links.error}
	{summary}
	showAdd={session.isAdmin && summary.counts.total === 0}
/>
