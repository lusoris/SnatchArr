<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import DataTable from '@sveltesentio/ui/data/table';
	import type { ColumnDef } from '@sveltesentio/ui/data';
	import { api } from '$lib/api/client.js';
	import type { components } from '$lib/api/schema.js';
	import ProblemNote from '$lib/components/ProblemNote.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { keys } from '$lib/query.js';
	import { formatWhen } from '$lib/stamina.js';

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

<h1 class="mb-4 text-2xl font-semibold">{m.history_title()}</h1>

{#if events.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if events.isError}
	<ProblemNote error={events.error} />
{:else}
	<DataTable
		rows={events.data ?? []}
		{columns}
		label={m.history_title()}
		rowKey={(e) => e.id}
		initialState={{ pageSize: 50 }}
	/>
{/if}
