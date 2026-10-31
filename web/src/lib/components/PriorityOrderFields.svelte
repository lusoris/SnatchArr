<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import { kindLabel } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		dropPriorityKind,
		movePriorityKind,
		priorityRows,
		ranksFromRows,
		tiePriorityKind,
		type PriorityRow
	} from '#lib/priority-order.js';

	type PriorityPresetMap = components['schemas']['PriorityPresetMap'];
	interface Props {
		preset: PriorityPresetMap;
		order: 'search' | 'download';
		disabled?: boolean;
	}
	let { preset = $bindable(), order, disabled = false }: Props = $props();
	let rows = $state<PriorityRow[]>([]);
	let announcement = $state('');
	let draggedKind = $state<string | null>(null);
	let dropTarget = $state<string | null>(null);
	let lastPreset: PriorityPresetMap | undefined;

	$effect(() => {
		if (preset !== lastPreset) {
			lastPreset = preset;
			rows = priorityRows(preset);
		}
	});

	function commit(next: PriorityRow[], kind: string): void {
		rows = next;
		preset = ranksFromRows(next);
		lastPreset = preset;
		announcement = m.settings_priority_moved({
			kind: kindLabel(kind),
			position: next.findIndex((row) => row.kind === kind) + 1,
			rank: preset[kind]
		});
	}

	function endDrag(): void {
		draggedKind = null;
		dropTarget = null;
	}
</script>

<fieldset class="flex min-w-0 flex-col gap-2" data-field={`${order}_priority_preset`} {disabled}>
	<legend class="text-sm font-medium">
		{order === 'search'
			? m.settings_search_priority_preset()
			: m.settings_download_priority_preset()}
	</legend>
	<ol class="flex flex-col gap-1">
		{#each rows as row, index (row.kind)}
			<li
				class="flex flex-wrap items-center gap-x-2 gap-y-1 rounded border border-line px-1 py-1 text-sm"
				class:drop-target={dropTarget === row.kind}
				data-kind={row.kind}
				draggable={!disabled}
				ondragstart={(event) => {
					if (disabled) return event.preventDefault();
					draggedKind = row.kind;
					event.dataTransfer?.setData('text/plain', row.kind);
				}}
				ondragover={(event) => {
					if (disabled || !draggedKind) return;
					event.preventDefault();
					dropTarget = row.kind;
				}}
				ondragleave={() => {
					dropTarget = null;
				}}
				ondragend={endDrag}
				ondrop={(event) => {
					event.preventDefault();
					const kind = event.dataTransfer?.getData('text/plain');
					if (!disabled && kind && kind === draggedKind) {
						commit(dropPriorityKind(rows, kind, index), kind);
					}
					endDrag();
				}}
			>
				<div class="flex min-w-0 basis-full items-baseline justify-between gap-2">
					<span class="min-w-0 font-medium">{kindLabel(row.kind)}</span>
					<span
						id={`${order}-priority-${row.kind}`}
						class="text-xs whitespace-nowrap text-subtle"
						data-rank={preset[row.kind]}
					>
						{m.settings_priority_rank({ rank: preset[row.kind] })}
					</span>
				</div>
				<div class="flex flex-wrap items-center gap-1">
					<button
						type="button"
						class="btn btn-sm px-1"
						aria-label={m.settings_priority_move_up({ kind: kindLabel(row.kind) })}
						disabled={disabled || index === 0}
						onclick={() => commit(movePriorityKind(rows, row.kind, -1), row.kind)}
						>{m.settings_priority_up()}</button
					>
					<button
						type="button"
						class="btn btn-sm px-1"
						aria-label={m.settings_priority_move_down({ kind: kindLabel(row.kind) })}
						disabled={disabled || index === rows.length - 1}
						onclick={() => commit(movePriorityKind(rows, row.kind, 1), row.kind)}
						>{m.settings_priority_down()}</button
					>
					<label class="flex items-center gap-1 text-xs">
						<input
							type="checkbox"
							class="size-6 shrink-0"
							checked={row.tied}
							aria-label={m.settings_priority_tie_kind({ kind: kindLabel(row.kind) })}
							disabled={disabled || index === 0}
							onchange={(event) =>
								commit(tiePriorityKind(rows, row.kind, event.currentTarget.checked), row.kind)}
						/>
						{m.settings_priority_tie()}
					</label>
				</div>
			</li>
		{/each}
	</ol>
	<p class="sr-only" aria-live="polite" aria-atomic="true">{announcement}</p>
</fieldset>

<style>
	.drop-target {
		outline: 2px dashed var(--color-brand);
		outline-offset: -2px;
	}
</style>
