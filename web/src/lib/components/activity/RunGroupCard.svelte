<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import type { ActivityGroup } from '#lib/activity.js';
	import LinkText from '#lib/components/LinkText.svelte';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { formatRelativeTime, formatWhen } from '#lib/stamina.js';

	interface Props {
		group: ActivityGroup;
		instanceName?: string;
		expanded?: boolean;
		ontoggle?: () => void;
	}

	const { group, instanceName, expanded = false, ontoggle }: Props = $props();

	let internalExpanded = $state(false);
	const isExpanded = $derived(ontoggle ? expanded : internalExpanded);

	let expandedEvtKey = $state<string | null>(null);

	function toggleGroup(): void {
		if (ontoggle) {
			ontoggle();
		} else {
			internalExpanded = !internalExpanded;
		}
	}

	function toggleRow(key: string): void {
		expandedEvtKey = expandedEvtKey === key ? null : key;
	}
</script>

<li
	class="card flex min-h-[2.75rem] flex-col justify-center gap-1.5 rounded-md border border-line px-3 py-1.5 md:min-h-[2.25rem]"
>
	<div class="flex w-full flex-wrap items-center gap-2">
		<StatusIcon status={group.status} label={group.level} size={16} />
		<time
			datetime={group.ts}
			title={formatWhen(group.ts, getLocale())}
			class="font-mono text-xs whitespace-nowrap text-subtle"
		>
			{formatRelativeTime(group.ts, getLocale())}
		</time>
		{#if group.instance_id}
			<a
				href={resolve('/instances/[id]', { id: group.instance_id })}
				class="rounded border border-line px-1.5 py-0.5 text-xs text-subtle hover:underline"
			>
				{instanceName ?? group.instance_id.slice(0, 8)}
			</a>
		{/if}
		<button
			type="button"
			aria-expanded={isExpanded}
			class="focus:ring-brand flex flex-1 cursor-pointer flex-wrap items-center gap-2 rounded text-left select-none focus:ring-1 focus:outline-none"
			onclick={toggleGroup}
		>
			<span class="text-xs font-semibold md:text-sm">
				{#if group.run_id}
					Run {group.run_id.slice(0, 8)}
				{:else}
					{group.type}
				{/if}
			</span>
			<span
				class="rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-xs text-subtle"
			>
				{m.activity_run_summary({
					dispatches: group.dispatches,
					items: group.itemCount
				})}
			</span>
			<span class="ml-auto text-xs text-subtle" aria-hidden="true">
				{isExpanded ? '▲' : '▼'}
			</span>
		</button>
	</div>

	{#if isExpanded}
		<ol class="mt-2 flex flex-col gap-1 border-t border-l-2 border-line border-l-line pt-2 pl-3">
			{#each group.events as evt, idx (`${group.id}-${idx}-${evt.id ?? ''}`)}
				{@const evtKey = `${group.id}-${idx}`}
				<li class="flex flex-col gap-1 py-1">
					<button
						type="button"
						aria-expanded={expandedEvtKey === evtKey}
						class="flex cursor-pointer flex-wrap items-center gap-2 text-left text-xs"
						onclick={() => toggleRow(evtKey)}
					>
						<span class="font-mono text-subtle">{evt.ts?.slice(11, 19) ?? ''}</span>
						<span class="text-brand font-medium">{evt.type ?? evt.event}</span>
						<LinkText class="min-w-0 [overflow-wrap:anywhere] break-words" text={evt.title ?? ''} />
						{#if evt.detail}
							<LinkText
								class="min-w-0 [overflow-wrap:anywhere] break-words text-subtle"
								text={evt.detail}
							/>
						{/if}
					</button>
					{#if expandedEvtKey === evtKey}
						<pre
							class="max-w-full overflow-x-auto rounded bg-surface p-2 font-mono text-xs">{JSON.stringify(
								evt,
								null,
								2
							)}</pre>
					{/if}
				</li>
			{/each}
		</ol>
	{/if}
</li>
