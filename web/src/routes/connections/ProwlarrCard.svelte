<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import ConnectionHubCard from '#lib/components/ConnectionHubCard.svelte';
	import { prowlarrQueries } from '#lib/queries/connections.js';
	import { deriveServiceStatus } from '#lib/connections.js';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';

	const { links, status } = prowlarrQueries();

	const summary = $derived.by(() => {
		const linkList = links.data ?? [];
		const statusList = status.data ?? [];
		const healthItems = linkList.map((l) => {
			const st = statusList.find((s) => s.link_id === l.id);
			return {
				reachable: st ? st.reachable : undefined,
				error: st?.error ?? l.last_error,
				checkedAt: st?.checked_at ?? l.last_check_at
			};
		});
		return deriveServiceStatus(healthItems);
	});

	const href = '/connections/prowlarr';
</script>

<ConnectionHubCard
	icon="search"
	title={m.nav_prowlarr()}
	{href}
	description={m.connections_prowlarr_desc()}
	pending={links.isPending || status.isPending}
	error={links.error ?? status.error}
	{summary}
	showAdd={session.isAdmin && summary.counts.total === 0}
/>
