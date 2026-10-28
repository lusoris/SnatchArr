<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ConfirmAction from '#lib/components/ConfirmAction.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { runStatusLabel, snatchKindLabel } from '#lib/runs.js';
	import { formatWhen } from '#lib/stamina.js';

	type Run = components['schemas']['Run'];

	const { instanceId: id }: { instanceId: string } = $props();
	const instanceId = untrack(() => id);

	const runs = createSentioQuery<Run[]>({
		queryKey: keys.instanceRuns(instanceId),
		queryFn: async () =>
			(await api.GET('/runs', { params: { query: { instance_id: instanceId, limit: 20 } } }))
				.data ?? []
	});

	const cancel = createSentioMutation({
		mutationFn: async (runId: string) => {
			await api.POST('/runs/{runId}/cancel', { params: { path: { runId } } });
		},
		invalidates: [keys.runs, keys.caps],
		onSuccess: () => toast.success(m.run_cancelled())
	});

	/** Run cards shown on a phone before the show-all toggle; wider screens scroll a bounded table. */
	const PHONE_CAP = 5;
	let showAll = $state(false);

	/** Only a run the worker has not finished can be cancelled. */
	function cancellable(run: Run): boolean {
		return run.status === 'queued' || run.status === 'leased';
	}
</script>

<section class="mb-6" aria-labelledby="runs-title">
	<h2 id="runs-title" class="mb-3 text-lg font-semibold">{m.runs_title()}</h2>
	<ProblemNote error={cancel.error} />
	{#if runs.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if runs.isError}
		<ProblemNote error={runs.error} />
	{:else if (runs.data ?? []).length === 0}
		<p class="text-sm text-subtle">{m.runs_empty()}</p>
	{:else}
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			class="relative overflow-x-auto md:max-h-[11rem] md:overflow-y-auto"
			role="region"
			tabindex="0"
			aria-label={m.runs_region_label()}
		>
			<table class="table-stack run-cards table">
				<caption class="sr-only">{m.runs_title()}</caption>
				<thead>
					<tr>
						<th scope="col">{m.run_kind()}</th>
						<th scope="col">{m.run_status()}</th>
						<th scope="col">{m.run_started()}</th>
						<th scope="col">{m.run_finished()}</th>
						<th scope="col">{m.run_searched()}</th>
						<th scope="col"><span class="sr-only">{m.run_actions()}</span></th>
					</tr>
				</thead>
				<tbody>
					{#each runs.data ?? [] as run, index (run.id)}
						<tr class:capped={!showAll && index >= PHONE_CAP}>
							<td data-label={m.run_kind()}>
								<div>
									{snatchKindLabel(run.kind)}
									{#if run.dry_run}
										<span class="ml-1.5 rounded-full border border-line px-2 py-0.5 text-xs">
											{m.run_preview_badge()}
										</span>
									{/if}
								</div>
							</td>
							<td data-label={m.run_status()}>
								{runStatusLabel(run.status)}{run.error ? ` · ${run.error}` : ''}
							</td>
							<td data-label={m.run_started()}>
								{formatWhen(run.started_at ?? run.queued_at, getLocale())}
							</td>
							<td data-label={m.run_finished()}
								>{formatWhen(run.finished_at, getLocale()) || '–'}</td
							>
							<td data-label={m.run_searched()}>{run.searched_count}</td>
							<td data-label="">
								{#if cancellable(run)}
									<ConfirmAction
										label={m.run_cancel()}
										message={m.run_cancel_confirm({ kind: snatchKindLabel(run.kind) })}
										pending={cancel.isPending}
										onconfirm={() => cancel.mutate(run.id)}
									/>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if (runs.data ?? []).length > PHONE_CAP}
			<button
				type="button"
				class="text-brand mt-2 text-sm hover:underline md:hidden"
				aria-expanded={showAll}
				onclick={() => (showAll = !showAll)}
			>
				{showAll ? m.runs_show_fewer() : m.runs_show_all({ count: (runs.data ?? []).length })}
			</button>
		{/if}
	{/if}
</section>

<style>
	/* Phone run cards: two label/value columns keep five cards inside two viewport heights. */
	@media (width < 48rem) {
		.run-cards tbody {
			gap: 0.5rem;
		}
		.run-cards tr {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			gap: 0.125rem 0.75rem;
			padding: 0.5rem 0.75rem;
		}
		.run-cards td {
			flex-wrap: wrap;
			justify-content: flex-start;
			gap: 0 0.375rem;
			font-size: 0.875rem;
			line-height: 1.25rem;
		}
		.run-cards td::before {
			font-size: 0.75rem;
		}
		.run-cards tr.capped {
			display: none;
		}
		.run-cards td[data-label='']:empty {
			display: none;
		}
	}
</style>
