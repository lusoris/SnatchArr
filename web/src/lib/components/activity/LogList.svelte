<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { logStatus } from '#lib/activity.js';
	import LinkText from '#lib/components/LinkText.svelte';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import { logLevelLabel, type LogRecord } from '#lib/logs.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { formatWhen } from '#lib/stamina.js';

	interface Props {
		records: readonly LogRecord[];
	}

	const { records }: Props = $props();

	let expandedSeq = $state<number | null>(null);

	function toggleRow(seq: number): void {
		expandedSeq = expandedSeq === seq ? null : seq;
	}
</script>

{#if records.length === 0}
	<div class="p-6 text-center">
		<p class="text-sm text-subtle">{m.logs_empty()}</p>
	</div>
{:else}
	<ol class="flex w-full flex-col gap-1 font-mono text-sm">
		{#each records as rec (rec.seq)}
			<li
				class="card flex min-h-[2.75rem] flex-col justify-center gap-1 rounded-md border border-line px-3 py-1.5 md:min-h-[2.25rem]"
				data-level={rec.level}
			>
				<div class="flex w-full flex-wrap items-center gap-2">
					<StatusIcon status={logStatus(rec.level)} label={logLevelLabel(rec.level)} size={16} />
					<span class="text-xs whitespace-nowrap text-subtle">
						{formatWhen(rec.time, getLocale())}
					</span>
					<button
						type="button"
						aria-expanded={expandedSeq === rec.seq}
						class="focus:ring-brand flex flex-1 cursor-pointer flex-wrap items-center gap-2 rounded text-left select-none focus:ring-1 focus:outline-none"
						onclick={() => toggleRow(rec.seq)}
					>
						<span class="text-xs font-semibold">{logLevelLabel(rec.level)}</span>
						<span class="text-xs [overflow-wrap:anywhere] break-words text-subtle">
							{rec.source}
						</span>
						<LinkText
							class="min-w-0 text-xs [overflow-wrap:anywhere] break-words md:text-sm"
							text={rec.message}
						/>
						{#each rec.attrs as attr, i (i)}
							<span class="text-xs [overflow-wrap:anywhere] break-words text-subtle">
								{attr.key}={attr.value}
							</span>
						{/each}
					</button>
				</div>
				{#if expandedSeq === rec.seq}
					<div class="mt-1 border-t border-line pt-1 text-xs">
						<pre
							class="max-w-full overflow-x-auto rounded bg-surface p-2 font-mono text-xs">{JSON.stringify(
								rec,
								null,
								2
							)}</pre>
					</div>
				{/if}
			</li>
		{/each}
	</ol>
{/if}
