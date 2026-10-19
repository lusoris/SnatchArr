<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type Run = components['schemas']['Run'];
	type PreviewTarget = components['schemas']['PreviewTarget'];

	interface Props {
		instanceId: string;
	}

	const { instanceId: id }: Props = $props();
	const instanceId = untrack(() => id);

	let activeRunId = $state<string | null>(null);
	let pollCount = $state(0);

	const previewMutation = createSentioMutation({
		mutationFn: async (kind: 'missing' | 'upgrade') => {
			const res = await api.POST('/instances/{instanceId}/preview', {
				params: {
					path: { instanceId },
					query: { kind }
				}
			});
			return res.data;
		},
		onSuccess: (data) => {
			pollCount = 0;
			activeRunId = data?.run_id ?? null;
		}
	});

	function shouldPoll(run: Run | undefined): boolean {
		if (!activeRunId || pollCount >= 60) return false;
		if (run && (run.status === 'done' || run.status === 'failed' || run.status === 'cancelled')) {
			return false;
		}
		return true;
	}

	const runQuery = createSentioQuery<Run | undefined>(() => ({
		queryKey: keys.run(activeRunId ?? ''),
		enabled: Boolean(activeRunId),
		refetchInterval: (query) => (shouldPoll(query.state.data) ? 1_000 : false),
		queryFn: async () => {
			if (!activeRunId) return undefined;
			pollCount += 1;
			const res = await api.GET('/runs/{runId}', {
				params: { path: { runId: activeRunId } }
			});
			return res.data;
		}
	}));

	function totalCost(targets: PreviewTarget[]): number {
		return targets.reduce((sum, t) => sum + t.cost, 0);
	}
</script>

<section class="card mb-6" aria-labelledby="preview-title">
	<h2 id="preview-title" class="mb-3 text-lg font-semibold">{m.preview_title()}</h2>
	<div class="mb-4 flex flex-wrap gap-2">
		<button
			type="button"
			class="btn"
			onclick={() => previewMutation.mutate('missing')}
			disabled={previewMutation.isPending}
		>
			{m.preview_missing()}
		</button>
		<button
			type="button"
			class="btn"
			onclick={() => previewMutation.mutate('upgrade')}
			disabled={previewMutation.isPending}
		>
			{m.preview_upgrade()}
		</button>
	</div>

	<ProblemNote error={previewMutation.error ?? runQuery.error} />

	{#if runQuery.data}
		{@const run = runQuery.data}
		{@const targets = run.previewed_targets ?? []}
		{@const isRunning = run.status === 'queued' || run.status === 'leased'}
		<div class="mt-4 flex flex-col gap-3">
			{#if run.seed !== undefined}
				<p class="text-xs text-subtle">{m.preview_seed({ seed: run.seed })}</p>
			{/if}

			{#if isRunning}
				<p aria-live="polite" class="text-sm text-subtle">{m.preview_running()}</p>
			{:else if targets.length === 0}
				<p class="text-sm text-subtle">{m.preview_empty()}</p>
			{:else}
				<div class="relative overflow-x-auto">
					<table class="table">
						<caption class="sr-only">{m.preview_targets_caption()}</caption>
						<thead>
							<tr>
								<th scope="col">{m.preview_target_title()}</th>
								<th scope="col">{m.preview_target_type()}</th>
								<th scope="col">{m.preview_target_id()}</th>
								<th scope="col">{m.preview_target_cost()}</th>
							</tr>
						</thead>
						<tbody>
							{#each targets as target (target.id)}
								<tr>
									<td>{target.title}</td>
									<td>{target.entity_type}</td>
									<td>{target.id}</td>
									<td>{target.cost}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<p class="text-sm font-semibold">{m.preview_total_cost({ cost: totalCost(targets) })}</p>
			{/if}
		</div>
	{/if}
</section>
