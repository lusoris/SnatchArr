<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import NavIcon from '#lib/components/shell/NavIcon.svelte';
	import { m } from '#lib/paraglide/messages.js';

	const connections = [
		{
			href: '/connections/download-clients' as const,
			label: m.nav_download_clients(),
			icon: 'cloud-download',
			description: m.connections_download_clients_desc()
		},
		{
			href: '/connections/seerr' as const,
			label: m.nav_seerr(),
			icon: 'film',
			description: m.connections_seerr_desc()
		},
		{
			href: '/connections/prowlarr' as const,
			label: m.nav_prowlarr(),
			icon: 'search',
			description: m.connections_prowlarr_desc()
		},
		{
			href: '/connections/configarr' as const,
			label: m.nav_configarr(),
			icon: 'file-code',
			description: m.connections_configarr_desc()
		}
	];
</script>

<PageHeader title={m.nav_connections()} description={m.connections_description()} documentTitle />

<section aria-labelledby="connections-heading" class="w-full">
	<h2 id="connections-heading" class="sr-only">{m.nav_connections()}</h2>
	<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-2">
		{#each connections as conn (conn.href)}
			<a
				href={resolve(conn.href)}
				class="card flex flex-col gap-2 p-5 transition-colors hover:border-control hover:shadow-xs"
			>
				<div class="flex items-center gap-3">
					<span
						class="text-brand flex h-10 w-10 items-center justify-center rounded-lg bg-[var(--color-secondary)]"
					>
						<NavIcon name={conn.icon} size={22} />
					</span>
					<h3 class="text-base font-semibold text-fg">{conn.label}</h3>
				</div>
				<p class="text-sm text-subtle">{conn.description}</p>
			</a>
		{/each}
	</div>
</section>
