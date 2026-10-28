<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import type { StatusKind } from '#lib/activity.js';
	import LinkText from '#lib/components/LinkText.svelte';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import { liveFrameToEvent, type LiveFrame } from '#lib/live.js';
	import EventDialog from './EventDialog.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { formatRelativeTime, formatWhen } from '#lib/stamina.js';

	interface Props {
		event: LiveFrame;
		status?: StatusKind;
		instanceName?: string;
		expanded?: boolean;
		ontoggle?: () => void;
	}

	const { event, status = 'info', instanceName, expanded = false, ontoggle }: Props = $props();

	let detailsOpen = $state(false);
	let internalExpanded = $state(false);

	const isExpanded = $derived(ontoggle ? expanded : internalExpanded);

	function toggle(): void {
		if (ontoggle) {
			ontoggle();
		} else {
			internalExpanded = !internalExpanded;
		}
	}

	function handleKeyDown(e: KeyboardEvent): void {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			toggle();
		}
	}
</script>

<li
	class="card flex min-h-[2.75rem] flex-col justify-center gap-1 rounded-md border border-line px-3 py-1.5 md:min-h-[2.25rem]"
	data-level={event.level}
>
	<div class="flex w-full flex-wrap items-center gap-2">
		<StatusIcon {status} label={event.level ?? 'info'} size={16} />
		<time
			datetime={event.ts}
			title={formatWhen(event.ts, getLocale())}
			class="font-mono text-xs whitespace-nowrap text-subtle"
		>
			{formatRelativeTime(event.ts, getLocale())}
		</time>
		{#if event.instance_id}
			<a
				href={resolve('/instances/[id]', { id: event.instance_id })}
				class="rounded border border-line px-1.5 py-0.5 text-xs text-subtle hover:underline"
			>
				{instanceName ?? event.instance_id.slice(0, 8)}
			</a>
		{/if}
		<button
			type="button"
			aria-expanded={isExpanded}
			class="focus:ring-brand flex flex-1 cursor-pointer flex-wrap items-center gap-2 rounded text-left select-none focus:ring-1 focus:outline-none"
			onclick={toggle}
			onkeydown={handleKeyDown}
		>
			<span class="text-brand text-xs font-semibold">{event.type ?? event.event}</span>
			<LinkText
				class="min-w-0 text-xs font-medium [overflow-wrap:anywhere] break-words md:text-sm"
				text={event.title ?? ''}
			/>
			{#if event.detail}
				<LinkText
					class="min-w-0 text-xs [overflow-wrap:anywhere] break-words text-subtle"
					text={event.detail}
				/>
			{/if}
		</button>
		<button
			type="button"
			class="text-brand text-xs hover:underline"
			onclick={() => (detailsOpen = true)}>{m.entity_details()}</button
		>
	</div>
	{#if isExpanded}
		<div class="mt-1 border-t border-line pt-1 text-xs">
			<pre
				class="max-w-full overflow-x-auto rounded bg-surface p-2 font-mono text-xs">{JSON.stringify(
					event,
					null,
					2
				)}</pre>
		</div>
	{/if}
</li>

{#if detailsOpen}
	<EventDialog event={liveFrameToEvent(event)} bind:open={detailsOpen} />
{/if}
