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
	import { keys } from '$lib/query.js';

	const clients = createSentioQuery({
		queryKey: keys.dlclients,
		queryFn: async () => (await api.GET('/download-clients')).data ?? []
	});
	const status = createSentioQuery({
		queryKey: keys.dlstatus,
		queryFn: async () => (await api.GET('/download-clients/status')).data ?? [],
		refetchInterval: 30_000
	});
	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});
	let discoverFrom = $state('');
	const discover = createSentioMutation({
		mutationFn: async (instanceId: string) =>
			(
				await api.POST('/instances/{instanceId}/download-clients/discover', {
					params: { path: { instanceId } }
				})
			).data,
		invalidates: [keys.dlclients, keys.dlstatus],
		onSuccess: (r) =>
			toast.success(`+${r?.imported ?? 0} / ~${r?.updated ?? 0} / -${r?.removed ?? 0}`),
		onError: (err) => toast.error(describeError(err))
	});
	function statusFor(id: string) {
		return status.data?.find((s) => s.client_id === id);
	}
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.dlclients_title()}</h1>

<form
	class="mb-4 flex flex-wrap items-end gap-2"
	onsubmit={(e) => {
		e.preventDefault();
		if (discoverFrom) discover.mutate(discoverFrom);
	}}
>
	<label class="flex flex-col gap-1 text-sm">
		<span>{m.dlclients_discover()}</span>
		<select class="select" bind:value={discoverFrom} required>
			<option value="" disabled>–</option>
			{#each instances.data ?? [] as inst (inst.id)}
				<option value={inst.id}>{inst.name}</option>
			{/each}
		</select>
	</label>
	<button type="submit" class="btn btn-primary" disabled={discover.isPending || !discoverFrom}
		>{m.dlclients_discover()}</button
	>
</form>

{#if clients.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if clients.isError}
	<ProblemNote error={clients.error} />
{:else if (clients.data ?? []).length === 0}
	<p class="card">{m.dlclients_empty()}</p>
{:else}
	<ul class="grid gap-3 md:grid-cols-2">
		{#each clients.data ?? [] as c (c.id)}
			{@const st = statusFor(c.id)}
			<li class="card">
				<h2 class="font-semibold">
					{c.name} <span class="text-sm text-subtle">{c.kind} · {c.protocol}</span>
				</h2>
				<p class="text-sm break-all text-subtle">{c.base_url}</p>
				{#if st}
					<p class="mt-1 text-sm" role="status">
						{st.reachable ? m.dlclients_reachable() : m.dlclients_unreachable()}
						{#if st.reachable}· {m.dlclients_active({ active: st.active, queued: st.queued })} · ↓ {Math.round(
								st.download_rate_bps / 1024
							)} KiB/s{/if}
						{#if st.error}<span class="text-danger"> {st.error}</span>{/if}
					</p>
				{/if}
			</li>
		{/each}
	</ul>
{/if}
