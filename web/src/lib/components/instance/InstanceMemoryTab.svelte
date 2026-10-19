<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import ResetMemory from '#lib/components/ResetMemory.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type Instance = components['schemas']['Instance'];

	const { id }: { id: string } = $props();
	// The layout remounts each tab per id ({#key}), so the initial value is the value.
	const instanceId = untrack(() => id);

	const instance = createSentioQuery<Instance | undefined>(() => ({
		queryKey: keys.instance(instanceId),
		queryFn: async () =>
			(await api.GET('/instances/{instanceId}', { params: { path: { instanceId } } })).data
	}));
</script>

{#if instance.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if instance.isError}
	<ProblemNote error={instance.error} />
{:else if instance.data}
	<ResetMemory instance={{ id: instanceId, name: instance.data.name }} />
{/if}
