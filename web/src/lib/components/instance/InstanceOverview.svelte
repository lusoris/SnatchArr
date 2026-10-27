<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioMutation, useQueryClient } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import CheckState from '#lib/components/CheckState.svelte';
	import InstanceEditForm from '#lib/components/InstanceEditForm.svelte';
	import InstancePriorityCard from '#lib/components/instance/InstancePriorityCard.svelte';
	import { isArrKind } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import { probeMessage } from '#lib/products.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { instanceQuery } from './instance-query.svelte.js';

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);
	const queryClient = useQueryClient();
	const instance = instanceQuery(instanceId);

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

<QueryGuard query={instance}>
	{#snippet children(data)}
		<section class="card mb-6">
			<div class="flex flex-wrap items-center justify-between gap-4">
				<CheckState instance={data} />
				{#if session.isAdmin}
					<button
						type="button"
						class="btn"
						onclick={() => probe.mutate()}
						disabled={probe.isPending}
					>
						{m.instance_test()}
					</button>
				{/if}
			</div>
		</section>

		{#if session.isAdmin}
			<div class="grid gap-6 xl:grid-cols-2">
				<InstanceEditForm instance={data} />
				{#if isArrKind(data.kind)}
					<InstancePriorityCard instance={data} />
				{/if}
			</div>
		{/if}
	{/snippet}
</QueryGuard>
