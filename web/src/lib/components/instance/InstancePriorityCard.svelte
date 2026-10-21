<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type Instance = components['schemas']['Instance'];
	type InstancePriority = components['schemas']['InstancePriority'];
	type InstancePriorityInput = components['schemas']['InstancePriorityInput'];
	type PrioritySource = components['schemas']['PrioritySource'];

	const { instance }: { instance: Instance } = $props();
	const initial = untrack(() => instance);

	const rankSchema = z.number().int().min(1).max(100).nullable();
	const prioritySchema = z.object({
		search_priority: rankSchema,
		download_priority: rankSchema
	}) satisfies z.ZodType<InstancePriorityInput>;

	const priority = createSentioQuery<InstancePriority | undefined>(() => ({
		queryKey: keys.instancePriority(initial.id),
		queryFn: async () =>
			(
				await api.GET('/instances/{instanceId}/priority', {
					params: { path: { instanceId: initial.id } }
				})
			).data
	}));

	let searchOverride = $state<number | null>(null);
	let downloadOverride = $state<number | null>(null);
	let synced = $state(false);

	$effect(() => {
		if (priority.data && !synced) {
			searchOverride = priority.data.search_priority ?? null;
			downloadOverride = priority.data.download_priority ?? null;
			synced = true;
		}
	});

	const save = createSentioMutation({
		mutationFn: async (body: InstancePriorityInput) =>
			(
				await api.PUT('/instances/{instanceId}/priority', {
					params: { path: { instanceId: initial.id } },
					body
				})
			).data,
		invalidates: [keys.instancePriority(initial.id)],
		onSuccess: (res) => {
			if (res) {
				searchOverride = res.search_priority ?? null;
				downloadOverride = res.download_priority ?? null;
			}
			toast.success(m.instance_priority_saved());
		},
		onError: (err) => toast.error(describeError(err))
	});

	function sourceLabel(src: PrioritySource): string {
		return src === 'instance'
			? m.instance_priority_source_instance()
			: m.instance_priority_source_preset();
	}

	function parseNullableInt(raw: string): number | null {
		const trimmed = raw.trim();
		if (trimmed === '') return null;
		const parsed = parseInt(trimmed, 10);
		return Number.isNaN(parsed) ? null : parsed;
	}

	function handleSubmit(e: SubmitEvent): void {
		e.preventDefault();
		const parsed = prioritySchema.safeParse({
			search_priority: searchOverride,
			download_priority: downloadOverride
		});
		if (parsed.success) save.mutate(parsed.data);
	}

	function clearOverrides(): void {
		searchOverride = null;
		downloadOverride = null;
		save.mutate({ search_priority: null, download_priority: null });
	}
</script>

<form
	class="card mb-6 flex max-w-xl flex-col gap-3"
	aria-labelledby="instance-priority-title"
	onsubmit={handleSubmit}
>
	<div>
		<h2 id="instance-priority-title" class="text-lg font-semibold">
			{m.instance_priority_title()}
		</h2>
		<p class="text-xs text-subtle">{m.instance_priority_hint()}</p>
	</div>

	{#if priority.data}
		<div class="flex flex-wrap gap-4 text-xs text-subtle" data-testid="effective-priority">
			<span>
				{m.instance_priority_effective_search({
					rank: priority.data.effective_search_priority,
					source: sourceLabel(priority.data.search_source)
				})}
			</span>
			<span>
				{m.instance_priority_effective_download({
					rank: priority.data.effective_download_priority,
					source: sourceLabel(priority.data.download_source)
				})}
			</span>
		</div>
	{/if}

	<div class="grid gap-3 sm:grid-cols-2">
		<Field id="priority-search" label={m.instance_priority_search()}>
			<input
				id="priority-search"
				data-field="search_priority"
				class="input"
				type="number"
				min="1"
				max="100"
				step="1"
				placeholder={m.instance_priority_follow_preset()}
				value={searchOverride ?? ''}
				oninput={(e) => {
					searchOverride = parseNullableInt((e.currentTarget as HTMLInputElement).value);
				}}
			/>
		</Field>
		<Field id="priority-download" label={m.instance_priority_download()}>
			<input
				id="priority-download"
				data-field="download_priority"
				class="input"
				type="number"
				min="1"
				max="100"
				step="1"
				placeholder={m.instance_priority_follow_preset()}
				value={downloadOverride ?? ''}
				oninput={(e) => {
					downloadOverride = parseNullableInt((e.currentTarget as HTMLInputElement).value);
				}}
			/>
		</Field>
	</div>

	<ProblemNote error={save.error} />

	<div class="flex flex-wrap gap-2">
		<button type="submit" class="btn btn-primary" disabled={save.isPending}>
			{m.instance_priority_save()}
		</button>
		<button type="button" class="btn" disabled={save.isPending} onclick={clearOverrides}>
			{m.instance_priority_clear()}
		</button>
	</div>
</form>
