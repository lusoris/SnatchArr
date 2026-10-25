<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import InstanceSelect from '#lib/components/InstanceSelect.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

	type Instance = components['schemas']['Instance'];
	type GateSeries = components['schemas']['WatchawareGateSeries'];
	type GateDecision = components['schemas']['WatchawareGateDecision'];

	interface Props {
		instances?: Instance[];
	}

	let { instances: propInstances }: Props = $props();

	const instancesQuery = createSentioQuery(() => ({
		queryKey: keys.instances,
		enabled: !propInstances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	}));

	const instances = $derived(propInstances ?? instancesQuery.data ?? []);
	const sonarrInstances = $derived(instances.filter((i) => i.kind === 'sonarr'));

	let selectedInstanceId = $state('');

	$effect(() => {
		if (sonarrInstances.length > 0) {
			if (!selectedInstanceId || !sonarrInstances.some((i) => i.id === selectedInstanceId)) {
				selectedInstanceId = sonarrInstances[0].id;
			}
		} else {
			selectedInstanceId = '';
		}
	});

	const seriesQuery = createSentioQuery<GateSeries[]>(() => ({
		queryKey: keys.watchawareGateSeries(selectedInstanceId),
		enabled: !!selectedInstanceId,
		queryFn: async () => {
			const res = await api.GET('/watchaware/gate/series', {
				params: { query: { instance_id: selectedInstanceId, limit: 100 } }
			});
			return res.data ?? [];
		}
	}));

	const decisionsQuery = createSentioQuery<GateDecision[]>(() => ({
		queryKey: keys.watchawareGateDecisions(selectedInstanceId),
		enabled: !!selectedInstanceId,
		queryFn: async () => {
			const res = await api.GET('/watchaware/gate/decisions', {
				params: { query: { instance_id: selectedInstanceId, limit: 100 } }
			});
			return res.data ?? [];
		}
	}));

	const seriesMap = $derived(new Map((seriesQuery.data ?? []).map((s) => [s.series_id, s.title])));

	function seriesTitle(id: number): string {
		return seriesMap.get(id) ?? `#${id}`;
	}

	function stateLabel(state: string): string {
		switch (state) {
			case 'gated':
				return m.watchaware_gate_state_gated();
			case 'exempt_tag':
				return m.watchaware_gate_state_exempt_tag();
			case 'before_gate':
				return m.watchaware_gate_state_before_gate();
			case 'files_beyond_gate':
				return m.watchaware_gate_state_files_beyond_gate();
			default:
				return state;
		}
	}

	function actionLabel(action: string): string {
		switch (action) {
			case 'unmonitor':
				return m.watchaware_gate_action_unmonitor();
			case 'promote':
				return m.watchaware_gate_action_promote();
			default:
				return action;
		}
	}

	function formatHeldSeasons(held: number[] | undefined): string {
		if (!held || held.length === 0) {
			return '—';
		}
		return held.join(', ');
	}
</script>

<div class="flex flex-col gap-6">
	<p class="card text-subtle">{m.watchaware_gate_observe_note()}</p>

	{#if instancesQuery.isPending && !propInstances}
		<p aria-live="polite">{m.loading()}</p>
	{:else if instancesQuery.isError && !propInstances}
		<ProblemNote error={instancesQuery.error} />
	{:else if sonarrInstances.length === 0}
		<p class="card">{m.watchaware_gate_no_sonarr()}</p>
	{:else}
		<div class="max-w-xs">
			<InstanceSelect
				id="season-gate-instance-select"
				label={m.watchaware_gate_instance()}
				instances={sonarrInstances}
				bind:value={selectedInstanceId}
				noneLabel={m.watchaware_gate_no_instance()}
			/>
		</div>

		{#if seriesQuery.isPending || decisionsQuery.isPending}
			<p aria-live="polite">{m.loading()}</p>
		{:else if seriesQuery.isError}
			<ProblemNote error={seriesQuery.error} />
		{:else if decisionsQuery.isError}
			<ProblemNote error={decisionsQuery.error} />
		{:else}
			<section class="flex flex-col gap-3">
				<h2 class="text-base font-semibold">{m.watchaware_gate_series_title()}</h2>
				{#if (seriesQuery.data ?? []).length === 0}
					<p class="card">{m.watchaware_gate_series_empty()}</p>
				{:else}
					<div class="relative overflow-x-auto">
						<table class="table-stack table">
							<caption class="sr-only">{m.watchaware_gate_series_title()}</caption>
							<thead>
								<tr>
									<th scope="col">{m.watchaware_gate_col_title()}</th>
									<th scope="col">{m.watchaware_gate_col_state()}</th>
									<th scope="col">{m.watchaware_gate_col_gate_season()}</th>
									<th scope="col">{m.watchaware_gate_col_held_seasons()}</th>
									<th scope="col">{m.watchaware_gate_col_updated()}</th>
								</tr>
							</thead>
							<tbody>
								{#each seriesQuery.data ?? [] as s (s.series_id)}
									<tr>
										<td data-label={m.watchaware_gate_col_title()}>{s.title}</td>
										<td data-label={m.watchaware_gate_col_state()}>{stateLabel(s.state)}</td>
										<td data-label={m.watchaware_gate_col_gate_season()}>{s.gate_season}</td>
										<td data-label={m.watchaware_gate_col_held_seasons()}>
											{formatHeldSeasons(s.held_seasons)}
										</td>
										<td data-label={m.watchaware_gate_col_updated()}>
											{formatWhen(s.updated_at, getLocale())}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</section>

			<section class="flex flex-col gap-3">
				<h2 class="text-base font-semibold">{m.watchaware_gate_decisions_title()}</h2>
				{#if (decisionsQuery.data ?? []).length === 0}
					<p class="card">{m.watchaware_gate_decisions_empty()}</p>
				{:else}
					<div class="relative overflow-x-auto">
						<table class="table-stack table">
							<caption class="sr-only">{m.watchaware_gate_decisions_title()}</caption>
							<thead>
								<tr>
									<th scope="col">{m.watchaware_gate_col_series()}</th>
									<th scope="col">{m.watchaware_gate_col_season()}</th>
									<th scope="col">{m.watchaware_gate_col_action()}</th>
									<th scope="col">{m.watchaware_gate_col_reason()}</th>
									<th scope="col">{m.watchaware_gate_col_decided_at()}</th>
								</tr>
							</thead>
							<tbody>
								{#each decisionsQuery.data ?? [] as d (d.id)}
									<tr>
										<td data-label={m.watchaware_gate_col_series()}>
											{seriesTitle(d.series_id)}
										</td>
										<td data-label={m.watchaware_gate_col_season()}>{d.season_number}</td>
										<td data-label={m.watchaware_gate_col_action()}>
											<span>{actionLabel(d.action)}</span>
											{#if !d.would_act}
												<span class="ml-1.5 rounded-full border border-line px-2 py-0.5 text-xs">
													{m.watchaware_gate_mode_observe()}
												</span>
											{/if}
										</td>
										<td data-label={m.watchaware_gate_col_reason()}>{d.reason}</td>
										<td data-label={m.watchaware_gate_col_decided_at()}>
											{formatWhen(d.decided_at, getLocale())}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</section>
		{/if}
	{/if}
</div>
