<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdminSaveButton from '#lib/components/AdminSaveButton.svelte';
	import PolicyFilters from '#lib/components/PolicyFilters.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import AppSpecificSection from '#lib/components/policy/AppSpecificSection.svelte';
	import LimitsSection from '#lib/components/policy/LimitsSection.svelte';
	import PresetPicker from '#lib/components/policy/PresetPicker.svelte';
	import SafetySection from '#lib/components/policy/SafetySection.svelte';
	import SearchSection from '#lib/components/policy/SearchSection.svelte';
	import TimingSection from '#lib/components/policy/TimingSection.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import { instanceQuery } from './instance-query.svelte.js';

	type Policy = components['schemas']['SnatchPolicy'];

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);

	const instance = instanceQuery(instanceId);

	const policy = createSentioQuery<Policy | undefined>(() => ({
		queryKey: keys.policy(instanceId),
		queryFn: async () =>
			(await api.GET('/instances/{instanceId}/policy', { params: { path: { instanceId } } })).data
	}));

	// A policy stored before the filters existed has no lists; the form edits empty ones.
	const noFilters = {
		include_tags: [],
		exclude_tags: [],
		include_quality_profiles: [],
		exclude_quality_profiles: [],
		release_delay_h: 0,
		skip_unavailable_movies: true,
		dry_run: false
	} satisfies Partial<Policy>;

	let draft = $state<Policy | null>(null);
	$effect(() => {
		if (policy.data && !draft) draft = { ...noFilters, ...policy.data };
	});

	const save = createSentioMutation({
		mutationFn: async (p: Policy) =>
			(
				await api.PUT('/instances/{instanceId}/policy', {
					params: { path: { instanceId } },
					body: p
				})
			).data,
		invalidates: [keys.instances],
		onSuccess: () => toast.success(m.policy_saved()),
		onError: (err) => toast.error(describeError(err))
	});

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (draft) save.mutate(draft);
	}
</script>

{#if policy.isPending || instance.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if policy.isError}
	<ProblemNote error={policy.error} />
{:else if instance.isError}
	<ProblemNote error={instance.error} />
{:else if draft}
	<form class="flex flex-col gap-6" aria-labelledby="policy-title" onsubmit={handleSubmit}>
		<h2 id="policy-title" class="text-lg font-semibold">{m.policy_title()}</h2>

		{#if save.isError}
			<ProblemNote error={save.error} />
		{/if}

		{#if session.isAdmin}
			<PresetPicker
				{draft}
				onSelect={(vals) => {
					if (draft) draft = { ...draft, ...vals };
				}}
			/>
		{/if}

		<div class="grid gap-6 xl:grid-cols-2">
			<div class="flex flex-col gap-6">
				<SearchSection bind:policy={draft} kind={instance.data?.kind} />
				<TimingSection bind:policy={draft} />
				<LimitsSection bind:policy={draft} />
			</div>
			<div class="flex flex-col gap-6">
				<PolicyFilters {instanceId} bind:policy={draft} />
				<AppSpecificSection bind:policy={draft} kind={instance.data?.kind} />
				<SafetySection bind:policy={draft} />
			</div>
		</div>

		<AdminSaveButton disabled={save.isPending} />
	</form>
{/if}
