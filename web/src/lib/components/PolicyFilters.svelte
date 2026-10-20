<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type Policy = components['schemas']['SnatchPolicy'];
	type Options = components['schemas']['PolicyFilterOptions'];
	type Option = components['schemas']['PolicyFilterOption'];
	type ListKey =
		'include_tags' | 'exclude_tags' | 'include_quality_profiles' | 'exclude_quality_profiles';

	let { instanceId, policy = $bindable() }: { instanceId: string; policy: Policy } = $props();
	// The route layout remounts per instance, so the initial id is the id.
	const id = untrack(() => instanceId);

	const options = createSentioQuery<Options | undefined>({
		queryKey: keys.policyFilterOptions(id),
		queryFn: async () =>
			(
				await api.GET('/instances/{instanceId}/policy/filter-options', {
					params: { path: { instanceId: id } }
				})
			).data
	});

	const lists: {
		key: ListKey;
		label: () => string;
		hint: () => string;
		source: (o: Options) => Option[];
	}[] = [
		{
			key: 'include_tags',
			label: m.policy_include_tags,
			hint: m.policy_include_tags_hint,
			source: (o) => o.tags
		},
		{
			key: 'exclude_tags',
			label: m.policy_exclude_tags,
			hint: m.policy_exclude_tags_hint,
			source: (o) => o.tags
		},
		{
			key: 'include_quality_profiles',
			label: m.policy_include_profiles,
			hint: m.policy_include_profiles_hint,
			source: (o) => o.quality_profiles
		},
		{
			key: 'exclude_quality_profiles',
			label: m.policy_exclude_profiles,
			hint: m.policy_exclude_profiles_hint,
			source: (o) => o.quality_profiles
		}
	];
</script>

<fieldset class="card flex flex-col gap-4">
	<legend class="px-1 text-sm font-semibold">{m.policy_filters_title()}</legend>
	<p class="text-xs text-subtle">{m.policy_filters_hint()}</p>
	{#if options.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if options.isError}
		<div><ProblemNote error={options.error} /></div>
	{:else if options.data}
		<div class="grid gap-4 sm:grid-cols-2">
			{#each lists as list (list.key)}
				{@const choices = list.source(options.data)}
				{#if choices.length === 0}
					<div class="flex flex-col gap-1" data-field={list.key}>
						<span class="text-sm font-medium">{list.label()}</span>
						<p class="text-xs text-subtle">{m.policy_filters_none()}</p>
					</div>
				{:else}
					<Field id={list.key} label={list.label()} hint={list.hint()}>
						<select
							id={list.key}
							data-field={list.key}
							class="select"
							multiple
							size={Math.min(choices.length, 6)}
							aria-describedby={`${list.key}-hint`}
							bind:value={policy[list.key]}
						>
							{#each choices as choice (choice.id)}
								<option value={choice.id}>{choice.name}</option>
							{/each}
						</select>
					</Field>
				{/if}
			{/each}
		</div>
	{/if}
</fieldset>
