<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import { createSentioQuery } from '@sveltesentio/query';
	import { useSSE } from '@sveltesentio/realtime/use-sse';
	import { api } from '#lib/api/client.js';
	import ConnectionStatus from '#lib/components/ConnectionStatus.svelte';
	import LinkText from '#lib/components/LinkText.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { LIVE_EVENTS } from '#lib/live-events.js';
	import {
		eventToLiveFrame,
		levelTokenClasses,
		mergeLiveFrames,
		parseFrame,
		type LiveFrame
	} from '#lib/live.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { systemStatusQuery } from '#lib/queries/system.js';
	import { formatWhen } from '#lib/stamina.js';

	const sse = useSSE({
		url: '/api/v1/events/stream',
		withCredentials: true,
		events: LIVE_EVENTS,
		bufferMs: 100,
		historyLimit: 500,
		// The client calls its default timers with itself as receiver, which browsers
		// refuse, so it never reconnects (golusoris/sveltesentio#424); bound wrappers
		// avoid that until a fixed release is out.
		setTimeoutImpl: ((cb: TimerHandler, ms?: number) =>
			setTimeout(cb, ms)) as unknown as typeof setTimeout,
		clearTimeoutImpl: ((id?: number | NodeJS.Timeout) =>
			clearTimeout(id)) as unknown as typeof clearTimeout
	});

	let backlogFrames = $state<LiveFrame[]>([]);
	let backlogLoading = $state(true);
	let backlogError = $state<string | null>(null);

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	const schedules = createSentioQuery({
		queryKey: keys.schedules,
		queryFn: async () => (await api.GET('/schedules')).data ?? []
	});

	const systemStatus = systemStatusQuery();

	async function loadBacklog(): Promise<void> {
		try {
			const res = await api.GET('/events', { params: { query: { page_size: 100 } } });
			if (res.data) {
				const fetched = res.data.map(eventToLiveFrame);
				backlogFrames = mergeLiveFrames(fetched, backlogFrames, 500);
				backlogError = null;
			} else if (res.error) {
				backlogError = m.live_backlog_error();
			}
		} catch {
			backlogError = m.live_backlog_error();
		} finally {
			backlogLoading = false;
		}
	}

	onMount(() => {
		void loadBacklog();
	});

	let hasOpened = false;
	let hadError = false;

	$effect(() => {
		const state = sse.state;
		if (state === 'open') {
			if (hadError) {
				hadError = false;
				void loadBacklog();
			}
			hasOpened = true;
		} else if (hasOpened && (state === 'connecting' || sse.error)) {
			hadError = true;
		}
	});

	const streamFrames = $derived(
		sse.messages.map((msg) => parseFrame(msg.type, msg.data, msg.lastEventId))
	);

	const frames = $derived(mergeLiveFrames(streamFrames, backlogFrames, 500));

	function instanceName(id?: string): string | undefined {
		if (!id) return undefined;
		const match = (instances.data ?? []).find((i) => i.id === id);
		return match?.name ?? id.slice(0, 8);
	}

	function scheduleStatusForInstance(instanceId: string): string {
		const instSchedules = (schedules.data ?? []).filter(
			(s) => s.enabled && (!s.instance_id || s.instance_id === instanceId)
		);
		const activePause = instSchedules.find((s) => s.action === 'pause' && s.active);
		if (activePause) {
			return m.live_cycle_paused();
		}
		const activeWindow = instSchedules.find((s) => s.active);
		if (activeWindow) {
			return m.live_cycle_window({ window: activeWindow.name });
		}
		return m.live_cycle_continuous();
	}
</script>

<PageHeader title={m.live_title()}>
	{#snippet status()}
		<ConnectionStatus connected={sse.connected} />
	{/snippet}
</PageHeader>

{#if backlogError}
	<p role="alert" class="card mb-4 border-line text-sm text-danger">
		{backlogError}
	</p>
{/if}

{#if frames.length === 0}
	{#if backlogLoading}
		<div data-testid="live-skeleton" class="flex flex-col gap-2">
			<div class="h-10 w-full animate-pulse rounded border border-line bg-surface"></div>
			<div class="h-10 w-full animate-pulse rounded border border-line bg-surface"></div>
		</div>
	{:else}
		<div class="card flex flex-col gap-4">
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
										<span class="text-subtle">{scheduleStatusForInstance(inst.id)}</span>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				</div>
			</div>

			<div class="flex flex-wrap gap-2 pt-2">
				<a href={resolve('/history')} class="btn">{m.live_link_history()}</a>
				<a href={resolve('/schedules')} class="btn">{m.live_link_schedules()}</a>
			</div>
		</div>
	{/if}
{:else}
	<div role="log" aria-live="polite" aria-relevant="additions">
		<ol class="flex flex-col gap-1 font-mono text-sm">
			{#each frames as f, i (f.id ?? `frame-${i}-${f.ts ?? ''}-${f.event}`)}
				<li class="card flex flex-wrap items-center gap-2 px-3 py-1.5" data-level={f.level}>
					<span
						class="inline-block h-2 w-2 rounded-full {levelTokenClasses(f.level).dotClass}"
						aria-hidden="true"
					></span>
					<span class="text-subtle">{f.ts?.slice(11, 19) ?? ''}</span>
					<span class="font-semibold {levelTokenClasses(f.level).textClass}">{f.event}</span>
					{#if f.instance_id}
						<a
							href={resolve('/instances/[id]', { id: f.instance_id })}
							class="rounded border border-line px-1.5 py-0.5 text-xs text-subtle hover:underline"
						>
							{instanceName(f.instance_id)}
						</a>
					{/if}
					<LinkText
						class="min-w-0 font-medium [overflow-wrap:anywhere] break-words"
						text={f.title ?? ''}
					/>
					{#if f.detail}
						<LinkText
							class="min-w-0 [overflow-wrap:anywhere] break-words text-subtle"
							text={f.detail}
						/>
					{/if}
				</li>
			{/each}
		</ol>
	</div>
{/if}
