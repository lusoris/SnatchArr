<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '$lib/api/client.js';
	import ProblemNote from '$lib/components/ProblemNote.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { keys } from '$lib/query.js';
	import { formatWhen } from '$lib/stamina.js';

	const status = createSentioQuery({
		queryKey: keys.cleanuparr,
		queryFn: async () => (await api.GET('/cleanuparr/status')).data ?? [],
		refetchInterval: 60_000
	});
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.cleanuparr_title()}</h1>

{#if status.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if status.isError}
	<ProblemNote error={status.error} />
{:else if (status.data ?? []).length === 0}
	<p class="card">{m.cleanuparr_empty()}</p>
{:else}
	{#each status.data ?? [] as link (link.link_id)}
		<section class="card mb-4" aria-labelledby={`cleanuparr-${link.link_id}`}>
			<h2 id={`cleanuparr-${link.link_id}`} class="font-semibold">
				{link.name} <span class="text-sm text-subtle">{link.version ?? ''}</span>
			</h2>
			<p class="text-sm" role="status">
				{link.reachable ? m.dlclients_reachable() : m.dlclients_unreachable()}
				{#if link.error}<span class="text-danger"> {link.error}</span>{/if}
			</p>
			<p class="text-sm">
				{m.cleanuparr_struck({ n: link.struck_downloads })} · {m.cleanuparr_marked({
					n: link.marked_for_removal
				})}
			</p>
			{#if link.recent_strikes.length > 0}
				<ul class="mt-2 text-sm">
					{#each link.recent_strikes as s (s.id)}
						<li>
							<span class="text-subtle">{formatWhen(s.created_at, getLocale())}</span>
							{s.type} · {s.title}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/each}
{/if}
