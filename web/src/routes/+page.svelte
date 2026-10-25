<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ConfirmAction from '#lib/components/ConfirmAction.svelte';
	import EmptyFleetOnboarding from '#lib/components/EmptyFleetOnboarding.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import KpiTile from '#lib/components/KpiTile.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import StaminaGauge from '#lib/components/StaminaGauge.svelte';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import {
		activePauseText,
		eventLevelLabel,
		eventStatus,
		formatScheduleWindow,
		instanceStatus,
		kpiFleet,
		kpiProwlarr,
		kpiSearches,
		lastRunForInstance,
		nextScheduleForInstance
	} from '#lib/overview.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { instanceKindLabel } from '#lib/products.js';
	import { countExhausted, countHeadroom } from '#lib/prowlarr.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

	type Instance = components['schemas']['Instance'];
	type CapStatus = components['schemas']['CapStatus'];
	type ProwlarrStatus = components['schemas']['ProwlarrStatus'];
	type Run = components['schemas']['Run'];
	type Event = components['schemas']['Event'];
	type Schedule = components['schemas']['Schedule'];

	const instances = createSentioQuery<Instance[]>({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	const caps = createSentioQuery<CapStatus[]>({
		queryKey: keys.caps,
		queryFn: async () => (await api.GET('/hourly-caps')).data ?? [],
		refetchInterval: 30_000
	});

	const prowlarr = createSentioQuery<ProwlarrStatus[]>({
		queryKey: keys.prowlarr,
		queryFn: async () => (await api.GET('/prowlarr/status')).data ?? [],
		refetchInterval: 30_000
	});

	const runs = createSentioQuery<Run[]>({
		queryKey: keys.runs,
		queryFn: async () => (await api.GET('/runs', { params: { query: { limit: 50 } } })).data ?? [],
		refetchInterval: 30_000
	});

	const events = createSentioQuery<Event[]>({
		queryKey: keys.events,
		queryFn: async () =>
			(await api.GET('/events', { params: { query: { page_size: 10 } } })).data ?? [],
		refetchInterval: 30_000
	});

	const schedules = createSentioQuery<Schedule[]>({
		queryKey: keys.schedules,
		queryFn: async () => (await api.GET('/schedules')).data ?? [],
		refetchInterval: 60_000
	});

	const quickie = createSentioMutation({
		mutationFn: async (v: { id: string; name: string; kind: 'missing' | 'upgrade' }) => {
			await api.POST('/instances/{instanceId}/runs', {
				params: { path: { instanceId: v.id } },
				body: { kind: v.kind }
			});
			return v.name;
		},
		invalidates: [keys.runs, keys.caps],
		onSuccess: (name) => toast.success(m.quickie_queued({ name })),
		onError: (err) => toast.error(describeError(err))
	});

	function capFor(inst: Instance, list: CapStatus[] | undefined): CapStatus | undefined {
		return list?.find((c) => c.scope === 'instance' && c.instance_id === inst.id);
	}

	const searchesKpi = $derived(kpiSearches(caps.data));
	const fleetKpi = $derived(kpiFleet(instances.data));
	const prowlarrKpi = $derived(kpiProwlarr(prowlarr.data));
</script>

<PageHeader title={m.nav_overview()} help={HELP_MAP['/']} />

<div class="overview-content flex min-w-0 flex-col gap-6">
	<!-- Row 1: KPI tiles (2-up on phone, 3-col on tablet/desktop) -->
	<section
		class="grid min-w-0 grid-cols-2 gap-4 md:grid-cols-3"
		aria-label={m.overview_kpi_label()}
	>
		<KpiTile
			label={m.overview_kpi_searches()}
			heroValue={searchesKpi.hasGlobal
				? `${searchesKpi.used} / ${searchesKpi.cap}`
				: `${searchesKpi.used}`}
			subtitle={searchesKpi.resetsAt
				? m.overview_kpi_resets({ when: formatWhen(searchesKpi.resetsAt, getLocale()) })
				: searchesKpi.hasGlobal
					? m.stamina_used({ used: searchesKpi.used, cap: searchesKpi.cap })
					: m.overview_kpi_no_global_cap()}
			status={searchesKpi.status}
			statusLabel={searchesKpi.level === 'spent'
				? m.stamina_spent()
				: searchesKpi.level === 'tired'
					? m.stamina_tired()
					: m.stamina_fresh()}
			accentClass={searchesKpi.status === 'danger'
				? 'text-danger'
				: searchesKpi.status === 'warn'
					? 'text-warning'
					: 'text-brand'}
			loading={caps.isPending}
			error={caps.error}
		/>
		<KpiTile
			label={m.overview_kpi_instances()}
			heroValue={`${fleetKpi.healthy} / ${fleetKpi.total}`}
			subtitle={fleetKpi.failing > 0
				? m.overview_kpi_failing_count({ count: fleetKpi.failing })
				: fleetKpi.total === 0
					? m.empty_instances()
					: m.overview_kpi_all_healthy()}
			status={fleetKpi.status}
			statusLabel={fleetKpi.failing > 0
				? m.overview_kpi_failing_count({ count: fleetKpi.failing })
				: m.overview_kpi_all_healthy()}
			accentClass={fleetKpi.status === 'danger'
				? 'text-danger'
				: fleetKpi.status === 'warn'
					? 'text-warning'
					: 'text-success'}
			loading={instances.isPending}
			error={instances.error}
		/>
		<div class="col-span-2 min-w-0 md:col-span-1">
			<KpiTile
				label={m.overview_kpi_prowlarr()}
				heroValue={prowlarrKpi.total > 0 ? `${prowlarrKpi.headroom} / ${prowlarrKpi.total}` : '0'}
				subtitle={prowlarrKpi.total > 0
					? m.prowlarr_exhausted_count({ count: prowlarrKpi.exhausted })
					: m.overview_kpi_prowlarr_unlinked()}
				status={prowlarrKpi.status}
				statusLabel={prowlarrKpi.exhausted > 0
					? m.prowlarr_exhausted_count({ count: prowlarrKpi.exhausted })
					: m.stamina_fresh()}
				accentClass={prowlarrKpi.status === 'danger'
					? 'text-danger'
					: prowlarrKpi.status === 'warn'
						? 'text-warning'
						: 'text-brand'}
				loading={prowlarr.isPending}
				error={prowlarr.error}
			/>
		</div>
	</section>

	<!-- Row 2: 12-col grid desktop, 2-col tablet, 1-col phone -->
	<div class="grid min-w-0 grid-cols-1 gap-6 md:grid-cols-2 xl:grid-cols-12">
		<!-- Left: 8 col desktop -->
		<section
			class="col-span-1 flex min-w-0 flex-col gap-4 md:col-span-1 xl:col-span-8"
			aria-labelledby="fleet-heading"
		>
			{#if instances.isPending}
				<div data-testid="skeleton" class="card flex flex-col gap-3 p-4" aria-busy="true">
					<div class="bg-muted h-6 w-32 animate-pulse rounded"></div>
					<div class="bg-muted h-10 w-full animate-pulse rounded"></div>
					<div class="bg-muted h-10 w-full animate-pulse rounded"></div>
				</div>
			{:else if instances.isError}
				<ProblemNote error={instances.error} />
			{:else if (instances.data ?? []).length === 0}
				<EmptyFleetOnboarding />
			{:else}
				<div class="card max-w-full min-w-0 overflow-hidden p-0">
					<div class="flex items-center justify-between border-b border-line px-4 py-3">
						<h2 id="fleet-heading" class="text-base font-semibold">
							{m.overview_fleet_title()}
						</h2>
						<a href={resolve('/instances')} class="text-brand text-xs hover:underline">
							{m.nav_instances()}
						</a>
					</div>
					<div class="relative w-full overflow-x-auto">
						<table class="compact-table table w-full">
							<caption class="sr-only">{m.overview_fleet_title()}</caption>
							<thead>
								<tr>
									<th scope="col" class="w-10 text-center">
										<span class="sr-only">{m.schedule_status()}</span>
									</th>
									<th scope="col">{m.instance_name()}</th>
									<th scope="col">{m.instance_kind()}</th>
									<th scope="col" class="min-w-[8rem]">{m.stamina()}</th>
									<th scope="col">{m.overview_last_run()}</th>
									<th scope="col">{m.overview_next_window()}</th>
									<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
								</tr>
							</thead>
							<tbody>
								{#each instances.data ?? [] as inst (inst.id)}
									{@const instStatus = instanceStatus(inst)}
									{@const cap = capFor(inst, caps.data)}
									{@const lastRun = lastRunForInstance(runs.data, inst.id)}
									{@const nextSchedule = nextScheduleForInstance(schedules.data, inst.id)}
									<tr>
										<td class="w-10 text-center">
											<StatusIcon status={instStatus.status} label={instStatus.label} size={16} />
										</td>
										<td>
											<a
												href={resolve('/instances/[id]', { id: inst.id })}
												class="font-medium hover:underline"
											>
												{inst.name}
											</a>
											{#if inst.last_error}
												<div class="truncate text-xs text-danger" title={inst.last_error}>
													{inst.last_error}
												</div>
											{/if}
										</td>
										<td>
											<span
												class="inline-block rounded-full border border-line px-2 py-0.5 text-xs text-subtle"
											>
												{instanceKindLabel(inst.kind, inst.last_seen_product)}
											</span>
										</td>
										<td>
											{#if cap}
												<StaminaGauge label={inst.name} used={cap.used} cap={cap.cap} hideLabel />
											{:else}
												<span class="text-xs text-subtle">—</span>
											{/if}
										</td>
										<td class="text-xs whitespace-nowrap text-subtle">
											{lastRun
												? formatWhen(lastRun.started_at ?? lastRun.queued_at, getLocale())
												: '—'}
										</td>
										<td class="text-xs whitespace-nowrap text-subtle">
											{formatScheduleWindow(nextSchedule)}
										</td>
										<td>
											<div
												class="flex flex-col items-stretch gap-1.5 2xl:flex-row 2xl:items-center"
											>
												<ConfirmAction
													label={m.quickie_missing()}
													message={m.overview_confirm_missing({ name: inst.name })}
													groupLabel={m.quickie_missing()}
													pending={quickie.isPending}
													inline={true}
													onconfirm={() =>
														quickie.mutate({ id: inst.id, name: inst.name, kind: 'missing' })}
												/>
												<ConfirmAction
													label={m.quickie_upgrade()}
													message={m.overview_confirm_upgrade({ name: inst.name })}
													groupLabel={m.quickie_upgrade()}
													pending={quickie.isPending}
													inline={true}
													onconfirm={() =>
														quickie.mutate({ id: inst.id, name: inst.name, kind: 'upgrade' })}
												/>
											</div>
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			{/if}
		</section>

		<!-- Right: 4 col desktop -->
		<div class="col-span-1 flex min-w-0 flex-col gap-6 md:col-span-1 xl:col-span-4">
			<!-- Recent activity -->
			<section
				class="card flex min-w-0 flex-col gap-3 p-4"
				aria-labelledby="overview-activity-title"
			>
				<div class="flex items-center justify-between">
					<h2 id="overview-activity-title" class="text-base font-semibold">
						{m.overview_recent_activity()}
					</h2>
					<a href={resolve('/activity')} class="text-brand text-xs hover:underline">
						{m.overview_view_all_activity()}
					</a>
				</div>
				{#if events.isPending}
					<div data-testid="skeleton" class="flex flex-col gap-2" aria-busy="true">
						<div class="bg-muted h-6 w-full animate-pulse rounded"></div>
						<div class="bg-muted h-6 w-full animate-pulse rounded"></div>
						<div class="bg-muted h-6 w-full animate-pulse rounded"></div>
					</div>
				{:else if events.isError}
					<ProblemNote error={events.error} />
				{:else if (events.data ?? []).length === 0}
					<p class="text-xs text-subtle">{m.overview_no_recent_events()}</p>
				{:else}
					<ul class="flex flex-col divide-y divide-line text-xs">
						{#each (events.data ?? []).slice(0, 10) as ev (ev.id)}
							<li class="flex items-center justify-between gap-2 py-2">
								<div class="flex min-w-0 items-center gap-2">
									<StatusIcon
										status={eventStatus(ev.level)}
										label={eventLevelLabel(ev.level)}
										size={14}
									/>
									<span class="truncate font-medium text-fg" title={ev.title ?? ev.type}>
										{ev.title ?? ev.type}
									</span>
								</div>
								<span class="shrink-0 whitespace-nowrap text-subtle">
									{formatWhen(ev.ts, getLocale())}
								</span>
							</li>
						{/each}
					</ul>
				{/if}
			</section>

			<!-- Schedules -->
			<section
				class="card flex min-w-0 flex-col gap-3 p-4"
				aria-labelledby="overview-schedule-title"
			>
				<div class="flex items-center justify-between">
					<h2 id="overview-schedule-title" class="text-base font-semibold">
						{m.automation_tab_schedules()}
					</h2>
					<a href={resolve('/automation')} class="text-brand text-xs hover:underline">
						{m.overview_manage_schedules()}
					</a>
				</div>
				{#if schedules.isPending}
					<div data-testid="skeleton" class="flex flex-col gap-2" aria-busy="true">
						<div class="bg-muted h-6 w-full animate-pulse rounded"></div>
					</div>
				{:else if schedules.isError}
					<ProblemNote error={schedules.error} />
				{:else if (schedules.data ?? []).length === 0}
					<p class="text-xs text-subtle">{m.schedules_empty()}</p>
				{:else}
					<ul class="flex flex-col divide-y divide-line text-xs">
						{#each (schedules.data ?? []).filter((s) => s.enabled) as s (s.id)}
							{@const pauseText = activePauseText(s, instances.data)}
							<li class="flex items-center justify-between gap-2 py-1.5">
								<div class="flex min-w-0 flex-col">
									<span class="truncate font-medium text-fg">{s.name}</span>
									<span class="text-subtle">{s.start}–{s.end} {s.tz}</span>
								</div>
								{#if pauseText}
									<span
										class="bg-brand/10 text-brand shrink-0 rounded px-1.5 py-0.5 text-xs font-semibold"
									>
										{pauseText}
									</span>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</section>

			<!-- Prowlarr meters -->
			{#if prowlarr.isPending}
				<div data-testid="skeleton" class="card flex min-w-0 flex-col gap-2 p-4" aria-busy="true">
					<div class="bg-muted h-5 w-32 animate-pulse rounded"></div>
					<div class="bg-muted h-4 w-full animate-pulse rounded"></div>
				</div>
			{:else if prowlarr.isError}
				<div class="card min-w-0 p-4">
					<ProblemNote error={prowlarr.error} />
				</div>
			{:else if (prowlarr.data ?? []).length > 0}
				<section
					class="card flex min-w-0 flex-col gap-3 p-4"
					aria-labelledby="dashboard-prowlarr-title"
				>
					<div class="flex items-center justify-between">
						<h2 id="dashboard-prowlarr-title" class="text-base font-semibold">
							<a href={resolve('/connections/prowlarr')} class="hover:underline">
								{m.prowlarr_dashboard_title()}
							</a>
						</h2>
						<a href={resolve('/connections/prowlarr')} class="text-brand text-xs hover:underline">
							{m.overview_view_prowlarr()}
						</a>
					</div>
					<ul class="flex flex-col gap-3">
						{#each prowlarr.data ?? [] as link (link.link_id)}
							{@const total = link.indexers.length}
							{@const exhausted = countExhausted(link.indexers)}
							{@const headroom = countHeadroom(link.indexers)}
							<li class="flex flex-col gap-1">
								<div
									class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-sm"
									data-label-value
								>
									<span class="font-medium">{link.name}</span>
									<span class="text-subtle">
										{m.prowlarr_headroom_count({ count: headroom, total })} · {m.prowlarr_exhausted_count(
											{ count: exhausted }
										)}
									</span>
								</div>
								<div
									role="meter"
									aria-label={m.prowlarr_headroom_label({ name: link.name })}
									aria-valuemin={0}
									aria-valuemax={total}
									aria-valuenow={headroom}
									aria-valuetext={`${headroom} / ${total}`}
									class="h-2 w-full overflow-hidden rounded-full border border-line bg-surface"
								>
									<div
										class="h-full rounded-full"
										style:background="var(--color-brand)"
										style:width={`${total > 0 ? Math.round((headroom / total) * 100) : 0}%`}
									></div>
								</div>
							</li>
						{/each}
					</ul>
				</section>
			{/if}
		</div>
	</div>
</div>

<style>
	.compact-table tbody tr {
		height: 2.75rem;
	}
	@media (min-width: 1280px) {
		.compact-table tbody tr {
			height: 2.25rem;
		}
		.compact-table :global(.btn) {
			min-height: 1.75rem;
			padding: 0.15rem 0.5rem;
			font-size: 0.75rem;
		}
	}
	.compact-table th,
	.compact-table td {
		vertical-align: middle;
	}
</style>
