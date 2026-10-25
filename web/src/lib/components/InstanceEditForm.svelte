<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import KindSelect from '#lib/components/KindSelect.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type Instance = components['schemas']['Instance'];
	type InstanceUpdate = components['schemas']['InstanceUpdate'];

	const { instance }: { instance: Instance } = $props();
	// The form edits the instance it was opened with; a refetch after saving must not reset it.
	const initial = untrack(() => instance);
	const readonly = initial.source === 'configarr';

	let kind = $state(initial.kind);
	let name = $state(initial.name);
	let baseUrl = $state(initial.base_url);
	let enabled = $state(initial.enabled);
	// Write-only: the API never returns the key, so the field starts empty and empty means unchanged.
	let apiKey = $state('');

	function body(): InstanceUpdate {
		const update: InstanceUpdate = { kind, name, base_url: baseUrl, enabled };
		if (apiKey) update.api_key = apiKey;
		return update;
	}

	const save = createSentioMutation({
		mutationFn: async (update: InstanceUpdate) =>
			(
				await api.PUT('/instances/{instanceId}', {
					params: { path: { instanceId: initial.id } },
					body: update
				})
			).data,
		invalidates: [keys.instances, keys.instance(initial.id), keys.caps],
		onSuccess: () => {
			apiKey = '';
			toast.success(m.instance_saved());
		}
	});
</script>

<form
	class="card flex flex-col gap-3"
	aria-labelledby="edit-instance-title"
	onsubmit={(e) => {
		e.preventDefault();
		if (!readonly) save.mutate(body());
	}}
>
	<h2 id="edit-instance-title" class="text-lg font-semibold">{m.instance_edit_title()}</h2>
	{#if readonly}
		<p id="edit-instance-readonly" class="text-sm text-subtle">
			{m.instance_configarr_readonly()}
		</p>
	{/if}
	<fieldset
		class="flex flex-col gap-3"
		disabled={readonly}
		aria-describedby={readonly ? 'edit-instance-readonly' : undefined}
	>
		<legend class="sr-only">{m.instance_edit_title()}</legend>
		<KindSelect bind:value={kind} disabled={readonly} />
		<Field id="edit-name" label={m.instance_name()}>
			<input id="edit-name" class="input" required maxlength="64" bind:value={name} />
		</Field>
		<Field id="edit-base_url" label={m.instance_url()}>
			<input id="edit-base_url" class="input" required inputmode="url" bind:value={baseUrl} />
		</Field>
		{#if !readonly}
			<Field id="edit-api_key" label={m.instance_api_key()} hint={m.instance_api_key_keep()}>
				<input
					id="edit-api_key"
					class="input"
					type="password"
					autocomplete="new-password"
					minlength="8"
					bind:value={apiKey}
					aria-describedby="edit-api_key-hint"
				/>
			</Field>
		{/if}
		<label class="flex items-center gap-2"
			><input type="checkbox" bind:checked={enabled} />
			{m.instance_enabled()}</label
		>
	</fieldset>
	<ProblemNote error={save.error} />
	{#if !readonly}
		<div>
			<button type="submit" class="btn btn-primary" disabled={save.isPending}
				>{m.instance_save()}</button
			>
		</div>
	{/if}
</form>
