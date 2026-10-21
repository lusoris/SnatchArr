<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import PriorityPresetFields from '#lib/components/PriorityPresetFields.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { APP_KINDS, type AppKind } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import NotificationChannels from './NotificationChannels.svelte';
	import NotificationRules from './NotificationRules.svelte';

	type Settings = components['schemas']['Settings'];
	type PriorityPreset = components['schemas']['PriorityPreset'];
	type PriorityPresetMap = components['schemas']['PriorityPresetMap'];

	const rankSchema = z.number().int().min(1).max(100);
	const presetFields = Object.fromEntries(APP_KINDS.map((k) => [k, rankSchema])) as Record<
		AppKind,
		typeof rankSchema
	>;
	const presetMapSchema = z.object(presetFields) satisfies z.ZodType<PriorityPresetMap>;
	const settingsSchema = z.object({
		history_retention_days: z.number().int().min(1).max(3650),
		user_agent: z.string().min(1).max(200),
		global_hourly_cap: z.number().int().min(0).max(5000),
		search_priority_preset: presetMapSchema,
		download_priority_preset: presetMapSchema,
		llm_enabled: z.boolean()
	}) satisfies z.ZodType<Omit<Settings, 'updated_at'>>;

	const settings = createSentioQuery({
		queryKey: keys.settings,
		queryFn: async () => (await api.GET('/settings')).data
	});
	const presets = createSentioQuery<PriorityPreset[] | undefined>({
		queryKey: keys.priorityPresets,
		queryFn: async () => (await api.GET('/priority-presets')).data
	});

	let draft = $state<Settings | null>(null);
	let currentPassword = $state('');
	let newPassword = $state('');
	let passwordError = $state('');
	let changingPassword = $state(false);

	const mediaFirst = $derived(presets.data?.find((p) => p.name === 'media_first'));

	$effect(() => {
		if (settings.data && !draft) {
			draft = {
				...settings.data,
				search_priority_preset: { ...(settings.data.search_priority_preset ?? {}) },
				download_priority_preset: { ...(settings.data.download_priority_preset ?? {}) }
			};
		}
	});

	function resetToShippedPreset(): void {
		if (!draft || !mediaFirst) return;
		draft.search_priority_preset = { ...mediaFirst.search };
		draft.download_priority_preset = { ...mediaFirst.download };
	}

	const save = createSentioMutation({
		mutationFn: async (s: Settings) => (await api.PUT('/settings', { body: s })).data,
		invalidates: [keys.settings, keys.caps, keys.instances],
		onSuccess: () => toast.success(m.settings_saved()),
		onError: (err) => toast.error(describeError(err))
	});

	async function changePassword(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		changingPassword = true;
		passwordError = '';
		try {
			await api.POST('/auth/password', {
				body: {
					current_password: currentPassword,
					new_password: newPassword
				}
			});
			toast.success(m.password_changed());
			currentPassword = '';
			newPassword = '';
		} catch (err) {
			passwordError = describeError(err);
		} finally {
			changingPassword = false;
		}
	}
</script>

<div class="mx-auto flex max-w-xl flex-col gap-6">
	<h1 class="text-2xl font-semibold">{m.settings_title()}</h1>

	{#if settings.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if settings.isError}
		<ProblemNote error={settings.error} />
	{:else if draft}
		{#if save.isError}
			<ProblemNote error={save.error} />
		{/if}

		<form
			class="card flex flex-col gap-4"
			onsubmit={(e) => {
				e.preventDefault();
				if (draft && settingsSchema.safeParse(draft).success) save.mutate(draft);
			}}
		>
			<Field id="retention" label={m.settings_retention()} hint={m.settings_retention_hint()}>
				<input
					id="retention"
					data-field="history_retention_days"
					class="input"
					type="number"
					min="1"
					max="3650"
					aria-describedby="retention-hint"
					bind:value={draft.history_retention_days}
				/>
			</Field>
			<Field id="ua" label={m.settings_user_agent()} hint={m.settings_user_agent_hint()}>
				<input
					id="ua"
					data-field="user_agent"
					class="input"
					maxlength="200"
					aria-describedby="ua-hint"
					bind:value={draft.user_agent}
				/>
			</Field>
			<Field id="global_cap" label={m.settings_global_cap()} hint={m.settings_global_cap_hint()}>
				<input
					id="global_cap"
					data-field="global_hourly_cap"
					class="input"
					type="number"
					min="0"
					max="5000"
					aria-describedby="global_cap-hint"
					bind:value={draft.global_hourly_cap}
				/>
			</Field>
			<PriorityPresetFields
				bind:searchPreset={draft.search_priority_preset}
				bind:downloadPreset={draft.download_priority_preset}
				canReset={mediaFirst !== undefined}
				onReset={resetToShippedPreset}
			/>
			<label class="flex items-center gap-2">
				<input type="checkbox" data-field="llm_enabled" bind:checked={draft.llm_enabled} />
				{m.settings_llm_enabled()}
			</label>
			{#if session.isAdmin}
				<div>
					<button type="submit" class="btn btn-primary" disabled={save.isPending}
						>{m.instance_save()}</button
					>
				</div>
			{/if}
		</form>

		<h2 class="mt-4 text-xl font-semibold">{m.change_password_title()}</h2>
		<form class="card flex flex-col gap-4" onsubmit={changePassword}>
			<Field id="current-password" label={m.current_password()}>
				<input
					id="current-password"
					class="input"
					type="password"
					autocomplete="current-password"
					required
					bind:value={currentPassword}
				/>
			</Field>
			<Field id="new-password" label={m.new_password()} hint={m.password_min_hint()}>
				<input
					id="new-password"
					class="input"
					type="password"
					autocomplete="new-password"
					required
					minlength="12"
					bind:value={newPassword}
				/>
			</Field>
			{#if passwordError}
				<p role="alert" class="text-sm text-danger">{passwordError}</p>
			{/if}
			<div>
				<button type="submit" class="btn btn-primary" disabled={changingPassword}>
					{m.change_password_submit()}
				</button>
			</div>
		</form>

		<h2 class="mt-4 text-xl font-semibold">{m.notifications_title()}</h2>
		<NotificationChannels />
		<NotificationRules />
	{/if}
</div>
