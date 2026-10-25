<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type Schedule = components['schemas']['Schedule'];
	type Instance = components['schemas']['Instance'];
	type ScheduleAction = components['schemas']['ScheduleInput']['action'];

	interface Props {
		instances?: Instance[];
		editing?: Schedule | null;
		oncancel?: () => void;
		onsuccess?: () => void;
	}

	const { instances = [], editing = null, oncancel, onsuccess }: Props = $props();

	function defaultTz(): string {
		try {
			return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
		} catch {
			return 'UTC';
		}
	}

	const supportedZones =
		typeof Intl !== 'undefined' && typeof Intl.supportedValuesOf === 'function'
			? Intl.supportedValuesOf('timeZone')
			: [];

	const dayOptions = [
		{ day: 0, label: m.weekday_sun() },
		{ day: 1, label: m.weekday_mon() },
		{ day: 2, label: m.weekday_tue() },
		{ day: 3, label: m.weekday_wed() },
		{ day: 4, label: m.weekday_thu() },
		{ day: 5, label: m.weekday_fri() },
		{ day: 6, label: m.weekday_sat() }
	];

	let name = $state('');
	let instanceId = $state('');
	let days = $state<number[]>([0, 1, 2, 3, 4, 5, 6]);
	let start = $state('22:00');
	let end = $state('06:00');
	let tz = $state(defaultTz());
	let action = $state<ScheduleAction>('pause');
	let capValue = $state<number | undefined>(undefined);
	let enabled = $state(true);
	let formError = $state<unknown>(null);

	$effect(() => {
		if (editing) {
			name = editing.name;
			instanceId = editing.instance_id ?? '';
			days = [...editing.days];
			start = editing.start;
			end = editing.end;
			tz = editing.tz;
			action = editing.action;
			capValue = editing.cap_value;
			enabled = editing.enabled;
			formError = null;
		} else {
			resetForm();
		}
	});

	function resetForm() {
		name = '';
		instanceId = '';
		days = [0, 1, 2, 3, 4, 5, 6];
		start = '22:00';
		end = '06:00';
		tz = defaultTz();
		action = 'pause';
		capValue = undefined;
		enabled = true;
		formError = null;
	}

	function toggleDay(d: number) {
		if (days.includes(d)) {
			if (days.length > 1) {
				days = days.filter((x) => x !== d);
			}
		} else {
			days = [...days, d].sort((a, b) => a - b);
		}
	}

	function buildPayload() {
		return {
			name,
			days,
			start,
			end,
			tz,
			action,
			enabled,
			...(instanceId ? { instance_id: instanceId } : {}),
			...(action === 'cap_override' && capValue !== undefined
				? { cap_value: Number(capValue) }
				: {})
		};
	}

	const save = createSentioMutation({
		mutationFn: async () => {
			const body = buildPayload();
			if (editing) {
				const res = await api.PUT('/schedules/{scheduleId}', {
					params: { path: { scheduleId: editing.id } },
					body
				});
				return res.data;
			}
			const res = await api.POST('/schedules', { body });
			return res.data;
		},
		invalidates: [keys.schedules],
		onSuccess: () => {
			toast.success(editing ? m.schedule_saved() : m.schedule_created());
			formError = null;
			if (!editing) resetForm();
			onsuccess?.();
		},
		onError: (err) => {
			formError = err;
			toast.error(describeError(err));
		}
	});
</script>

<form
	class="card flex flex-col gap-4"
	onsubmit={(e) => {
		e.preventDefault();
		save.mutate();
	}}
>
	<h2 class="text-lg font-semibold">{editing ? m.schedule_edit_title() : m.schedule_add()}</h2>

	{#if formError}
		<ProblemNote error={formError} />
	{/if}

	<Field id="schedule-name" label={m.schedule_name()}>
		<input id="schedule-name" class="input" required maxlength="64" bind:value={name} />
	</Field>

	<Field id="schedule-instance" label={m.schedule_instance()}>
		<select id="schedule-instance" class="select" bind:value={instanceId}>
			<option value="">{m.schedule_global()}</option>
			{#each instances as inst (inst.id)}
				<option value={inst.id}>{inst.name}</option>
			{/each}
		</select>
	</Field>

	<fieldset class="flex flex-col gap-2">
		<legend class="text-sm font-medium">{m.schedule_days()}</legend>
		<div class="flex flex-wrap gap-3">
			{#each dayOptions as opt (opt.day)}
				<label class="flex items-center gap-1 text-sm">
					<input
						type="checkbox"
						checked={days.includes(opt.day)}
						onchange={() => toggleDay(opt.day)}
					/>
					<span>{opt.label}</span>
				</label>
			{/each}
		</div>
	</fieldset>

	<div class="grid grid-cols-2 gap-3">
		<Field id="schedule-start" label={m.schedule_start()} hint="HH:MM">
			<input
				id="schedule-start"
				class="input"
				required
				pattern="^([01][0-9]|2[0-3]):[0-5][0-9]$"
				placeholder="22:00"
				bind:value={start}
			/>
		</Field>
		<Field id="schedule-end" label={m.schedule_end()} hint="HH:MM">
			<input
				id="schedule-end"
				class="input"
				required
				pattern="^([01][0-9]|2[0-3]):[0-5][0-9]$"
				placeholder="06:00"
				bind:value={end}
			/>
		</Field>
	</div>

	<Field id="schedule-tz" label={m.schedule_tz()}>
		<input id="schedule-tz" class="input" list="schedule-tz-list" required bind:value={tz} />
		<datalist id="schedule-tz-list">
			{#each supportedZones as zone (zone)}
				<option value={zone}>{zone}</option>
			{/each}
		</datalist>
	</Field>

	<Field id="schedule-action" label={m.schedule_action()}>
		<select id="schedule-action" class="select" bind:value={action}>
			<option value="pause">{m.schedule_pause()}</option>
			<option value="cap_override">{m.schedule_cap_override()}</option>
		</select>
	</Field>

	{#if action === 'cap_override'}
		<Field id="schedule-cap-value" label={m.schedule_cap_value()}>
			<input
				id="schedule-cap-value"
				type="number"
				class="input"
				min="1"
				max="500"
				required
				bind:value={capValue}
			/>
		</Field>
	{/if}

	<label class="flex items-center gap-2 text-sm font-medium">
		<input id="schedule-enabled" type="checkbox" bind:checked={enabled} />
		<span>{m.schedule_enabled()}</span>
	</label>

	<div class="flex items-center gap-2">
		<button type="submit" class="btn btn-primary" disabled={save.isPending || days.length === 0}>
			{editing ? m.schedule_save() : m.schedule_add()}
		</button>
		{#if editing}
			<button type="button" class="btn" disabled={save.isPending} onclick={oncancel}>
				{m.schedule_cancel()}
			</button>
		{/if}
	</div>
</form>
