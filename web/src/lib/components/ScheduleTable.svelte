<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import { activePauseText } from '#lib/overview.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type Schedule = components['schemas']['Schedule'];
	type Instance = components['schemas']['Instance'];

	interface Props {
		schedules: Schedule[];
		instances?: Instance[];
		onedit: (s: Schedule) => void;
	}

	const { schedules, instances = [], onedit }: Props = $props();

	let deletingId = $state<string | null>(null);

	const dayNames = [
		m.weekday_sun(),
		m.weekday_mon(),
		m.weekday_tue(),
		m.weekday_wed(),
		m.weekday_thu(),
		m.weekday_fri(),
		m.weekday_sat()
	];

	function instanceName(id?: string): string | undefined {
		if (!id) return undefined;
		const match = instances.find((i) => i.id === id);
		return match?.name;
	}

	const remove = createSentioMutation({
		mutationFn: async (id: string) => {
			await api.DELETE('/schedules/{scheduleId}', {
				params: { path: { scheduleId: id } }
			});
		},
		invalidates: [keys.schedules],
		onSuccess: () => {
			toast.success(m.schedule_deleted());
			deletingId = null;
		},
		onError: (err) => toast.error(describeError(err))
	});
</script>

<div class="relative overflow-x-auto">
	<table class="table">
		<caption class="sr-only">{m.schedules_title()}</caption>
		<thead>
			<tr>
				<th scope="col">{m.schedule_name()}</th>
				<th scope="col">{m.schedule_days()}</th>
				<th scope="col">{m.schedule_window()}</th>
				<th scope="col">{m.schedule_action()}</th>
				{#if session.isAdmin}
					<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
				{/if}
			</tr>
		</thead>
		<tbody>
			{#each schedules as s (s.id)}
				<tr>
					<td>
						<div>{s.name}</div>
						{#if s.instance_id}
							<div class="text-xs text-subtle">{instanceName(s.instance_id) ?? s.instance_id}</div>
						{/if}
					</td>
					<td>{s.days.map((d) => dayNames[d] ?? d).join(', ')}</td>
					<td>{s.start}–{s.end} {s.tz}</td>
					<td>
						<div>
							{s.action === 'pause'
								? m.schedule_pause()
								: `${m.schedule_cap_override()} ${s.cap_value ?? ''}`}
						</div>
						{#if s.action === 'pause' && s.active}
							<div class="mt-1 text-xs font-semibold text-danger">
								{activePauseText(s, instances)}
							</div>
						{/if}
					</td>
					{#if session.isAdmin}
						<td>
							{#if deletingId === s.id}
								<div class="flex flex-wrap items-center gap-2">
									<span class="text-xs font-medium text-danger">
										{m.schedule_delete_confirm({ name: s.name })}
									</span>
									<button
										type="button"
										class="btn btn-primary"
										disabled={remove.isPending}
										onclick={() => remove.mutate(s.id)}
									>
										{m.schedule_confirm_delete()}
									</button>
									<button
										type="button"
										class="btn"
										disabled={remove.isPending}
										onclick={() => (deletingId = null)}
									>
										{m.schedule_cancel()}
									</button>
								</div>
							{:else}
								<div class="flex items-center gap-2">
									<button type="button" class="btn" onclick={() => onedit(s)}>
										{m.schedule_edit()}
									</button>
									<button type="button" class="btn" onclick={() => (deletingId = s.id)}>
										{m.schedule_delete()}
									</button>
								</div>
							{/if}
						</td>
					{/if}
				</tr>
			{/each}
		</tbody>
	</table>
</div>
