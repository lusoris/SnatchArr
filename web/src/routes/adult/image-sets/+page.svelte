<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type AdultImageSet = components['schemas']['AdultImageSet'];
	type AdultImageSetDetail = components['schemas']['AdultImageSetDetail'];

	const imageSets = createSentioQuery<AdultImageSet[]>({
		queryKey: keys.adultImageSets,
		queryFn: async () => (await api.GET('/adult/image-sets')).data?.items ?? []
	});

	let selectedId = $state<string | null>(null);
	let scanIgnoreStatus = $state<boolean | null>(null);

	const selectedSet = createSentioQuery<AdultImageSetDetail | null>(() => ({
		queryKey: selectedId ? keys.adultImageSet(selectedId) : ['adult', 'image-sets', 'none'],
		queryFn: async () => {
			if (!selectedId) return null;
			return (
				(
					await api.GET('/adult/image-sets/{setId}', {
						params: { path: { setId: selectedId } }
					})
				).data ?? null
			);
		},
		enabled: selectedId !== null
	}));

	$effect(() => {
		if (selectedSet.data?.ignore_file_present !== undefined) {
			scanIgnoreStatus = selectedSet.data.ignore_file_present;
		}
	});

	$effect(() => {
		if (selectedId === null && imageSets.data && imageSets.data.length > 0) {
			selectedId = imageSets.data[0].id;
		}
	});

	const scanNow = createSentioMutation({
		mutationFn: async () => (await api.POST('/adult/scan')).data,
		invalidates: [keys.adultImageSets],
		onSuccess: (summary) => {
			if (summary) {
				scanIgnoreStatus = summary.ignore_file_present;
				toast.success(
					m.adult_image_sets_scanned_toast({
						sets: summary.sets_found,
						items: summary.items_found
					})
				);
			}
		},
		onError: (err) => toast.error(describeError(err))
	});

	function formatBytes(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
		if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
		return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
	}

	function selectSet(id: string): void {
		selectedId = id;
	}
</script>

<PageHeader
	title={m.adult_image_sets_title()}
	description={m.adult_image_sets_subtitle()}
	documentTitle
	help={HELP_MAP['/adult/image-sets']}
>
	{#snippet actions()}
		{#if session.isAdmin}
			<button
				type="button"
				class="btn"
				onclick={() => scanNow.mutate()}
				disabled={scanNow.isPending}
			>
				{scanNow.isPending ? m.adult_image_sets_scanning() : m.adult_image_sets_scan_now()}
			</button>
		{/if}
	{/snippet}
</PageHeader>

<section aria-labelledby="adult-image-sets-status-title" class="card mb-6 w-full">
	<h2 id="adult-image-sets-status-title" class="mb-2 text-lg font-semibold">
		{m.adult_image_sets_status_title()}
	</h2>
	<div class="flex flex-wrap items-center gap-4 text-sm text-subtle">
		{#if scanIgnoreStatus !== null}
			<span class={scanIgnoreStatus ? 'text-success' : 'text-danger'}>
				{scanIgnoreStatus
					? m.adult_image_sets_ignore_file_present()
					: m.adult_image_sets_ignore_file_missing()}
			</span>
		{/if}
	</div>
</section>

<section aria-labelledby="adult-image-sets-list-title" class="w-full">
	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-3 xl:col-span-5">
			<h2 id="adult-image-sets-list-title" class="text-lg font-semibold">
				{m.adult_image_sets_title()}
			</h2>
			<QueryGuard query={imageSets}>
				{#snippet children(items)}
					{#if items.length === 0}
						<p class="card">{m.adult_image_sets_none()}</p>
					{:else}
						<ul class="flex flex-col gap-3">
							{#each items as set (set.id)}
								<li
									class="card flex flex-col justify-between {selectedId === set.id
										? 'ring-focus ring-2'
										: ''}"
								>
									<div class="flex flex-col gap-1.5">
										<h3 class="font-semibold">{set.title}</h3>
										<span class="text-xs break-all text-subtle">{set.directory_path}</span>
										<div class="flex flex-wrap items-center gap-3 text-xs text-subtle">
											<span>{m.adult_image_sets_item_count({ count: set.item_count })}</span>
											<span>{formatBytes(set.total_size_bytes)}</span>
										</div>
									</div>
									<div class="mt-3 flex justify-end">
										<button
											type="button"
											class="btn btn-sm"
											aria-label="{m.adult_image_sets_view_items()} {set.title}"
											onclick={() => selectSet(set.id)}
										>
											{m.adult_image_sets_view_items()}
										</button>
									</div>
								</li>
							{/each}
						</ul>
					{/if}
				{/snippet}
			</QueryGuard>
		</div>

		<div class="flex flex-col gap-3 xl:col-span-7">
			{#if selectedId}
				<section aria-labelledby="adult-image-set-detail-title" class="card flex flex-col gap-4">
					<div class="flex items-center justify-between">
						<h2 id="adult-image-set-detail-title" class="text-lg font-semibold">
							{m.adult_image_sets_detail_title()}
						</h2>
						{#if selectedSet.data}
							<span class="text-xs text-subtle">{selectedSet.data.title}</span>
						{/if}
					</div>
					<QueryGuard query={selectedSet}>
						{#snippet children(detail)}
							{#if !detail || detail.items.length === 0}
								<p class="text-sm text-subtle">{m.adult_image_sets_empty_set()}</p>
							{:else}
								<ul class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
									{#each detail.items as item (item.id)}
										<li class="border-border flex flex-col gap-1 rounded border p-2">
											<img
												src="/api/v1/adult/image-items/{item.id}/file"
												alt={item.file_name}
												class="aspect-square w-full rounded object-cover"
												loading="lazy"
											/>
											<span class="truncate text-xs font-medium" title={item.file_name}>
												{item.file_name}
											</span>
											<span class="text-[10px] text-subtle">
												{formatBytes(item.file_size_bytes)}
											</span>
										</li>
									{/each}
								</ul>
							{/if}
						{/snippet}
					</QueryGuard>
				</section>
			{/if}
		</div>
	</div>
</section>
