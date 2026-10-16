<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioQuery, createSentioMutation, useQueryClient } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import CheckState from '#lib/components/CheckState.svelte';
	import Field from '#lib/components/Field.svelte';
	import InstanceEditForm from '#lib/components/InstanceEditForm.svelte';
	import PolicyFilters from '#lib/components/PolicyFilters.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import ResetMemory from '#lib/components/ResetMemory.svelte';
	import RunList from '#lib/components/RunList.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { instanceKindLabel, probeMessage } from '#lib/products.js';
	import { keys } from '#lib/query.js';

	type Policy = components['schemas']['SnatchPolicy'];
	type Instance = components['schemas']['Instance'];
	const { id }: { id: string } = $props();
	// The route remounts this component per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);
	const queryClient = useQueryClient();

	const instance = createSentioQuery<Instance | undefined>({
		queryKey: keys.instance(instanceId),
		queryFn: async () =>
			(await api.GET('/instances/{instanceId}', { params: { path: { instanceId } } })).data
	});
	const policy = createSentioQuery<Policy | undefined>({
		queryKey: keys.policy(instanceId),
		queryFn: async () =>
			(await api.GET('/instances/{instanceId}/policy', { params: { path: { instanceId } } })).data
	});

	// A policy stored before the filters existed has no lists; the form edits empty ones.
	const noFilters = {
		include_tags: [],
		exclude_tags: [],
		include_quality_profiles: [],
		exclude_quality_profiles: [],
		release_delay_h: 0
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

	const probe = createSentioMutation({
		mutationFn: async () =>
			(await api.POST('/instances/{instanceId}/test', { params: { path: { instanceId } } })).data,
		invalidates: [keys.instances, keys.instance(instanceId)],
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => {
			void queryClient.invalidateQueries({ queryKey: keys.instance(instanceId) });
			toast.error(describeError(err));
		}
	});
</script>

{#if instance.isPending || policy.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if instance.isError}
	<ProblemNote error={instance.error} />
{:else if policy.isError}
	<ProblemNote error={policy.error} />
{:else if instance.data && draft}
	<h1 class="mb-1 text-2xl font-semibold">{instance.data.name}</h1>
	<p class="mb-4 text-sm text-subtle">
		{instanceKindLabel(instance.data.kind, instance.data.last_seen_product)} · {instance.data
			.base_url}
	</p>
	<div class="mb-4">
		<CheckState instance={instance.data} />
	</div>
	<button type="button" class="btn mb-6" onclick={() => probe.mutate()} disabled={probe.isPending}
		>{m.instance_test()}</button
	>

	<InstanceEditForm instance={instance.data} />

	<form
		class="card grid max-w-3xl gap-4 md:grid-cols-2"
		aria-labelledby="policy-title"
		onsubmit={(e) => {
			e.preventDefault();
			if (draft) save.mutate(draft);
		}}
	>
		<h2 id="policy-title" class="text-lg font-semibold md:col-span-2">{m.policy_title()}</h2>
		<Field id="missing" label={m.policy_missing_per_cycle()}>
			<input
				id="missing"
				class="input"
				type="number"
				min="0"
				max="100"
				bind:value={draft.missing_per_cycle}
			/>
		</Field>
		<Field id="upgrade" label={m.policy_upgrade_per_cycle()}>
			<input
				id="upgrade"
				class="input"
				type="number"
				min="0"
				max="100"
				bind:value={draft.upgrade_per_cycle}
			/>
		</Field>
		<Field id="interval" label={m.policy_cycle_interval()}>
			<input
				id="interval"
				class="input"
				type="number"
				min="60"
				step="60"
				bind:value={draft.cycle_interval_s}
			/>
		</Field>
		<Field id="cap" label={m.policy_hourly_cap()}>
			<input id="cap" class="input" type="number" min="1" max="500" bind:value={draft.hourly_cap} />
		</Field>
		<Field id="selection" label={m.policy_selection()}>
			<select id="selection" class="select" bind:value={draft.selection}>
				<option value="random">random</option>
				<option value="sequential">sequential</option>
				<option value="recent">recent</option>
			</select>
		</Field>
		<Field id="ttl" label={m.policy_processed_ttl()}>
			<input
				id="ttl"
				class="input"
				type="number"
				min="1"
				max="8760"
				bind:value={draft.processed_ttl_h}
			/>
		</Field>
		<Field id="afterglow_max" label={m.policy_afterglow_max()}>
			<input
				id="afterglow_max"
				class="input"
				type="number"
				min="1"
				max="8760"
				bind:value={draft.afterglow_max_h}
			/>
		</Field>
		<Field id="grab_window" label={m.policy_recent_grab_window()}>
			<input
				id="grab_window"
				class="input"
				type="number"
				min="0"
				max="720"
				bind:value={draft.recent_grab_window_h}
			/>
		</Field>
		<label class="flex items-center gap-2"
			><input type="checkbox" bind:checked={draft.monitored_only} />
			{m.policy_monitored_only()}</label
		>
		<label class="flex items-center gap-2"
			><input type="checkbox" bind:checked={draft.skip_future_releases} />
			{m.policy_skip_future()}</label
		>
		<label class="flex items-center gap-2"
			><input type="checkbox" bind:checked={draft.await_command} />
			{m.policy_await_command()}</label
		>
		<PolicyFilters {instanceId} bind:policy={draft} />
		<div class="md:col-span-2">
			<button type="submit" class="btn btn-primary" disabled={save.isPending}
				>{m.instance_save()}</button
			>
		</div>
	</form>

	<div class="mt-6">
		<RunList {instanceId} />
		<ResetMemory instance={{ id: instanceId, name: instance.data.name }} />
	</div>
{/if}
