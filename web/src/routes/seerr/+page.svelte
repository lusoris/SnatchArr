<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { tick } from 'svelte';
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ConfirmDelete from '#lib/components/ConfirmDelete.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import SeerrLinkForm from '#lib/components/SeerrLinkForm.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { probeMessage } from '#lib/products.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

	type SeerrLink = components['schemas']['SeerrLink'];
	type SeerrLinkInput = components['schemas']['SeerrLinkInput'];

	const links = createSentioQuery({
		queryKey: keys.seerrLinks,
		queryFn: async () => (await api.GET('/seerr')).data ?? []
	});
	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});
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

	/** The link in the form; null adds a new one. */
	let editing = $state<SeerrLink | null>(null);
	/** Bumped to remount an emptied create form after a save. */
	let formGeneration = $state(0);

	const save = createSentioMutation({
		mutationFn: async (body: SeerrLinkInput) => {
			const target = editing;
			const res = target
				? await api.PUT('/seerr/{linkId}', { params: { path: { linkId: target.id } }, body })
				: await api.POST('/seerr', { body });
			return res.data;
		},
		invalidates: [keys.seerrLinks, keys.seerrRequests],
		onSuccess: (link) => {
			toast.success(m.form_saved({ name: link?.name ?? '' }));
			closeForm();
		}
	});

	const remove = createSentioMutation({
		mutationFn: async (link: SeerrLink) => {
			await api.DELETE('/seerr/{linkId}', { params: { path: { linkId: link.id } } });
			return link;
		},
		invalidates: [keys.seerrLinks, keys.seerrRequests],
		onSuccess: (link) => {
			toast.success(m.form_deleted({ name: link.name }));
			if (editing?.id === link.id) closeForm();
		},
		onError: (err) => toast.error(describeError(err))
	});

	const probe = createSentioMutation({
		mutationFn: async (linkId: string) =>
			(await api.POST('/seerr/{linkId}/test', { params: { path: { linkId } } })).data,
		invalidates: [keys.seerrLinks],
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => toast.error(m.probe_failed({ detail: describeError(err) }))
	});

	const probeInput = createSentioMutation({
		mutationFn: async (body: SeerrLinkInput) => (await api.POST('/seerr/test', { body })).data,
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => toast.error(m.probe_failed({ detail: describeError(err) }))
	});

	const sync = createSentioMutation({
		mutationFn: async (linkId: string) =>
			(await api.POST('/seerr/{linkId}/sync', { params: { path: { linkId } } })).data,
		invalidates: [keys.seerrLinks, keys.seerrRequests],
		onSuccess: (r) =>
			toast.success(
				m.seerr_sync_done({
					seen: r?.seen ?? 0,
					resolved: r?.resolved ?? 0,
					unresolved: r?.unresolved ?? 0
				})
			),
		onError: (err) => toast.error(describeError(err))
	});

	const importInstances = createSentioMutation({
		mutationFn: async (linkId: string) =>
			(await api.POST('/seerr/{linkId}/import-instances', { params: { path: { linkId } } })).data,
		invalidates: [keys.instances, keys.caps],
		onSuccess: (r) =>
			toast.success(
				m.seerr_import_done({ created: r?.created.length ?? 0, skipped: r?.skipped.length ?? 0 })
			),
		onError: (err) => toast.error(describeError(err))
	});

	function closeForm(): void {
		editing = null;
		formGeneration += 1;
		save.reset();
	}

	async function edit(link: SeerrLink): Promise<void> {
		save.reset();
		editing = link;
		await tick();
		document.getElementById('seerr-name')?.focus();
	}
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.seerr_title()}</h1>

<h2 class="mb-3 text-lg font-semibold">{m.seerr_links_title()}</h2>
{#if links.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if links.isError}
	<ProblemNote error={links.error} />
{:else if (links.data ?? []).length === 0}
	<p class="card mb-6">{m.seerr_links_empty()}</p>
{:else}
	<ul class="mb-6 grid gap-3 md:grid-cols-2">
		{#each links.data ?? [] as link (link.id)}
			<li class="card">
				<h3 class="font-semibold">
					{link.name} <span class="text-sm text-subtle">{link.last_seen_version ?? ''}</span>
				</h3>
				<p class="text-sm break-all text-subtle">{link.base_url}</p>
				<p class="text-sm">
					{link.last_check_at
						? m.link_last_check({ when: formatWhen(link.last_check_at, getLocale()) })
						: m.instance_never_tested()}
					{#if link.last_sync_at}
						· {m.seerr_last_sync({ when: formatWhen(link.last_sync_at, getLocale()) })}
					{/if}
				</p>
				{#if link.last_error}<p class="text-sm text-danger">{link.last_error}</p>{/if}
				<div class="mt-3 flex flex-wrap gap-2">
					<button type="button" class="btn" onclick={() => edit(link)}>{m.action_edit()}</button>
					<button
						type="button"
						class="btn"
						disabled={probe.isPending}
						onclick={() => probe.mutate(link.id)}>{m.action_test()}</button
					>
					<button
						type="button"
						class="btn"
						disabled={sync.isPending}
						onclick={() => sync.mutate(link.id)}>{m.seerr_sync()}</button
					>
					<button
						type="button"
						class="btn"
						disabled={importInstances.isPending}
						onclick={() => importInstances.mutate(link.id)}>{m.seerr_import()}</button
					>
					<ConfirmDelete
						name={link.name}
						disabled={remove.isPending}
						onconfirm={() => remove.mutate(link)}
					/>
				</div>
			</li>
		{/each}
	</ul>
{/if}

{#key `${editing?.id ?? 'new'}-${formGeneration}`}
	<SeerrLinkForm
		link={editing}
		instances={instances.data ?? []}
		pending={save.isPending}
		error={save.error}
		onsave={(body) => save.mutate(body)}
		oncancel={closeForm}
		ontest={(body) => probeInput.mutate(body)}
		testing={probeInput.isPending}
	/>
{/key}

<h2 class="mt-8 mb-3 text-lg font-semibold">{m.seerr_requests_title()}</h2>
{#if requests.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if requests.isError}
	<ProblemNote error={requests.error} />
{:else if (requests.data ?? []).length === 0}
	<p class="card">{m.seerr_empty()}</p>
{:else}
	<div class="relative overflow-x-auto">
		<table class="table">
			<caption class="sr-only">{m.seerr_requests_title()}</caption>
			<thead>
				<tr>
					<th scope="col">{m.history_title_col()}</th>
					<th scope="col">{m.seerr_requested_by()}</th>
					<th scope="col">{m.instance_name()}</th>
					<th scope="col">{m.seerr_resolved()}</th>
					<th scope="col">{m.seerr_last_snatched()}</th>
					<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each requests.data ?? [] as r (`${r.link_id}/${r.request_id}`)}
					<tr>
						<td>{r.title || `${r.media_type} #${r.request_id}`} {r.is_4k ? '4K' : ''}</td>
						<td>{r.requested_by}</td>
						<td>
							{#if r.instance_name}
								{r.instance_name}
							{:else}
								<span aria-hidden="true">–</span><span class="sr-only">{m.seerr_no_instance()}</span
								>
							{/if}
						</td>
						<td>
							{#if r.resolved}
								<span aria-hidden="true">✓</span><span class="sr-only"
									>{m.seerr_resolved_status()}</span
								>
							{:else}
								{r.unresolved_reason ?? m.seerr_unresolved()}
							{/if}
						</td>
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
	</div>
{/if}
