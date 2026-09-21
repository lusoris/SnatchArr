<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '$lib/api/client.js';
	import ProblemNote from '$lib/components/ProblemNote.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { keys } from '$lib/query.js';

	const schedules = createSentioQuery({
		queryKey: keys.schedules,
		queryFn: async () => (await api.GET('/schedules')).data ?? []
	});
	const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.schedules_title()}</h1>

{#if schedules.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if schedules.isError}
	<ProblemNote error={schedules.error} />
{:else if (schedules.data ?? []).length === 0}
	<p class="card">{m.schedules_empty()}</p>
{:else}
	<table class="table">
		<caption class="sr-only">{m.schedules_title()}</caption>
		<thead>
			<tr>
				<th scope="col">{m.schedule_name()}</th>
				<th scope="col">{m.schedule_days()}</th>
				<th scope="col">{m.schedule_window()}</th>
				<th scope="col">{m.schedule_action()}</th>
			</tr>
		</thead>
		<tbody>
			{#each schedules.data ?? [] as s (s.id)}
				<tr>
					<td>{s.name}</td>
					<td>{s.days.map((d) => dayNames[d] ?? d).join(', ')}</td>
					<td>{s.start}–{s.end} {s.tz}</td>
					<td
						>{s.action === 'pause'
							? m.schedule_pause()
							: `${m.schedule_cap_override()} ${s.cap_value ?? ''}`}</td
					>
				</tr>
			{/each}
		</tbody>
	</table>
{/if}
