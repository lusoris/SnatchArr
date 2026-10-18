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
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

	type QueueStrike = components['schemas']['QueueStrike'];

	const { instanceId: id }: { instanceId: string } = $props();
	const instanceId = untrack(() => id);

	const query = createSentioQuery<QueueStrike[]>({
		queryKey: keys.queueStrikes(instanceId),
		queryFn: async () =>
			(
				await api.GET('/instances/{instanceId}/queue-strikes', {
					params: { path: { instanceId } }
				})
			).data ?? []
	});

	const ruleLabels: Record<components['schemas']['QueueRule'], () => string> = {
		stalled: m.queue_rule_stalled,
		slow: m.queue_rule_slow,
		failed: m.queue_rule_failed,
		import_blocked: m.queue_rule_import_blocked,
		unwanted_score: m.queue_rule_unwanted_score,
		not_an_upgrade: m.queue_rule_not_an_upgrade,
		unwanted_unmonitored: m.queue_rule_unwanted_unmonitored
	};
</script>

<section class="mt-6" aria-labelledby="queue-strikes-title">
	<h2 id="queue-strikes-title" class="mb-3 text-lg font-semibold">
		{m.queue_strikes_title()}
	</h2>
	{#if query.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if query.isError}
		<ProblemNote error={query.error} />
	{:else if (query.data ?? []).length === 0}
		<p class="text-sm text-subtle">{m.queue_strikes_empty()}</p>
	{:else}
		<div class="overflow-x-auto">
			<table class="table">
				<caption class="sr-only">{m.queue_strikes_title()}</caption>
				<thead>
					<tr>
						<th scope="col">{m.queue_strike_download_id()}</th>
						<th scope="col">{m.queue_strike_rule()}</th>
						<th scope="col">{m.queue_strike_strikes()}</th>
						<th scope="col">{m.queue_strike_last_seen()}</th>
					</tr>
				</thead>
				<tbody>
					{#each query.data ?? [] as strike (strike.download_id)}
						<tr>
							<td class="font-mono text-xs">{strike.download_id}</td>
							<td>{ruleLabels[strike.rule]()}</td>
							<td>
								{m.queue_strike_fraction({ count: strike.count, threshold: strike.threshold })}
							</td>
							<td>{formatWhen(strike.last_seen, getLocale())}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>
