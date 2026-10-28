<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import type { ResolvedPathname } from '$app/types';
	import { createSentioQuery } from '@sveltesentio/query';
	import { useSSE } from '@sveltesentio/realtime/use-sse';
	import {
		createTimerOptions,
		groupActivityEvents,
		matchesActivityFilters,
		resolveScheduleState,
		type ActivityFilter,
		type ActivityGroup
	} from '#lib/activity.js';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ActivityFilters from '#lib/components/activity/ActivityFilters.svelte';
	import EventRow from '#lib/components/activity/EventRow.svelte';
	import LogList from '#lib/components/activity/LogList.svelte';
	import NewEventsPill from '#lib/components/activity/NewEventsPill.svelte';
	import RunGroupCard from '#lib/components/activity/RunGroupCard.svelte';
	import ConnectionStatus from '#lib/components/ConnectionStatus.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import Pause from '@lucide/svelte/icons/pause';
	import { LIVE_EVENTS } from '#lib/live-events.js';
	import { eventToLiveFrame, mergeLiveFrames, parseFrame, type LiveFrame } from '#lib/live.js';
	import {
		mergeLogs,
		parseLogFrame,
		type LogFilter,
		type LogLevel,
		type LogRecord
	} from '#lib/logs.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { systemStatusQuery } from '#lib/queries/system.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import { formatWhen } from '#lib/stamina.js';

	type EventItem = components['schemas']['Event'];
	type LogPage = components['schemas']['LogPage'];
	type FeedSource = 'events' | 'log';

	interface Props {
		fixedInstanceId?: string;
	}
	const { fixedInstanceId }: Props = $props();

	const rawSource = $derived(page.url.searchParams.get('source'));
	const activeSource = $derived<FeedSource>(
		rawSource === 'log' && session.isAdmin ? 'log' : 'events'
	);
	const effectiveInstance = $derived(
		fixedInstanceId ?? page.url.searchParams.get('instance') ?? ''
	);
	const filterLevel = $derived(page.url.searchParams.get('level') ?? '');
	const filterType = $derived(page.url.searchParams.get('type') ?? '');
	const filterFrom = $derived(page.url.searchParams.get('from') ?? '');
	const filterTo = $derived(page.url.searchParams.get('to') ?? '');
	const filterQ = $derived(page.url.searchParams.get('q') ?? '');

	let followLive = $state(true);
	let unseenCount = $state(0);
	let scrollContainer = $state<HTMLElement | null>(null);

	function updateFilter(updates: Record<string, string | null | undefined>, replace = false): void {
		const newUrl = new URL(page.url.href);
		for (const [k, v] of Object.entries(updates)) {
			if (v !== undefined && v !== null && v !== '') newUrl.searchParams.set(k, v);
			else newUrl.searchParams.delete(k);
		}
		const basePath = fixedInstanceId
			? resolve('/instances/[id]/activity', { id: fixedInstanceId })
			: resolve('/activity');
		void goto(`${basePath}${newUrl.search}` as ResolvedPathname, {
			replaceState: replace,
			reset: false
		});
	}

	function selectSource(source: FeedSource): void {
		if (source !== activeSource)
			updateFilter({ source: source === 'events' ? null : 'log' }, false);
	}

	function clearFilters(): void {
		updateFilter({ level: null, type: null, from: null, to: null, q: null, instance: null }, false);
	}

	const hasActiveFilters = $derived(
		Boolean(
			filterLevel ||
			filterType ||
			filterFrom ||
			filterTo ||
			filterQ ||
			(effectiveInstance && !fixedInstanceId)
		)
	);

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});
	const schedules = createSentioQuery({
		queryKey: keys.schedules,
		queryFn: async () => (await api.GET('/schedules')).data ?? []
	});
	const systemStatus = systemStatusQuery();

	function instanceName(id?: string): string | undefined {
		if (!id) return undefined;
		return (instances.data ?? []).find((i) => i.id === id)?.name ?? id.slice(0, 8);
	}

	function scheduleStatusText(instanceId: string): string {
		const state = resolveScheduleState(schedules.data ?? [], instanceId);
		if (state.kind === 'paused') return m.live_cycle_paused();
		if (state.kind === 'window') return m.live_cycle_window({ window: state.name });
		return m.live_cycle_continuous();
	}

	const eventsSSE = useSSE({
		url: '/api/v1/events/stream',
		withCredentials: true,
		events: LIVE_EVENTS,
		bufferMs: 100,
		historyLimit: 500,
		...createTimerOptions()
	});
	let backlogFrames = $state<LiveFrame[]>([]);

	const eventsQuery = createSentioQuery<EventItem[]>(() => ({
		queryKey: ['events', effectiveInstance, filterType],
		queryFn: async () => {
			const res = await api.GET('/events', {
				params: {
					query: {
						page_size: 100,
						instance_id: effectiveInstance || undefined,
						type: filterType || undefined
					}
				}
			});
			if (res.error || !res.data) throw res.error ?? new Error('Failed to load events');
			return res.data;
		},
		enabled: activeSource === 'events'
	}));

	$effect(() => {
		if (eventsQuery.data) {
			backlogFrames = mergeLiveFrames(
				eventsQuery.data.map(eventToLiveFrame),
				untrack(() => backlogFrames),
				500
			);
		}
	});

	let hasOpened = false;
	let hadError = false;
	$effect(() => {
		const state = eventsSSE.state;
		if (state === 'open') {
			if (hadError) {
				hadError = false;
				void eventsQuery.refetch();
			}
			hasOpened = true;
		} else if (hasOpened && (state === 'connecting' || eventsSSE.error)) {
			hadError = true;
		}
	});

	const streamFrames = $derived(
		eventsSSE.messages.map((m) => parseFrame(m.type, m.data, m.lastEventId))
	);
	const allMergedFrames = $derived(mergeLiveFrames(streamFrames, backlogFrames, 500));
	const activeEventsFilter: ActivityFilter = $derived({
		instance: effectiveInstance,
		level: filterLevel,
		type: filterType,
		from: filterFrom,
		to: filterTo,
		q: filterQ
	});
	const filteredEvents = $derived(
		allMergedFrames.filter((f) => matchesActivityFilters(f, activeEventsFilter))
	);
	const eventGroups: ActivityGroup[] = $derived(groupActivityEvents([...filteredEvents].reverse()));

	const logsQuery = createSentioQuery<LogPage>(() => ({
		queryKey: keys.logs(filterLevel || 'info', filterQ, ''),
		queryFn: async () => {
			const res = await api.GET('/logs', {
				params: {
					query: {
						level: (filterLevel as LogLevel) || 'info',
						q: filterQ || undefined,
						limit: 500
					}
				}
			});
			if (res.error || !res.data) throw res.error ?? new Error('Failed to load logs');
			return res.data;
		},
		enabled: session.isAdmin && activeSource === 'log'
	}));

	const logsSSE = useSSE({
		url: '/api/v1/logs/stream',
		withCredentials: true,
		autoConnect: false,
		bufferMs: 250,
		historyLimit: 500,
		...createTimerOptions()
	});
	$effect(() => {
		if (session.isAdmin && activeSource === 'log') logsSSE.connect();
		else logsSSE.close();
	});
	const streamedLogs = $derived(
		logsSSE.messages
			.map((m) => parseLogFrame(m.data))
			.filter((r): r is LogRecord => r !== undefined)
	);
	const logFilterObj: LogFilter = $derived({
		level: (filterLevel as LogLevel) || 'info',
		q: filterQ,
		source: ''
	});
	const chronologicalLogs = $derived(
		[...mergeLogs(logsQuery.data?.records ?? [], streamedLogs, logFilterObj, 500)]
			.reverse()
			.filter((r) => {
				if (filterFrom && r.time < filterFrom) return false;
				if (filterTo && r.time > filterTo) return false;
				return true;
			})
	);

	let prevItemsCount = 0;
	$effect(() => {
		const currentCount = activeSource === 'events' ? eventGroups.length : chronologicalLogs.length;
		if (currentCount > prevItemsCount) {
			const added = currentCount - prevItemsCount;
			if (followLive) untrack(() => scrollToBottom());
			else unseenCount += added;
		}
		prevItemsCount = currentCount;
	});

	function scrollToBottom(): void {
		if (scrollContainer) scrollContainer.scrollTop = scrollContainer.scrollHeight;
	}
	function snapToBottom(): void {
		followLive = true;
		unseenCount = 0;
		scrollToBottom();
	}
	function handleScroll(): void {
		if (!scrollContainer) return;
		const isAtBottom =
			scrollContainer.scrollTop + scrollContainer.clientHeight >= scrollContainer.scrollHeight - 24;
		if (isAtBottom) {
			followLive = true;
			unseenCount = 0;
		} else if (followLive) followLive = false;
	}
	function toggleFollowLive(): void {
		if (!followLive) snapToBottom();
		else followLive = false;
	}
</script>

<div
	class="activity-feed flex w-full flex-col gap-3 {fixedInstanceId ? '' : 'md:min-h-0 md:flex-1'}"
>
	<div class="flex shrink-0 flex-wrap items-center justify-between gap-3">
		<div class="flex flex-wrap items-center gap-2">
			<div
				class="segmented-control inline-flex rounded-lg border border-line bg-surface p-1"
				role="tablist"
				aria-label={m.nav_activity()}
			>
				<button
					type="button"
					role="tab"
					id="tab-events"
					aria-selected={activeSource === 'events'}
					class="btn-segmented min-h-11 rounded-md px-3 py-1 text-xs font-medium transition md:min-h-0 {activeSource ===
					'events'
						? 'bg-card font-semibold shadow-xs'
						: 'hover:text-foreground text-subtle'}"
					onclick={() => selectSource('events')}>{m.activity_source_events()}</button
				>
				{#if session.isAdmin}
					<button
						type="button"
						role="tab"
						id="tab-log"
						aria-selected={activeSource === 'log'}
						class="btn-segmented min-h-11 rounded-md px-3 py-1 text-xs font-medium transition md:min-h-0 {activeSource ===
						'log'
							? 'bg-card font-semibold shadow-xs'
							: 'hover:text-foreground text-subtle'}"
						onclick={() => selectSource('log')}>{m.activity_source_log()}</button
					>
				{/if}
			</div>
			<button
				type="button"
				class="btn btn-sm inline-flex items-center gap-1.5 rounded border border-line font-medium {followLive
					? 'bg-surface font-semibold'
					: 'text-subtle'}"
				aria-pressed={followLive}
				onclick={toggleFollowLive}
			>
				{#if followLive}
					<span
						class="bg-success inline-block h-2 w-2 rounded-full motion-safe:animate-pulse"
						aria-hidden="true"
					></span>
				{:else}
					<Pause size={14} class="text-subtle" aria-hidden="true" />
				{/if}
				<span>{m.activity_follow_live()}</span>
			</button>
		</div>
		<ConnectionStatus
			connected={activeSource === 'log' ? logsSSE.connected : eventsSSE.connected}
		/>
	</div>

	<div class="shrink-0">
		<ActivityFilters
			{activeSource}
			{effectiveInstance}
			{fixedInstanceId}
			{filterLevel}
			{filterType}
			{filterFrom}
			{filterTo}
			{filterQ}
			instances={instances.data ?? []}
			{hasActiveFilters}
			onupdate={updateFilter}
			onclear={clearFilters}
		/>
	</div>

	<section
		aria-label={activeSource === 'log' ? m.logs_title() : m.nav_activity()}
		class="bg-card relative flex w-full flex-col overflow-hidden rounded-lg border border-line {fixedInstanceId
			? ''
			: 'md:min-h-0 md:flex-1'}"
	>
		<NewEventsPill count={unseenCount} onsnap={snapToBottom} />
		<div
			bind:this={scrollContainer}
			onscroll={handleScroll}
			class="feed-scroll-container overflow-y-auto p-2 pb-[calc(4rem+env(safe-area-inset-bottom,0px))] md:pb-2 {fixedInstanceId
				? 'min-h-[20rem] md:h-[16rem] md:min-h-0'
				: 'h-[max(24rem,calc(100dvh-24.5rem))] md:h-full md:min-h-0 md:flex-1'}"
			role="log"
			aria-live="polite"
			aria-relevant="additions"
		>
			{#if activeSource === 'events'}
				{#if eventsQuery.isError}
					<ProblemNote error={eventsQuery.error} />
				{:else if eventsQuery.isPending && backlogFrames.length === 0}
					<div data-testid="activity-skeleton" class="flex flex-col gap-2 p-2">
						<div class="h-9 w-full animate-pulse rounded border border-line bg-surface"></div>
						<div class="h-9 w-full animate-pulse rounded border border-line bg-surface"></div>
						<div class="h-9 w-full animate-pulse rounded border border-line bg-surface"></div>
					</div>
				{:else if allMergedFrames.length === 0}
					<div class="card flex flex-col gap-4 p-4">
						<div>
							<h2 class="text-lg font-semibold">{m.live_empty_title()}</h2>
							<p class="mt-1 text-sm text-subtle">{m.live_empty_desc()}</p>
						</div>
						<div class="grid gap-3 sm:grid-cols-2">
							<div class="rounded border border-line p-3">
								<h3 class="text-xs font-medium tracking-wider text-subtle uppercase">
									{m.live_worker_label()}
								</h3>
								<p class="mt-1 text-sm">
									{#if systemStatus.data?.worker_last_contact_at}
										{m.live_worker_last_contact({
											when: formatWhen(systemStatus.data.worker_last_contact_at, getLocale())
										})}
									{:else}
										{m.live_worker_offline()}
									{/if}
								</p>
							</div>
							<div class="rounded border border-line p-3">
								<h3 class="text-xs font-medium tracking-wider text-subtle uppercase">
									{m.live_schedules_label()}
								</h3>
								<div class="mt-1 text-sm">
									{#if (instances.data ?? []).length === 0}
										<p>{m.live_cycle_continuous()}</p>
									{:else}
										<ul class="flex flex-col gap-1">
											{#each instances.data ?? [] as inst (inst.id)}
												<li class="flex items-center justify-between text-xs">
													<span class="font-medium">{inst.name}</span>
													<span class="text-subtle">{scheduleStatusText(inst.id)}</span>
												</li>
											{/each}
										</ul>
									{/if}
								</div>
							</div>
						</div>
						<div class="flex flex-wrap gap-2 pt-2">
							<a href={resolve('/automation')} class="btn">{m.live_link_schedules()}</a>
						</div>
					</div>
				{:else if eventGroups.length === 0}
					<div class="p-6 text-center">
						<h3 class="text-sm font-semibold">{m.activity_empty_title()}</h3>
						<p class="mt-1 text-xs text-subtle">{m.activity_empty_desc()}</p>
					</div>
				{:else}
					<ol class="flex w-full flex-col gap-1 text-sm">
						{#each eventGroups as group (group.id)}
							{#if group.isGroup}
								<RunGroupCard {group} instanceName={instanceName(group.instance_id)} />
							{:else}
								<EventRow
									event={group.events[0]}
									status={group.status}
									instanceName={instanceName(group.events[0].instance_id)}
								/>
							{/if}
						{/each}
					</ol>
				{/if}
			{:else if activeSource === 'log'}
				{#if logsQuery.isError}
					<ProblemNote error={logsQuery.error} />
				{:else if logsQuery.isPending}
					<div data-testid="skeleton" class="flex flex-col gap-2 p-2">
						<div class="h-9 w-full animate-pulse rounded border border-line bg-surface"></div>
						<div class="h-9 w-full animate-pulse rounded border border-line bg-surface"></div>
						<div class="h-9 w-full animate-pulse rounded border border-line bg-surface"></div>
					</div>
				{:else}
					<LogList records={chronologicalLogs} />
				{/if}
			{/if}
		</div>
	</section>
</div>
