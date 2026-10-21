<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import ScheduleForm from '#lib/components/ScheduleForm.svelte';
	import ScheduleTable from '#lib/components/ScheduleTable.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type Schedule = components['schemas']['Schedule'];

	const schedules = createSentioQuery({
		queryKey: keys.schedules,
		queryFn: async () => (await api.GET('/schedules')).data ?? []
	});

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	let editingSchedule = $state<Schedule | null>(null);
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.schedules_title()}</h1>

{#if schedules.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if schedules.isError}
	<ProblemNote error={schedules.error} />
{:else}
	{#if (schedules.data ?? []).length === 0}
		<p class="card mb-6">{m.schedules_empty()}</p>
	{:else}
		<div class="mb-6">
			<ScheduleTable
				schedules={schedules.data ?? []}
				instances={instances.data ?? []}
				onedit={(s) => (editingSchedule = s)}
			/>
		</div>
	{/if}

	{#if session.isAdmin}
		<ScheduleForm
			instances={instances.data ?? []}
			editing={editingSchedule}
			oncancel={() => (editingSchedule = null)}
			onsuccess={() => (editingSchedule = null)}
		/>
	{/if}
{/if}
