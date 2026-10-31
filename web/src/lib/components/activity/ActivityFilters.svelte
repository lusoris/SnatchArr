<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { LIVE_EVENTS } from '#lib/live-events.js';
	import { logLevelLabel, logLevels } from '#lib/logs.js';
	import { m } from '#lib/paraglide/messages.js';

	interface InstanceOption {
		id: string;
		name: string;
	}

	interface Props {
		activeSource: 'events' | 'log';
		effectiveInstance: string;
		fixedInstanceId?: string;
		filterLevel: string;
		filterType: string;
		filterFrom: string;
		filterTo: string;
		filterQ: string;
		instances: readonly InstanceOption[];
		hasActiveFilters: boolean;
		onupdate: (updates: Record<string, string | null | undefined>, replace?: boolean) => void;
		onclear: () => void;
	}

	const {
		activeSource,
		effectiveInstance,
		fixedInstanceId,
		filterLevel,
		filterType,
		filterFrom,
		filterTo,
		filterQ,
		instances,
		hasActiveFilters,
		onupdate,
		onclear
	}: Props = $props();

	let disclosed = $state(false);

	const activeDisclosedCount = $derived(
		(effectiveInstance && !fixedInstanceId ? 1 : 0) +
			(filterLevel ? 1 : 0) +
			(filterType && activeSource === 'events' ? 1 : 0) +
			(filterFrom ? 1 : 0) +
			(filterTo ? 1 : 0)
	);
</script>

<form
	class="card flex flex-wrap items-end gap-2.5 p-3 text-xs"
	onsubmit={(e) => e.preventDefault()}
>
	<div class="flex w-full items-end gap-2 md:contents">
		<div class="flex min-w-[12rem] flex-1 flex-col gap-1">
			<label for="filter-q" class="text-subtle">{m.activity_filter_search()}</label>
			<input
				id="filter-q"
				type="search"
				class="input input-sm w-full rounded border border-line bg-surface px-2 py-1"
				placeholder={m.activity_filter_search()}
				value={filterQ}
				oninput={(e) => onupdate({ q: e.currentTarget.value }, true)}
			/>
		</div>
		<button
			type="button"
			class="btn btn-sm inline-flex items-center gap-1.5 rounded border border-line md:hidden"
			aria-expanded={disclosed}
			onclick={() => (disclosed = !disclosed)}
		>
			<span>{m.activity_filter_toggle()}</span>
			{#if activeDisclosedCount > 0}
				<span
					data-testid="filter-badge"
					class="rounded-full bg-sunken px-1.5 py-0.5 text-[10px] font-semibold text-fg"
				>
					{activeDisclosedCount}
				</span>
			{/if}
		</button>
	</div>

	<div
		data-testid="disclosed-filters"
		class="{disclosed ? 'flex' : 'hidden'} w-full flex-wrap items-end gap-2.5 md:contents"
	>
		{#if !fixedInstanceId && activeSource === 'events'}
			<div class="flex flex-col gap-1">
				<label for="filter-instance" class="text-subtle">{m.activity_filter_instance()}</label>
				<select
					id="filter-instance"
					class="select select-sm rounded border border-line bg-surface px-2 py-1"
					value={effectiveInstance}
					onchange={(e) => onupdate({ instance: e.currentTarget.value })}
				>
					<option value="">{m.activity_filter_all_instances()}</option>
					{#each instances as inst (inst.id)}
						<option value={inst.id}>{inst.name}</option>
					{/each}
				</select>
			</div>
		{/if}

		<div class="flex flex-col gap-1">
			<label for="filter-level" class="text-subtle">{m.activity_filter_level()}</label>
			<select
				id="filter-level"
				class="select select-sm rounded border border-line bg-surface px-2 py-1"
				value={filterLevel}
				onchange={(e) => onupdate({ level: e.currentTarget.value })}
			>
				<option value="">{m.activity_filter_all_levels()}</option>
				{#each logLevels as lvl (lvl)}
					<option value={lvl}>{logLevelLabel(lvl)}</option>
				{/each}
			</select>
		</div>

		{#if activeSource === 'events'}
			<div class="flex flex-col gap-1">
				<label for="filter-type" class="text-subtle">{m.activity_filter_type()}</label>
				<select
					id="filter-type"
					class="select select-sm rounded border border-line bg-surface px-2 py-1"
					value={filterType}
					onchange={(e) => onupdate({ type: e.currentTarget.value })}
				>
					<option value="">{m.activity_filter_all_types()}</option>
					{#each LIVE_EVENTS as evtType (evtType)}
						<option value={evtType}>{evtType}</option>
					{/each}
				</select>
			</div>
		{/if}

		<div class="flex flex-col gap-1">
			<label for="filter-from" class="text-subtle">{m.activity_filter_from()}</label>
			<input
				id="filter-from"
				type="datetime-local"
				class="input input-sm rounded border border-line bg-surface px-2 py-1"
				value={filterFrom}
				onchange={(e) => onupdate({ from: e.currentTarget.value })}
			/>
		</div>

		<div class="flex flex-col gap-1">
			<label for="filter-to" class="text-subtle">{m.activity_filter_to()}</label>
			<input
				id="filter-to"
				type="datetime-local"
				class="input input-sm rounded border border-line bg-surface px-2 py-1"
				value={filterTo}
				onchange={(e) => onupdate({ to: e.currentTarget.value })}
			/>
		</div>
	</div>

	{#if hasActiveFilters}
		<button
			type="button"
			class="btn btn-sm rounded border border-line text-subtle hover:text-fg"
			onclick={onclear}
		>
			{m.activity_clear_filters()}
		</button>
	{/if}
</form>
