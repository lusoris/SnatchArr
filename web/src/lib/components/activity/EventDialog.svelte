<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Dialog from '@sveltesentio/ui/dialog/component';
	import type { components } from '#lib/api/schema.js';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import InstanceLink from '#lib/components/instance/InstanceLink.svelte';
	import RunDialog from './RunDialog.svelte';
	import { eventStatus, eventLevelLabel } from '#lib/overview.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { formatWhen } from '#lib/stamina.js';

	interface Props {
		event: components['schemas']['Event'];
		open?: boolean;
	}
	let { event, open = $bindable(false) }: Props = $props();
	let runOpen = $state(false);
	const dialogId = $props.id();
</script>

{#if runOpen && event.run_id}
	<RunDialog runId={event.run_id} bind:open={runOpen} />
{/if}
<Dialog
	title={m.event_detail_title()}
	idBase={dialogId}
	class="max-h-[85dvh] grid-cols-[minmax(0,1fr)] overflow-y-auto border border-line bg-surface p-6"
	bind:open={
		() => open && !runOpen,
		(value) => {
			if (!runOpen) open = value;
		}
	}
>
	<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 py-4">
		<dt>{m.activity_filter_type()}</dt>
		<dd class="break-words">{event.type}</dd>
		<dt>{m.event_title()}</dt>
		<dd class="break-words">{event.title ?? '–'}</dd>
		<dt>{m.activity_filter_level()}</dt>
		<dd class="flex items-center gap-2">
			<StatusIcon
				status={eventStatus(event.level)}
				label={eventLevelLabel(event.level)}
				size={16}
				showLabel
			/>
		</dd>
		<dt>{m.event_time()}</dt>
		<dd>{formatWhen(event.ts, getLocale()) || '–'}</dd>
		{#if event.instance_id}
			<dt>{m.activity_filter_instance()}</dt>
			<dd>
				<InstanceLink id={event.instance_id} />
			</dd>
		{/if}
		{#if event.run_id}
			<dt>{m.run_detail_id()}</dt>
			<dd>
				<button type="button" class="break-all hover:underline" onclick={() => (runOpen = true)}
					>{event.run_id}</button
				>
			</dd>
		{/if}
	</dl>
	<h3 class="mb-2 font-semibold">{m.activity_detail_json()}</h3>
	<pre class="max-w-full overflow-x-auto rounded bg-surface p-2 font-mono text-xs">{JSON.stringify(
			event,
			null,
			2
		)}</pre>
	{#snippet footer()}
		<button type="button" class="btn" onclick={() => (open = false)}>{m.action_close()}</button>
	{/snippet}
</Dialog>
