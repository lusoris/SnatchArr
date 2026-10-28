<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { createSentioQuery } from '@sveltesentio/query';
	import Dialog from '@sveltesentio/ui/dialog/component';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { runStatusLabel, snatchKindLabel } from '#lib/runs.js';
	import { formatWhen } from '#lib/stamina.js';

	interface Props {
		runId: string;
		open?: boolean;
	}
	let { runId, open = $bindable(false) }: Props = $props();
	const dialogId = $props.id();
	const run = createSentioQuery<components['schemas']['Run'] | null>(() => ({
		queryKey: keys.run(runId),
		queryFn: async () =>
			(await api.GET('/runs/{runId}', { params: { path: { runId } } })).data ?? null,
		enabled: open && !!runId
	}));
</script>

<Dialog
	bind:open
	title={m.run_detail_title()}
	idBase={dialogId}
	class="max-h-[85dvh] overflow-y-auto border border-line bg-surface p-6"
>
	{#if run.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if run.isError}
		<ProblemNote error={run.error} />
	{:else if run.data}
		{@const data = run.data}
		<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 py-4">
			<dt>{m.run_detail_id()}</dt>
			<dd class="font-mono break-all">{data.id}</dd>
			<dt>{m.run_status()}</dt>
			<dd>{runStatusLabel(data.status)}</dd>
			<dt>{m.run_kind()}</dt>
			<dd>{snatchKindLabel(data.kind)}</dd>
			<dt>{m.instance_name()}</dt>
			<dd>
				<a class="hover:underline" href={resolve('/instances/[id]', { id: data.instance_id })}
					>{data.instance_id}</a
				>
			</dd>
			<dt>{m.run_queued()}</dt>
			<dd>{formatWhen(data.queued_at, getLocale())}</dd>
			<dt>{m.run_started()}</dt>
			<dd>{formatWhen(data.started_at, getLocale()) || '–'}</dd>
			<dt>{m.run_finished()}</dt>
			<dd>{formatWhen(data.finished_at, getLocale()) || '–'}</dd>
			<dt>{m.run_searched()}</dt>
			<dd>{data.searched_count}</dd>
		</dl>
		{#if data.error}<ProblemNote error={data.error} />{/if}
	{/if}
	{#snippet footer()}
		<button type="button" class="btn" onclick={() => (open = false)}>{m.action_close()}</button>
	{/snippet}
</Dialog>
