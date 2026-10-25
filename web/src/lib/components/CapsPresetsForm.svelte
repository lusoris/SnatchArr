<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdminSaveButton from '#lib/components/AdminSaveButton.svelte';
	import Field from '#lib/components/Field.svelte';
	import PriorityPresetFields from '#lib/components/PriorityPresetFields.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import {
		capsSettingsSchema,
		mergeCapsSettings,
		saveMergedSettings,
		type CapsSettingsDraft
	} from '#lib/settings.js';

	type PriorityPreset = components['schemas']['PriorityPreset'];

	const settings = createSentioQuery({
		queryKey: keys.settings,
		queryFn: async () => (await api.GET('/settings')).data
	});

	const presets = createSentioQuery<PriorityPreset[] | undefined>({
		queryKey: keys.priorityPresets,
		queryFn: async () => (await api.GET('/priority-presets')).data
	});

	let draft = $state<CapsSettingsDraft | null>(null);

	const mediaFirst = $derived(presets.data?.find((p) => p.name === 'media_first'));

	$effect(() => {
		if (settings.data && !draft) {
			draft = {
				global_hourly_cap: settings.data.global_hourly_cap,
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
		mutationFn: (d: CapsSettingsDraft) => saveMergedSettings(d, mergeCapsSettings),
		invalidates: [keys.settings, keys.caps, keys.instances],
		onSuccess: () => toast.success(m.settings_saved()),
		onError: (err) => toast.error(describeError(err))
	});

	function handleSubmit(e: SubmitEvent): void {
		e.preventDefault();
		if (!draft) return;
		const parsed = capsSettingsSchema.safeParse(draft);
		if (parsed.success) {
			save.mutate(parsed.data);
		}
	}
</script>

{#if settings.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if settings.isError}
	<ProblemNote error={settings.error} />
{:else if draft}
	<section class="grid min-w-0 gap-6 xl:grid-cols-12">
		<form class="card flex min-w-0 flex-col gap-4 xl:col-span-7" onsubmit={handleSubmit}>
			{#if save.isError}
				<ProblemNote error={save.error} />
			{/if}

			<Field id="global_cap" label={m.settings_global_cap()} hint={m.settings_global_cap_hint()}>
				<input
					id="global_cap"
					data-field="global_hourly_cap"
					class="input"
					type="number"
					min="0"
					max="5000"
					disabled={!session.isAdmin}
					aria-describedby="global_cap-hint"
					bind:value={draft.global_hourly_cap}
				/>
			</Field>

			<PriorityPresetFields
				bind:searchPreset={draft.search_priority_preset}
				bind:downloadPreset={draft.download_priority_preset}
				canReset={session.isAdmin && mediaFirst !== undefined}
				onReset={resetToShippedPreset}
				disabled={!session.isAdmin}
			/>

			<AdminSaveButton disabled={save.isPending} />
		</form>

		<aside class="card flex h-fit min-w-0 flex-col gap-3 xl:col-span-5">
			<h3 class="text-base font-semibold">{m.automation_caps_help_title()}</h3>
			<p class="text-sm leading-relaxed text-subtle">
				{m.automation_caps_help_desc()}
			</p>
		</aside>
	</section>
{/if}
