<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import ConnectionHubCard from '#lib/components/ConnectionHubCard.svelte';
	import { downloadClientQueries } from '#lib/queries/connections.js';
	import { deriveServiceStatus } from '#lib/connections.js';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';

	const { clients, status } = downloadClientQueries();

	const summary = $derived.by(() => {
		const clientList = clients.data ?? [];
		const statusList = status.data ?? [];
		const healthItems = clientList.map((c) => {
			const st = statusList.find((s) => s.client_id === c.id);
			return {
				reachable: st ? st.reachable : undefined,
				error: st?.error,
				checkedAt: st?.checked_at
			};
		});
		return deriveServiceStatus(healthItems);
	});

	const href = '/connections/download-clients';
</script>

<ConnectionHubCard
	icon="cloud-download"
	title={m.nav_download_clients()}
	{href}
	description={m.connections_download_clients_desc()}
	pending={clients.isPending || status.isPending}
	error={clients.error ?? status.error}
	{summary}
	showAdd={session.isAdmin && summary.counts.total === 0}
/>
