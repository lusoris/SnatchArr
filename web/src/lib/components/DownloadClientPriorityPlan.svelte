<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Dialog from '@sveltesentio/ui/dialog/component';
	import type { components } from '#lib/api/schema.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { downloadClientPriorityPlanQuery } from '#lib/queries/connections.js';
	import { formatWhen } from '#lib/stamina.js';
	import ProblemNote from './ProblemNote.svelte';

	interface Props {
		client: components['schemas']['DownloadClient'];
		open?: boolean;
	}

	let { client, open = $bindable(false) }: Props = $props();
	const dialogId = $props.id();
	const plan = downloadClientPriorityPlanQuery(
		() => client.id,
		() => open
	);
</script>

<Dialog
	bind:open
	title={m.dlclients_priority_plan_title({ name: client.name })}
	idBase={dialogId}
	class="max-h-[85dvh] grid-cols-[minmax(0,1fr)] overflow-y-auto border border-line bg-surface p-6"
>
	<div class="flex flex-col gap-4 py-4">
		{#if plan.isPending}
			<p aria-live="polite">{m.loading()}</p>
		{:else if plan.isError}
			<ProblemNote error={plan.error} />
		{:else if plan.data}
			{@const data = plan.data}
			<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2">
				<dt>{m.dlclients_priority_mode()}</dt>
				<dd>{data.mode === 'act' ? m.queue_rule_mode_act() : m.queue_rule_mode_observe()}</dd>
				<dt>{m.dlclients_priority_observed()}</dt>
				<dd><time datetime={data.observedAt}>{formatWhen(data.observedAt, getLocale())}</time></dd>
			</dl>
			<p>{m.dlclients_priority_unattributed({ count: data.unattributed })}</p>
			{#if data.queueingDisabled}
				<p class="rounded-md border border-line p-3">{m.dlclients_priority_queueing_disabled()}</p>
			{/if}
			{#if data.observeRowNames.length > 0}
				<p class="rounded-md border border-line p-3">
					{m.dlclients_priority_observe_rows({ names: data.observeRowNames.join(', ') })}
				</p>
			{/if}
			{#if data.priority.length + data.order.length + data.pause.length + data.resume.length === 0}
				<p>{m.dlclients_priority_empty()}</p>
			{/if}
			{#if data.priority.length > 0}
				<section>
					<h3 class="font-semibold">{m.dlclients_priority_moves()}</h3>
					<ul aria-label={m.dlclients_priority_moves()} class="list-disc space-y-1 pl-5">
						{#each data.priority as move (move.id)}
							<li class="break-all">{m.dlclients_priority_move(move)}</li>
						{/each}
					</ul>
				</section>
			{/if}
			{#if data.order.length > 0}
				<section>
					<h3 class="font-semibold">{m.dlclients_priority_order()}</h3>
					<ol aria-label={m.dlclients_priority_order()} class="list-decimal space-y-1 pl-5">
						{#each data.order as id (id)}
							<li class="break-all">{id}</li>
						{/each}
					</ol>
				</section>
			{/if}
			{#if data.pause.length > 0}
				<section>
					<h3 class="font-semibold">{m.dlclients_priority_pause()}</h3>
					<ul aria-label={m.dlclients_priority_pause()} class="list-disc space-y-1 pl-5">
						{#each data.pause as id (id)}
							<li class="break-all">{id}</li>
						{/each}
					</ul>
				</section>
			{/if}
			{#if data.resume.length > 0}
				<section>
					<h3 class="font-semibold">{m.dlclients_priority_resume()}</h3>
					<ul aria-label={m.dlclients_priority_resume()} class="list-disc space-y-1 pl-5">
						{#each data.resume as id (id)}
							<li class="break-all">{id}</li>
						{/each}
					</ul>
				</section>
			{/if}
		{:else}
			<p>{m.dlclients_priority_unobserved()}</p>
		{/if}
		{#if client.kind === 'qbittorrent'}
			<p class="text-subtle">{m.dlclients_priority_qbittorrent_limit()}</p>
		{/if}
	</div>
	{#snippet footer()}
		<button type="button" class="btn" onclick={() => (open = false)}>{m.action_close()}</button>
	{/snippet}
</Dialog>
