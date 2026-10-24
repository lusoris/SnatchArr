<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import CreateForm from '#lib/components/CreateForm.svelte';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type EventKind = components['schemas']['NotificationRuleEventKind'];

	const rules = createSentioQuery({
		queryKey: keys.rules,
		queryFn: async () => (await api.GET('/notifications/rules')).data ?? []
	});

	const channels = createSentioQuery({
		queryKey: keys.channels,
		queryFn: async () => (await api.GET('/notifications/channels')).data ?? []
	});

	let name = $state('');
	let eventKind = $state<EventKind>('run_failure_streak');
	let threshold = $state(1);
	let selectedChannels = $state<string[]>([]);

	function toggleChannel(id: string) {
		if (selectedChannels.includes(id)) {
			selectedChannels = selectedChannels.filter((c) => c !== id);
		} else {
			selectedChannels = [...selectedChannels, id];
		}
	}

	function resetForm() {
		name = '';
		eventKind = 'run_failure_streak';
		threshold = 1;
		selectedChannels = [];
	}

	function eventLabel(kind: string): string {
		switch (kind) {
			case 'run_failure_streak':
				return m.event_kind_run_failure_streak();
			case 'circuit_opened':
				return m.event_kind_circuit_opened();
			case 'instance_unreachable':
				return m.event_kind_instance_unreachable();
			case 'stamina_exhausted':
				return m.event_kind_stamina_exhausted();
			case 'seerr_request_snatched':
				return m.event_kind_seerr_request_snatched();
			default:
				return kind;
		}
	}

	function formatChannels(ids: string[]): string {
		const list = channels.data ?? [];
		const map = new Map(list.map((c) => [c.id, c.name]));
		return ids.map((id) => map.get(id) ?? id).join(', ');
	}

	const create = createSentioMutation({
		mutationFn: async () => {
			const res = await api.POST('/notifications/rules', {
				body: {
					name,
					event_kind: eventKind,
					threshold,
					enabled: true,
					channel_ids: selectedChannels
				}
			});
			return res.data;
		},
		invalidates: [keys.rules],
		onSuccess: () => {
			toast.success(m.rule_saved());
			resetForm();
		},
		onError: (err) => toast.error(describeError(err))
	});

	const remove = createSentioMutation({
		mutationFn: async (id: string) => {
			await api.DELETE('/notifications/rules/{ruleId}', {
				params: { path: { ruleId: id } }
			});
		},
		invalidates: [keys.rules],
		onSuccess: () => toast.success(m.rule_deleted()),
		onError: (err) => toast.error(describeError(err))
	});
</script>

<h3 class="mt-6 mb-3 text-lg font-semibold">{m.notifications_rules_title()}</h3>

{#if rules.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if rules.isError}
	<ProblemNote error={rules.error} />
{:else if (rules.data ?? []).length === 0}
	<p class="card mb-4">{m.notifications_empty_rules()}</p>
{:else}
	<div class="relative overflow-x-auto">
		<table class="mb-6 table">
			<caption class="sr-only">{m.notifications_rules_title()}</caption>
			<thead>
				<tr>
					<th scope="col">{m.rule_name()}</th>
					<th scope="col">{m.rule_event_kind()}</th>
					<th scope="col">{m.rule_threshold()}</th>
					<th scope="col">{m.rule_channels()}</th>
					{#if session.isAdmin}
						<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
					{/if}
				</tr>
			</thead>
			<tbody>
				{#each rules.data ?? [] as r (r.id)}
					<tr>
						<td>{r.name}</td>
						<td>{eventLabel(r.event_kind)}</td>
						<td>{r.threshold}</td>
						<td>{formatChannels(r.channel_ids)}</td>
						{#if session.isAdmin}
							<td>
								<button
									type="button"
									class="btn"
									disabled={remove.isPending}
									onclick={() => remove.mutate(r.id)}
								>
									{m.rule_delete()}
								</button>
							</td>
						{/if}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<CreateForm heading={m.rule_add()} onsubmit={() => create.mutate()}>
	<Field id="rule-name" label={m.rule_name()}>
		<input id="rule-name" class="input" required bind:value={name} />
	</Field>
	<Field id="rule-event" label={m.rule_event_kind()}>
		<select id="rule-event" class="select" bind:value={eventKind}>
			<option value="run_failure_streak">{m.event_kind_run_failure_streak()}</option>
			<option value="circuit_opened">{m.event_kind_circuit_opened()}</option>
			<option value="instance_unreachable">{m.event_kind_instance_unreachable()}</option>
			<option value="stamina_exhausted">{m.event_kind_stamina_exhausted()}</option>
			<option value="seerr_request_snatched">{m.event_kind_seerr_request_snatched()}</option>
		</select>
	</Field>
	<Field id="rule-threshold" label={m.rule_threshold()}>
		<input
			id="rule-threshold"
			class="input"
			type="number"
			min="1"
			max="1000"
			required
			bind:value={threshold}
		/>
	</Field>

	<fieldset class="flex flex-col gap-2">
		<legend class="text-sm font-medium">{m.rule_channels()}</legend>
		{#if (channels.data ?? []).length === 0}
			<p class="text-sm text-subtle">{m.notifications_empty_channels()}</p>
		{:else}
			{#each channels.data ?? [] as ch (ch.id)}
				<label class="flex items-center gap-2 text-sm">
					<input
						type="checkbox"
						checked={selectedChannels.includes(ch.id)}
						onchange={() => toggleChannel(ch.id)}
					/>
					<span>{ch.name} ({ch.kind})</span>
				</label>
			{/each}
		{/if}
	</fieldset>

	<div>
		<button
			type="submit"
			class="btn btn-primary"
			disabled={create.isPending || selectedChannels.length === 0}
		>
			{m.rule_add()}
		</button>
	</div>
</CreateForm>
