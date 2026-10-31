<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" generics="T extends AdultCatalogItem">
	import type { Snippet } from 'svelte';
	import { createSentioQuery, type QueryKey } from '@sveltesentio/query';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import type { HelpEntry } from '#lib/help.js';

	export interface AdultCatalogItem {
		id: string;
		name: string;
		is_favorite: boolean;
		ref_count?: number;
	}

	interface Props<T extends AdultCatalogItem> {
		title: string;
		description: string;
		favoriteOnly: string;
		none: string;
		favoriteBadge: string;
		refCount: (count: number) => string;
		help?: HelpEntry;
		filterInputId: string;
		queryKey: (favorite?: boolean) => QueryKey;
		fetch: (favorite?: boolean) => Promise<T[]>;
		extraText?: Snippet<[item: T]>;
	}

	const {
		title,
		description,
		favoriteOnly,
		none,
		favoriteBadge,
		refCount,
		help,
		filterInputId,
		queryKey,
		fetch: fetchItems,
		extraText
	}: Props<T> = $props();

	let favoriteOnlyFilter = $state(false);

	const catalogQuery = createSentioQuery(() => ({
		queryKey: queryKey(favoriteOnlyFilter ? true : undefined),
		queryFn: async () => fetchItems(favoriteOnlyFilter ? true : undefined)
	}));
</script>

<section class="space-y-6">
	<PageHeader {title} {description} documentTitle {help} />

	<div class="flex items-center justify-between">
		<label class="flex cursor-pointer items-center gap-2 text-sm text-fg">
			<input
				type="checkbox"
				id={filterInputId}
				aria-label={favoriteOnly}
				bind:checked={favoriteOnlyFilter}
			/>
			<span>{favoriteOnly}</span>
		</label>
	</div>

	<QueryGuard query={catalogQuery}>
		{#snippet children(items: T[])}
			{#if items.length === 0}
				<div class="card p-8 text-center text-subtle">
					<p>{none}</p>
				</div>
			{:else}
				<ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{#each items as item (item.id)}
						<li class="card flex flex-col justify-between p-4">
							<div class="space-y-1">
								<div class="flex items-start justify-between gap-2">
									<h2 class="font-medium break-words text-fg">
										{item.name}
										{#if extraText}
											{@render extraText(item)}
										{/if}
									</h2>
									{#if item.is_favorite}
										<span
											class="bg-brand/15 text-brand shrink-0 rounded px-2 py-0.5 text-xs font-medium"
										>
											{favoriteBadge}
										</span>
									{/if}
								</div>
								<p class="text-xs text-subtle">
									{refCount(item.ref_count ?? 0)}
								</p>
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		{/snippet}
	</QueryGuard>
</section>
