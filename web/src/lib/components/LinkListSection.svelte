<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" generics="T extends LinkItem & { id: string }">
	import type { Snippet } from 'svelte';
	import LinkCard, { type LinkItem } from '#lib/components/LinkCard.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';

	export interface QueryLike<T> {
		isPending: boolean;
		isError: boolean;
		error: unknown;
		data: readonly T[] | undefined | null;
	}

	export interface LinkEditorLike<T> {
		open: (item: T) => void;
	}

	export interface ProbeMutationLike {
		isPending: boolean;
		mutate: (id: string) => void;
	}

	export interface RemoveMutationLike<T> {
		isPending: boolean;
		mutate: (item: T) => void;
	}

	interface Props<T extends LinkItem & { id: string }> {
		idPrefix: string;
		title: string;
		emptyText: string;
		query: QueryLike<T>;
		editor: LinkEditorLike<T>;
		probe: ProbeMutationLike;
		remove: RemoveMutationLike<T>;
		/** Rendered after the card's test status, per link. */
		statusExtra?: Snippet<[link: T]>;
		/** Extra card buttons, per link. */
		extraActions?: Snippet<[link: T]>;
		/** The right-hand column, usually the create / edit form. */
		aside: Snippet;
	}

	const {
		idPrefix,
		title,
		emptyText,
		query,
		editor,
		probe,
		remove,
		statusExtra: renderStatusExtra,
		extraActions: renderExtraActions,
		aside
	}: Props<T> = $props();

	const sectionTitleId = $derived(`${idPrefix}-links-section-title`);
</script>

<section aria-labelledby={sectionTitleId} class="w-full">
	<h2 id={sectionTitleId} class="sr-only">{title}</h2>
	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-3 xl:col-span-7">
			<h2 class="text-lg font-semibold">{title}</h2>
			<QueryGuard {query}>
				{#snippet children(items)}
					{#if items.length === 0}
						<p class="card">{emptyText}</p>
					{:else}
						<ul class="grid grid-cols-1 gap-3 xl:grid-cols-2">
							{#each items as link (link.id)}
								<LinkCard
									{link}
									testPending={probe.isPending}
									deletePending={remove.isPending}
									onedit={() => editor.open(link)}
									ontest={() => probe.mutate(link.id)}
									ondelete={() => remove.mutate(link)}
								>
									{#snippet statusExtra()}
										{@render renderStatusExtra?.(link)}
									{/snippet}
									{#snippet extraActions()}
										{@render renderExtraActions?.(link)}
									{/snippet}
								</LinkCard>
							{/each}
						</ul>
					{/if}
				{/snippet}
			</QueryGuard>
		</div>

		<div class="xl:col-span-5">
			{@render aside()}
		</div>
	</div>
</section>
