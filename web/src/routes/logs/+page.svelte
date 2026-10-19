<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { useSSE } from '@sveltesentio/realtime/use-sse';
	import { api } from '#lib/api/client.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import {
		logLevelLabel,
		logLevels,
		logSources,
		mergeLogs,
		parseLogFrame,
		type LogFilter,
		type LogRecord
	} from '#lib/logs.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import { formatWhen } from '#lib/stamina.js';

	const limit = 500;
	const filter = $state<LogFilter>({ level: 'info', q: '', source: '' });

	const page = createSentioQuery(() => ({
		queryKey: keys.logs(filter.level, filter.q, filter.source),
		queryFn: async () =>
			(
				await api.GET('/logs', {
					params: {
						query: {
							level: filter.level,
							q: filter.q || undefined,
							source: filter.source || undefined,
							limit
						}
					}
				})
			).data,
		enabled: session.isAdmin
	}));

	// The stream sends every new record; the filters apply here, as the API applies them.
	const sse = useSSE({
		url: '/api/v1/logs/stream',
		withCredentials: true,
		autoConnect: false,
		bufferMs: 250,
		historyLimit: limit
	});
	$effect(() => {
		if (session.isAdmin) sse.connect();
	});

	const streamed = $derived(
		sse.messages.map((msg) => parseLogFrame(msg.data)).filter((r) => r !== undefined)
	);
	const records: LogRecord[] = $derived(
		mergeLogs(page.data?.records ?? [], streamed, filter, limit)
	);
	const sources = $derived(logSources(records));
</script>

<div class="mb-4 flex items-center justify-between">
	<h1 class="text-2xl font-semibold">{m.logs_title()}</h1>
	{#if session.isAdmin}
		<p class="flex items-center gap-2 text-sm" role="status">
			<span
				class="inline-block h-3 w-3 rounded-full"
				class:bg-green-600={sse.connected}
				class:bg-amber-500={!sse.connected}
				aria-hidden="true"
			></span>
			{sse.connected ? m.live_connected() : m.live_disconnected()}
		</p>
	{/if}
</div>

{#if !session.isAdmin}
	<p role="alert">{m.logs_admin_only()}</p>
{:else}
	<form class="card mb-4 flex flex-wrap gap-4" onsubmit={(e) => e.preventDefault()}>
		<Field id="logs-level" label={m.logs_level()}>
			<select id="logs-level" class="select" bind:value={filter.level}>
				{#each logLevels as level (level)}
					<option value={level}>{logLevelLabel(level)}</option>
				{/each}
			</select>
		</Field>
		<Field id="logs-q" label={m.logs_search()}>
			<input id="logs-q" class="input" type="search" maxlength="200" bind:value={filter.q} />
		</Field>
		<Field id="logs-source" label={m.logs_source()}>
			<input
				id="logs-source"
				class="input"
				maxlength="160"
				list="logs-sources"
				placeholder={m.logs_source_all()}
				bind:value={filter.source}
			/>
			<datalist id="logs-sources">
				{#each sources as s (s)}
					<option value={s}></option>
				{/each}
			</datalist>
		</Field>
	</form>
	<p class="mb-2 text-sm text-subtle">{m.logs_redacted_note()}</p>

	{#if page.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if page.isError}
		<ProblemNote error={page.error} />
	{:else}
		<p class="mb-2 text-sm text-subtle">
			{m.logs_window({
				shown: records.length,
				capacity: page.data?.capacity ?? 0,
				dropped: page.data?.dropped ?? 0
			})}
		</p>
		{#if records.length === 0}
			<p>{m.logs_empty()}</p>
		{/if}
		<div role="log" aria-label={m.logs_title()} aria-live="polite" aria-relevant="additions">
			<ol class="flex flex-col gap-1 font-mono text-sm">
				{#each records as r (r.seq)}
					<li class="card px-3 py-1" data-level={r.level}>
						<span class="text-subtle">{formatWhen(r.time, getLocale())}</span>
						<span class="ml-2 font-semibold">{logLevelLabel(r.level)}</span>
						<span class="ml-2 text-subtle">{r.source}</span>
						<span class="ml-2 [overflow-wrap:anywhere] break-all">{r.message}</span>
						{#each r.attrs as a, i (i)}
							<span class="ml-2 text-subtle">{a.key}={a.value}</span>
						{/each}
					</li>
				{/each}
			</ol>
		</div>
	{/if}
{/if}
