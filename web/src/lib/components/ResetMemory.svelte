<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ConfirmAction from '#lib/components/ConfirmAction.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type StateReset = components['schemas']['StateReset'];

	interface Props {
		/** The instance whose memory to forget; without it, every instance's. */
		instance?: { id: string; name: string };
	}

	const { instance }: Props = $props();

	// StateReset carries only instance_id: a reset always covers missing and upgrade alike.
	const label = $derived(instance ? m.memory_reset_one() : m.memory_reset_all());
	const message = $derived(
		instance ? m.memory_reset_one_confirm({ name: instance.name }) : m.memory_reset_all_confirm()
	);

	const reset = createSentioMutation({
		mutationFn: async (body: StateReset) =>
			(await api.POST('/state/reset', { body })).data?.forgotten ?? 0,
		invalidates: [keys.instances, keys.runs]
	});
</script>

<section class="card mb-6 flex max-w-xl flex-col gap-3" aria-labelledby="memory-title">
	<h2 id="memory-title" class="text-lg font-semibold">{m.memory_title()}</h2>
	<ConfirmAction
		{label}
		{message}
		pending={reset.isPending}
		onconfirm={() => reset.mutate(instance ? { instance_id: instance.id } : {})}
	/>
	<p aria-live="polite" class="text-sm">
		{reset.isSuccess ? m.memory_reset_done({ count: reset.data }) : ''}
	</p>
	<ProblemNote error={reset.error} />
</section>
