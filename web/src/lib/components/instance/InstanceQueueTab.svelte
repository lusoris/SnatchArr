<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation } from '@sveltesentio/query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import QueuePolicy from '#lib/components/QueuePolicy.svelte';
	import QueueStrikes from '#lib/components/QueueStrikes.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);

	const sweep = createSentioMutation({
		mutationFn: async () => {
			await api.POST('/instances/{instanceId}/queue/sweep', {
				params: { path: { instanceId } }
			});
		},
		invalidates: [keys.queueStrikes(instanceId)],
		onSuccess: () => toast.success(m.queue_sweep_done()),
		onError: (err) => toast.error(describeError(err))
	});
</script>

{#if session.isAdmin}
	<div class="mb-4 flex justify-end">
		<button
			type="button"
			class="btn btn-secondary"
			disabled={sweep.isPending}
			onclick={() => sweep.mutate(undefined)}
		>
			{m.queue_sweep_now()}
		</button>
	</div>
{/if}

<QueuePolicy {instanceId} />
<QueueStrikes {instanceId} />
