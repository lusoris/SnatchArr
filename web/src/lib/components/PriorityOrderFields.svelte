<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { components } from '#lib/api/schema.js';
	import { kindLabel } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
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
</script>

<fieldset class="flex min-w-0 flex-col gap-2" data-field={`${order}_priority_preset`} {disabled}>
	<legend class="text-sm font-medium">
		{order === 'search'
			? m.settings_search_priority_preset()
			: m.settings_download_priority_preset()}
	</legend>
	<ol class="flex flex-col gap-2">
		{#each rows as row, index (row.kind)}
			<li
				class="flex flex-wrap items-center gap-2 rounded border border-line p-2"
				data-kind={row.kind}
			>
				<span class="min-w-0 grow font-medium">{kindLabel(row.kind)}</span>
				<span
					id={`${order}-priority-${row.kind}`}
					class="text-sm text-subtle"
					data-rank={preset[row.kind]}
				>
					{m.settings_priority_rank({ rank: preset[row.kind] })}
				</span>
				<button
					type="button"
					class="btn"
					aria-label={m.settings_priority_move_up({ kind: kindLabel(row.kind) })}
					disabled={disabled || index === 0}
					onclick={() => commit(movePriorityKind(rows, row.kind, -1), row.kind)}
					>{m.settings_priority_up()}</button
				>
				<button
					type="button"
					class="btn"
					aria-label={m.settings_priority_move_down({ kind: kindLabel(row.kind) })}
					disabled={disabled || index === rows.length - 1}
					onclick={() => commit(movePriorityKind(rows, row.kind, 1), row.kind)}
					>{m.settings_priority_down()}</button
				>
				<label class="flex basis-full items-center gap-2 text-sm">
					<input
						type="checkbox"
						checked={row.tied}
						aria-label={m.settings_priority_tie_kind({ kind: kindLabel(row.kind) })}
						disabled={disabled || index === 0}
						onchange={(event) =>
							commit(tiePriorityKind(rows, row.kind, event.currentTarget.checked), row.kind)}
					/>
					{m.settings_priority_tie()}
				</label>
			</li>
		{/each}
	</ol>
	<p class="sr-only" aria-live="polite" aria-atomic="true">{announcement}</p>
</fieldset>
