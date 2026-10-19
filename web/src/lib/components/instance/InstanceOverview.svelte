<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioMutation, createSentioQuery, useQueryClient } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import CheckState from '#lib/components/CheckState.svelte';
	import InstanceEditForm from '#lib/components/InstanceEditForm.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { probeMessage } from '#lib/products.js';
	import { keys } from '#lib/query.js';

	type Instance = components['schemas']['Instance'];

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);
	const queryClient = useQueryClient();

	const instance = createSentioQuery<Instance | undefined>(() => ({
		queryKey: keys.instance(instanceId),
		queryFn: async () =>
			(await api.GET('/instances/{instanceId}', { params: { path: { instanceId } } })).data
	}));

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

{#if instance.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if instance.isError}
	<ProblemNote error={instance.error} />
{:else if instance.data}
	<div class="mb-4">
		<CheckState instance={instance.data} />
	</div>
	<button type="button" class="btn mb-6" onclick={() => probe.mutate()} disabled={probe.isPending}>
		{m.instance_test()}
	</button>

	<InstanceEditForm instance={instance.data} />
{/if}
