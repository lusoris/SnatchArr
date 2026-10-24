<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import DataTable from '@sveltesentio/ui/data/table';
	import type { ColumnDef } from '@sveltesentio/ui/data';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

	type Event = components['schemas']['Event'];

	const events = createSentioQuery({
		queryKey: keys.events,
		queryFn: async () =>
			(await api.GET('/events', { params: { query: { page_size: 200 } } })).data ?? [],
		refetchInterval: 30_000
	});

	const columns: ColumnDef<Event>[] = [
		{ id: 'ts', header: m.history_when(), accessor: (e) => formatWhen(e.ts, getLocale()) },
		{ id: 'level', header: m.history_level(), accessor: (e) => e.level },
		{ id: 'type', header: m.history_type(), accessor: (e) => e.type },
		{ id: 'title', header: m.history_title_col(), accessor: (e) => e.title ?? '' },
		{ id: 'detail', header: m.history_detail(), accessor: (e) => e.detail ?? '', sortable: false }
	];
</script>

<h2 class="mb-4 text-2xl font-semibold">{m.history_title()}</h2>

{#if events.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if events.isError}
	<ProblemNote error={events.error} />
{:else}
	<div class="history-table relative overflow-x-auto">
		<DataTable
			rows={events.data ?? []}
			{columns}
			label={m.history_title()}
			rowKey={(e) => e.id}
			initialState={{ pageSize: 50 }}
		/>
	</div>
{/if}

<style>
	/* DataTable lays out each row as its own grid; without a template every row sized its own
	   columns. Fixed minimums with flexible shares give every row and the header one template,
	   and below 57rem the table scrolls inside its box instead of the page. */
	.history-table {
		--ssentio-datatable-columns: minmax(12.5rem, 1.2fr) minmax(5.5rem, 0.5fr) minmax(13rem, 1fr)
			minmax(10rem, 1.5fr) minmax(16rem, 3fr);
	}
	.history-table :global(.ssentio-datatable) {
		min-inline-size: 57rem;
	}
	/* The library pads cells with the control height on both sides (4rem per row here). */
	.history-table :global(.ssentio-datatable__cell) {
		min-inline-size: 0;
		overflow-wrap: anywhere;
		padding-block: 0.5rem;
	}
	.history-table :global(.ssentio-datatable__row + .ssentio-datatable__row) {
		border-block-start: 1px solid var(--color-line);
	}
</style>
