<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type AdultPerformer = components['schemas']['AdultPerformer'];

	let favoriteOnly = $state(false);

	const performersQuery = createSentioQuery(() => ({
		queryKey: keys.adultPerformers(favoriteOnly ? true : undefined),
		queryFn: async () => {
			const res = await api.GET('/adult/performers', {
				params: {
					query: {
						favorite: favoriteOnly ? true : undefined,
						limit: 200
					}
				}
			});
			return res.data?.items ?? [];
		}
	}));
</script>

<section class="space-y-6">
	<PageHeader
		title={m.adult_performers_title()}
		description={m.adult_performers_subtitle()}
		documentTitle
		help={HELP_MAP['/adult/performers']}
	/>

	<div class="flex items-center justify-between">
		<label class="flex cursor-pointer items-center gap-2 text-sm text-fg">
			<input
				type="checkbox"
				id="performers-favorite-filter"
				aria-label={m.adult_performers_favorite_only()}
				bind:checked={favoriteOnly}
			/>
			<span>{m.adult_performers_favorite_only()}</span>
		</label>
	</div>

	<QueryGuard query={performersQuery}>
		{#snippet children(performers: AdultPerformer[])}
			{#if performers.length === 0}
				<div class="card p-8 text-center text-subtle">
					<p>{m.adult_performers_none()}</p>
				</div>
			{:else}
				<ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{#each performers as performer (performer.id)}
						<li class="card flex flex-col justify-between p-4">
							<div class="space-y-1">
								<div class="flex items-start justify-between gap-2">
									<h2 class="font-medium break-words text-fg">
										{performer.name}
										{#if performer.disambiguation}
											<span class="text-xs text-subtle">({performer.disambiguation})</span>
										{/if}
									</h2>
									{#if performer.is_favorite}
										<span
											class="bg-brand/15 text-brand shrink-0 rounded px-2 py-0.5 text-xs font-medium"
										>
											{m.adult_performers_favorite_badge()}
										</span>
									{/if}
								</div>
								<p class="text-xs text-subtle">
									{m.adult_performers_ref_count({ count: performer.ref_count ?? 0 })}
								</p>
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		{/snippet}
	</QueryGuard>
</section>
