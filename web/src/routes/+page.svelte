<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import EventDialog from '#lib/components/activity/EventDialog.svelte';
	import RunDialog from '#lib/components/activity/RunDialog.svelte';
	import AppLogo from '#lib/components/AppLogo.svelte';
	import CardScroll from '#lib/components/CardScroll.svelte';
	import CardScrollHint from '#lib/components/CardScrollHint.svelte';
	import ConfirmAction from '#lib/components/ConfirmAction.svelte';
	import EmptyFleetOnboarding from '#lib/components/EmptyFleetOnboarding.svelte';
	import { entityHref, pageHref } from '#lib/entities.js';
	import { HELP_MAP } from '#lib/help.js';
	import KpiTile from '#lib/components/KpiTile.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import StaminaGauge from '#lib/components/StaminaGauge.svelte';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import {
		OVERVIEW_EVENTS_CAP,
		OVERVIEW_FLEET_CAP,
		OVERVIEW_PROWLARR_CAP,
		OVERVIEW_SCHEDULES_CAP,
		activePauseText,
		capRows,
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

	let selectedEvent = $state<Event | null>(null);
	let eventOpen = $state(false);
	let selectedRunId = $state<string | null>(null);
	let runOpen = $state(false);

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
	const fleetRows = $derived(capRows(instances.data, OVERVIEW_FLEET_CAP));
	const eventRows = $derived(capRows(events.data, OVERVIEW_EVENTS_CAP));
	const scheduleRows = $derived(
		capRows(
			(schedules.data ?? []).filter((s) => s.enabled),
			OVERVIEW_SCHEDULES_CAP
		)
	);
	const prowlarrRows = $derived(capRows(prowlarr.data, OVERVIEW_PROWLARR_CAP));
</script>

{#snippet seeAllLink(
	href: '/instances' | '/activity' | '/automation' | '/connections/prowlarr',
	label: string
)}
	<a href={pageHref(href)} class="text-brand text-xs hover:underline">
		{label}
	</a>
{/snippet}

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
	<div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-12">
		<!-- Left: 8 col desktop -->
		<section
			class="col-span-1 flex min-w-0 flex-col gap-4 xl:col-span-8"
			aria-labelledby="fleet-heading"
		>
			{#if instances.isPending}
				<div data-testid="skeleton" class="card flex flex-col gap-3 p-4" aria-busy="true">
					<div class="h-6 w-32 animate-pulse rounded bg-sunken"></div>
					<div class="h-10 w-full animate-pulse rounded bg-sunken"></div>
					<div class="h-10 w-full animate-pulse rounded bg-sunken"></div>
				</div>
			{:else if instances.isError}
				<ProblemNote error={instances.error} />
			{:else if fleetRows.total === 0}
				<EmptyFleetOnboarding />
			{:else}
				<div class="card max-w-full min-w-0 overflow-hidden p-0">
					<div class="flex items-center justify-between border-b border-line px-4 py-3">
						<div class="flex min-w-0 items-baseline gap-2">
							<h2 id="fleet-heading" class="text-base font-semibold">
								{m.overview_fleet_title()}
							</h2>
							<CardScrollHint hidden={fleetRows.hidden} />
						</div>
						{@render seeAllLink(
							'/instances',
							fleetRows.cut
								? m.overview_all_instances_count({ count: fleetRows.total })
								: m.nav_instances()
						)}
					</div>
					<CardScroll
						label={m.overview_fleet_title()}
						cap={fleetRows.cap}
						total={fleetRows.total}
						rowSelector="tbody > tr"
					>
						<table class="compact-table table-stack table w-full">
							<caption class="sr-only">{m.overview_fleet_title()}</caption>
							<thead class="bg-surface md:sticky md:top-0 md:z-10">
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
								{#each fleetRows.rows as inst (inst.id)}
									{@const instStatus = instanceStatus(inst)}
									{@const cap = capFor(inst, caps.data)}
									{@const lastRun = lastRunForInstance(runs.data, inst.id)}
									{@const nextSchedule = nextScheduleForInstance(schedules.data, inst.id)}
									<tr class="target">
										<td class="md:w-10 md:text-center" data-label={m.schedule_status()}>
											<StatusIcon status={instStatus.status} label={instStatus.label} size={16} />
										</td>
										<td data-label={m.instance_name()}>
											<div class="flex items-center justify-end gap-2 md:justify-start">
												<AppLogo kind={inst.kind} size={18} />
												<div class="min-w-0 text-right md:text-left">
													<a
														href={entityHref('instance', inst.id)}
														class="target-link font-medium hover:underline"
													>
														{inst.name}
													</a>
													{#if inst.last_error}
														<div class="truncate text-xs text-danger" title={inst.last_error}>
															{inst.last_error}
														</div>
													{/if}
												</div>
											</div>
										</td>
										<td data-label={m.instance_kind()}>
											<span
												class="inline-block rounded-full border border-line px-2 py-0.5 text-xs text-subtle"
											>
												{instanceKindLabel(inst.kind, inst.last_seen_product)}
											</span>
										</td>
										<td data-label={m.stamina()}>
											{#if cap}
												<StaminaGauge label={inst.name} used={cap.used} cap={cap.cap} hideLabel />
											{:else}
												<span class="text-xs text-subtle">—</span>
											{/if}
										</td>
										<td
											class="text-xs whitespace-nowrap text-subtle"
											data-label={m.overview_last_run()}
										>
											{#if lastRun}
												<button
													type="button"
													class="hover:underline"
													onclick={() => {
														selectedRunId = lastRun.id;
														runOpen = true;
													}}
													>{formatWhen(
														lastRun.started_at ?? lastRun.queued_at,
														getLocale()
													)}</button
												>
											{:else}—{/if}
										</td>
										<td class="text-xs text-subtle" data-label={m.overview_next_window()}>
											{#if nextSchedule}<a href={pageHref('/automation')} class="hover:underline"
													>{formatScheduleWindow(nextSchedule)}</a
												>{:else}—{/if}
										</td>
										<td data-label="">
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
					</CardScroll>
				</div>
			{/if}
		</section>

		<!-- Right: 4 col desktop -->
		<div class="col-span-1 flex min-w-0 flex-col gap-6 xl:col-span-4">
			<!-- Recent activity -->
			<section
				class="card flex min-w-0 flex-col gap-3 p-4"
				aria-labelledby="overview-activity-title"
			>
				<div class="flex items-center justify-between">
					<div class="flex min-w-0 items-baseline gap-2">
						<h2 id="overview-activity-title" class="text-base font-semibold">
							{m.overview_recent_activity()}
						</h2>
						<CardScrollHint hidden={eventRows.hidden} />
					</div>
					{@render seeAllLink('/activity', m.overview_view_all_activity())}
				</div>
				{#if events.isPending}
					<div data-testid="skeleton" class="flex flex-col gap-2" aria-busy="true">
						<div class="h-6 w-full animate-pulse rounded bg-sunken"></div>
						<div class="h-6 w-full animate-pulse rounded bg-sunken"></div>
						<div class="h-6 w-full animate-pulse rounded bg-sunken"></div>
					</div>
				{:else if events.isError}
					<ProblemNote error={events.error} />
				{:else if eventRows.total === 0}
					<p class="text-xs text-subtle">{m.overview_no_recent_events()}</p>
				{:else}
					<CardScroll
						label={m.overview_recent_activity()}
						cap={eventRows.cap}
						total={eventRows.total}
						rowSelector="li"
					>
						<ul class="flex flex-col divide-y divide-line text-xs">
							{#each eventRows.rows as ev (ev.id)}
								<li>
									<button
										type="button"
										data-testid="dashboard-event"
										data-event-type={ev.type}
										class="flex w-full items-center justify-between gap-2 py-2 text-left hover:bg-surface"
										onclick={() => {
											selectedEvent = ev;
											eventOpen = true;
										}}
									>
										<span class="flex min-w-0 items-center gap-2">
											<StatusIcon
												status={eventStatus(ev.level)}
												label={eventLevelLabel(ev.level)}
												size={14}
											/>
											<span class="truncate font-medium text-fg" title={ev.title ?? ev.type}>
												{ev.title ?? ev.type}
											</span>
										</span>
										<span class="shrink-0 whitespace-nowrap text-subtle">
											{formatWhen(ev.ts, getLocale())}
										</span>
									</button>
								</li>
							{/each}
						</ul>
					</CardScroll>
				{/if}
			</section>

			<!-- Schedules -->
			<section
				class="card flex min-w-0 flex-col gap-3 p-4"
				aria-labelledby="overview-schedule-title"
			>
				<div class="flex items-center justify-between">
					<div class="flex min-w-0 items-baseline gap-2">
						<h2 id="overview-schedule-title" class="text-base font-semibold">
							{m.automation_tab_schedules()}
						</h2>
						<CardScrollHint hidden={scheduleRows.hidden} />
					</div>
					{@render seeAllLink(
						'/automation',
						scheduleRows.cut
							? m.overview_all_schedules_count({ count: scheduleRows.total })
							: m.overview_manage_schedules()
					)}
				</div>
				{#if schedules.isPending}
					<div data-testid="skeleton" class="flex flex-col gap-2" aria-busy="true">
						<div class="h-6 w-full animate-pulse rounded bg-sunken"></div>
					</div>
				{:else if schedules.isError}
					<ProblemNote error={schedules.error} />
				{:else if scheduleRows.total === 0}
					<p class="text-xs text-subtle">{m.schedules_empty()}</p>
				{:else}
					<CardScroll
						label={m.automation_tab_schedules()}
						cap={scheduleRows.cap}
						total={scheduleRows.total}
						rowSelector="li"
					>
						<ul class="flex flex-col divide-y divide-line text-xs">
							{#each scheduleRows.rows as s (s.id)}
								{@const pauseText = activePauseText(s, instances.data)}
								<li class="flex items-center justify-between gap-2 py-1.5">
									<div class="flex min-w-0 flex-col">
										<a
											href={pageHref('/automation')}
											class="truncate font-medium text-fg hover:underline">{s.name}</a
										>
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
					</CardScroll>
				{/if}
			</section>

			<!-- Prowlarr meters -->
			{#if prowlarr.isPending}
				<div data-testid="skeleton" class="card flex min-w-0 flex-col gap-2 p-4" aria-busy="true">
					<div class="h-5 w-32 animate-pulse rounded bg-sunken"></div>
					<div class="h-4 w-full animate-pulse rounded bg-sunken"></div>
				</div>
			{:else if prowlarr.isError}
				<div class="card min-w-0 p-4">
					<ProblemNote error={prowlarr.error} />
				</div>
			{:else if prowlarrRows.total > 0}
				<section
					class="card flex min-w-0 flex-col gap-3 p-4"
					aria-labelledby="dashboard-prowlarr-title"
				>
					<div class="flex items-center justify-between">
						<div class="flex min-w-0 items-baseline gap-2">
							<h2 id="dashboard-prowlarr-title" class="text-base font-semibold">
								<a href={pageHref('/connections/prowlarr')} class="hover:underline">
									{m.prowlarr_dashboard_title()}
								</a>
							</h2>
							<CardScrollHint hidden={prowlarrRows.hidden} />
						</div>
						{@render seeAllLink(
							'/connections/prowlarr',
							prowlarrRows.cut
								? m.overview_all_prowlarr_count({ count: prowlarrRows.total })
								: m.overview_view_prowlarr()
						)}
					</div>
					<CardScroll
						label={m.prowlarr_dashboard_title()}
						cap={prowlarrRows.cap}
						total={prowlarrRows.total}
						rowSelector="li"
					>
						<ul class="flex flex-col gap-3">
							{#each prowlarrRows.rows as link (link.link_id)}
								{@const total = link.indexers.length}
								{@const exhausted = countExhausted(link.indexers)}
								{@const headroom = countHeadroom(link.indexers)}
								<li class="flex flex-col gap-1">
									<div
										class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-sm"
										data-label-value
									>
										<a href={pageHref('/connections/prowlarr')} class="font-medium hover:underline"
											>{link.name}</a
										>
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
					</CardScroll>
				</section>
			{/if}
		</div>
	</div>
</div>

{#if eventOpen && selectedEvent}
	<EventDialog event={selectedEvent} bind:open={eventOpen} />
{/if}
{#if runOpen && selectedRunId}
	<RunDialog runId={selectedRunId} bind:open={runOpen} />
{/if}
