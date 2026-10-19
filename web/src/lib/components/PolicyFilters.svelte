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

	const lists: { key: ListKey; label: () => string; source: (o: Options) => Option[] }[] = [
		{ key: 'include_tags', label: m.policy_include_tags, source: (o) => o.tags },
		{ key: 'exclude_tags', label: m.policy_exclude_tags, source: (o) => o.tags },
		{
			key: 'include_quality_profiles',
			label: m.policy_include_profiles,
			source: (o) => o.quality_profiles
		},
		{
			key: 'exclude_quality_profiles',
			label: m.policy_exclude_profiles,
			source: (o) => o.quality_profiles
		}
	];
</script>

<fieldset class="grid gap-4 md:col-span-2 md:grid-cols-2">
	<legend class="mb-2 font-semibold">{m.policy_filters_title()}</legend>
	<p class="text-sm text-subtle md:col-span-2">{m.policy_filters_hint()}</p>
	{#if options.isPending}
		<p class="md:col-span-2" aria-live="polite">{m.loading()}</p>
	{:else if options.isError}
		<div class="md:col-span-2"><ProblemNote error={options.error} /></div>
	{:else if options.data}
		{#each lists as list (list.key)}
			{@const choices = list.source(options.data)}
			{#if choices.length === 0}
				<div class="flex flex-col gap-1">
					<span class="text-sm font-medium">{list.label()}</span>
					<p class="text-sm text-subtle">{m.policy_filters_none()}</p>
				</div>
			{:else}
				<Field id={list.key} label={list.label()}>
					<select
						id={list.key}
						class="select"
						multiple
						size={Math.min(choices.length, 6)}
						bind:value={policy[list.key]}
					>
						{#each choices as choice (choice.id)}
							<option value={choice.id}>{choice.name}</option>
						{/each}
					</select>
				</Field>
			{/if}
		{/each}
	{/if}
	<Field id="release_delay" label={m.policy_release_delay()}>
		<input
			id="release_delay"
			class="input"
			type="number"
			min="0"
			max="8760"
			bind:value={policy.release_delay_h}
		/>
	</Field>
</fieldset>
