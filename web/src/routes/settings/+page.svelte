<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '$lib/api/client.js';
	import type { components } from '$lib/api/schema.js';
	import Field from '$lib/components/Field.svelte';
	import ProblemNote from '$lib/components/ProblemNote.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { keys } from '$lib/query.js';

	type Settings = components['schemas']['Settings'];

	const settings = createSentioQuery({
		queryKey: keys.settings,
		queryFn: async () => (await api.GET('/settings')).data
	});
	let draft = $state<Settings | null>(null);
	$effect(() => {
		if (settings.data && !draft) draft = { ...settings.data };
	});
	const save = createSentioMutation({
		mutationFn: async (s: Settings) => (await api.PUT('/settings', { body: s })).data,
		invalidates: [keys.settings, keys.caps],
		onSuccess: () => toast.success(m.settings_saved()),
		onError: (err) => toast.error(describeError(err))
	});
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.settings_title()}</h1>

{#if settings.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if settings.isError}
	<ProblemNote error={settings.error} />
{:else if draft}
	<form
		class="card flex max-w-xl flex-col gap-4"
		onsubmit={(e) => {
			e.preventDefault();
			if (draft) save.mutate(draft);
		}}
	>
		<Field id="retention" label={m.settings_retention()}>
			<input
				id="retention"
				class="input"
				type="number"
				min="1"
				max="3650"
				bind:value={draft.history_retention_days}
			/>
		</Field>
		<Field id="ua" label={m.settings_user_agent()}>
			<input id="ua" class="input" maxlength="200" bind:value={draft.user_agent} />
		</Field>
		<Field id="global_cap" label={m.settings_global_cap()}>
			<input
				id="global_cap"
				class="input"
				type="number"
				min="0"
				max="5000"
				bind:value={draft.global_hourly_cap}
			/>
		</Field>
		<div>
			<button type="submit" class="btn btn-primary" disabled={save.isPending}
				>{m.instance_save()}</button
			>
		</div>
	</form>
{/if}
