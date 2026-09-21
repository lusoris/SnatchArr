<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '$lib/api/client.js';
	import ProblemNote from '$lib/components/ProblemNote.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { keys } from '$lib/query.js';
	import { formatWhen } from '$lib/stamina.js';

	const requests = createSentioQuery({
		queryKey: keys.seerrRequests,
		queryFn: async () => (await api.GET('/seerr/requests')).data ?? [],
		refetchInterval: 60_000
	});
	const snatch = createSentioMutation({
		mutationFn: async (r: { linkId: string; requestId: number; title: string }) => {
			await api.POST('/seerr/requests/{linkId}/{requestId}/snatch', {
				params: { path: { linkId: r.linkId, requestId: r.requestId } }
			});
			return r.title;
		},
		invalidates: [keys.runs],
		onSuccess: (title) => toast.success(m.quickie_queued({ name: title })),
		onError: (err) => toast.error(describeError(err))
	});
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.seerr_title()}</h1>

{#if requests.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if requests.isError}
	<ProblemNote error={requests.error} />
{:else if (requests.data ?? []).length === 0}
	<p class="card">{m.seerr_empty()}</p>
{:else}
	<table class="table">
		<caption class="sr-only">{m.seerr_title()}</caption>
		<thead>
			<tr>
				<th scope="col">{m.history_title_col()}</th>
				<th scope="col">{m.seerr_requested_by()}</th>
				<th scope="col">{m.instance_name()}</th>
				<th scope="col">{m.seerr_resolved()}</th>
				<th scope="col">{m.seerr_last_snatched()}</th>
				<th scope="col"><span class="sr-only">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each requests.data ?? [] as r (`${r.link_id}/${r.request_id}`)}
				<tr>
					<td>{r.title || `${r.media_type} #${r.request_id}`} {r.is_4k ? '4K' : ''}</td>
					<td>{r.requested_by}</td>
					<td>{r.instance_name ?? '–'}</td>
					<td>{r.resolved ? '✓' : (r.unresolved_reason ?? m.seerr_unresolved())}</td>
					<td>{formatWhen(r.last_snatched_at, getLocale())}</td>
					<td>
						<button
							type="button"
							class="btn"
							disabled={!r.resolved || snatch.isPending}
							onclick={() =>
								snatch.mutate({ linkId: r.link_id, requestId: r.request_id, title: r.title })}
							>{m.seerr_snatch_now()}</button
						>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
{/if}
