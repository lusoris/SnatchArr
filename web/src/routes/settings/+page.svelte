<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import NotificationChannels from './NotificationChannels.svelte';
	import NotificationRules from './NotificationRules.svelte';

	type Settings = components['schemas']['Settings'];

	const settings = createSentioQuery({
		queryKey: keys.settings,
		queryFn: async () => (await api.GET('/settings')).data
	});
	let draft = $state<Settings | null>(null);
	let currentPassword = $state('');
	let newPassword = $state('');
	let passwordError = $state('');
	let changingPassword = $state(false);

	$effect(() => {
		if (settings.data && !draft) draft = { ...settings.data };
	});
	const save = createSentioMutation({
		mutationFn: async (s: Settings) => (await api.PUT('/settings', { body: s })).data,
		invalidates: [keys.settings, keys.caps],
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
				if (draft) save.mutate(draft);
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
