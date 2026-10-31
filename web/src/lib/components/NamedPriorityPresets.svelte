<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import {
		applyPriorityPreset,
		deletePriorityPreset,
		namedPriorityPresetSchema,
		renamePriorityPreset,
		savePriorityPreset,
		type NamedPriorityPreset,
		type PriorityPresetInput
	} from '#lib/priority-presets.js';
	import { keys } from '#lib/query.js';
	import type { CapsSettingsDraft, Settings } from '#lib/settings.js';

	interface Props {
		presets: NamedPriorityPreset[];
		draft: CapsSettingsDraft;
		disabled?: boolean;
		onApply: (settings: Settings) => void;
	}
	let { presets, draft, disabled = false, onApply }: Props = $props();
	let name = $state('');
	let editing = $state<string | null>(null);
	let renamed = $state('');
	const count = $derived(presets.filter((preset) => !preset.builtin).length);

	const save = createSentioMutation({
		mutationFn: (body: PriorityPresetInput) => savePriorityPreset(body),
		invalidates: [keys.priorityPresets],
		onSuccess: () => {
			name = '';
			toast.success(m.settings_named_preset_saved());
		}
	});
	const apply = createSentioMutation({
		mutationFn: applyPriorityPreset,
		invalidates: [keys.settings, keys.caps, keys.instances],
		onSuccess: (settings: components['schemas']['Settings'] | undefined) => {
			if (settings) onApply(settings);
			toast.success(m.settings_named_preset_applied());
		}
	});
	const rename = createSentioMutation({
		mutationFn: ({ preset, name }: { preset: NamedPriorityPreset; name: string }) =>
			renamePriorityPreset(preset, name),
		invalidates: [keys.priorityPresets],
		onSuccess: () => {
			editing = null;
			toast.success(m.settings_named_preset_saved());
		}
	});
	const remove = createSentioMutation({
		mutationFn: deletePriorityPreset,
		invalidates: [keys.priorityPresets],
		onSuccess: () => toast.success(m.settings_named_preset_deleted())
	});
	const busy = $derived(
		disabled || save.isPending || apply.isPending || rename.isPending || remove.isPending
	);

	function saveCurrent(event: SubmitEvent): void {
		event.preventDefault();
		const parsed = namedPriorityPresetSchema.safeParse({
			name,
			search: draft.search_priority_preset,
			download: draft.download_priority_preset
		});
		if (parsed.success) save.mutate(parsed.data);
	}

	function submitRename(event: SubmitEvent, preset: NamedPriorityPreset): void {
		event.preventDefault();
		const parsed = namedPriorityPresetSchema.safeParse({
			name: renamed,
			search: preset.search,
			download: preset.download
		});
		if (parsed.success) rename.mutate({ preset, name: parsed.data.name });
	}
</script>

<section class="flex min-w-0 flex-col gap-3" aria-labelledby="named-priority-presets-title">
	<h3 id="named-priority-presets-title" class="text-base font-semibold">
		{m.settings_named_presets_title()}
	</h3>
	<p class="text-sm text-subtle">{m.settings_named_presets_hint()}</p>
	<ProblemNote error={save.error ?? apply.error ?? rename.error ?? remove.error} />
	<form class="flex flex-col gap-2" onsubmit={saveCurrent}>
		<Field id="priority-preset-name" label={m.settings_named_preset_name()}>
			<input
				id="priority-preset-name"
				class="input"
				required
				minlength="1"
				maxlength="40"
				disabled={busy || count >= 20}
				bind:value={name}
			/>
		</Field>
		<button type="submit" class="btn" disabled={busy || count >= 20}
			>{m.settings_named_preset_save()}</button
		>
		<p class="text-xs text-subtle">{m.settings_named_presets_count({ count })}</p>
	</form>
	<ul class="flex flex-col gap-3">
		{#each presets as preset (preset.id ?? preset.name)}
			<li class="flex min-w-0 flex-col gap-2 border-t border-line pt-3" data-preset={preset.name}>
				<div class="flex flex-wrap items-center gap-2">
					<span class="min-w-0 grow font-medium break-words">{preset.name}</span>
					{#if preset.builtin}<span class="text-xs text-subtle"
							>{m.settings_named_preset_builtin()}</span
						>{/if}
				</div>
				<div class="flex flex-wrap gap-2">
					<button
						type="button"
						class="btn"
						disabled={busy}
						aria-label={m.settings_named_preset_apply_label({ name: preset.name })}
						onclick={() => apply.mutate(preset)}>{m.settings_named_preset_apply()}</button
					>
					{#if !preset.builtin}
						<button
							type="button"
							class="btn"
							disabled={busy}
							aria-label={m.settings_named_preset_rename_label({ name: preset.name })}
							onclick={() => {
								editing = preset.id ?? null;
								renamed = preset.name;
							}}>{m.settings_named_preset_rename()}</button
						>
						<button
							type="button"
							class="btn"
							disabled={busy}
							aria-label={m.settings_named_preset_delete_label({ name: preset.name })}
							onclick={() => remove.mutate(preset)}>{m.settings_named_preset_delete()}</button
						>
					{/if}
				</div>
				{#if editing && editing === preset.id}
					<form class="flex flex-col gap-2" onsubmit={(event) => submitRename(event, preset)}>
						<Field id={`rename-${preset.id}`} label={m.settings_named_preset_name()}>
							<input
								id={`rename-${preset.id}`}
								class="input"
								required
								minlength="1"
								maxlength="40"
								disabled={busy}
								bind:value={renamed}
							/>
						</Field>
						<button type="submit" class="btn" disabled={busy}
							>{m.settings_named_preset_rename_save()}</button
						>
						<button
							type="button"
							class="btn"
							disabled={busy}
							onclick={() => {
								editing = null;
							}}>{m.settings_named_preset_cancel()}</button
						>
					</form>
				{/if}
			</li>
		{/each}
	</ul>
</section>
