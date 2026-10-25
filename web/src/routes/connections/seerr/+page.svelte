<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdminFormSlot from '#lib/components/AdminFormSlot.svelte';
	import LinkCard from '#lib/components/LinkCard.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import SeerrLinkForm from '#lib/components/SeerrLinkForm.svelte';
	import { FormEditor } from '#lib/formEditor.svelte.js';
	import { deleteCallbacks, probeCallbacks, saveSuccess } from '#lib/forms.js';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';
	import { session } from '#lib/session.svelte.js';

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

	const editor = new FormEditor<SeerrLink>('seerr-name', () => save.reset());

	const save = createSentioMutation({
		mutationFn: async (body: SeerrLinkInput) => {
			const target = editor.editing;
			const res = target
				? await api.PUT('/seerr/{linkId}', { params: { path: { linkId: target.id } }, body })
				: await api.POST('/seerr', { body });
			return res.data;
		},
		invalidates: [keys.seerrLinks, keys.seerrRequests],
		onSuccess: saveSuccess(editor.close)
	});

	const remove = createSentioMutation({
		mutationFn: async (link: SeerrLink) => {
			await api.DELETE('/seerr/{linkId}', { params: { path: { linkId: link.id } } });
			return link;
		},
		invalidates: [keys.seerrLinks, keys.seerrRequests],
		...deleteCallbacks(() => editor.editing, editor.close)
	});

	const probe = createSentioMutation({
		mutationFn: async (linkId: string) =>
			(await api.POST('/seerr/{linkId}/test', { params: { path: { linkId } } })).data,
		invalidates: [keys.seerrLinks],
		...probeCallbacks()
	});

	const probeInput = createSentioMutation({
		mutationFn: async (body: SeerrLinkInput) => (await api.POST('/seerr/test', { body })).data,
		...probeCallbacks()
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
</script>

<PageHeader title={m.seerr_title()} documentTitle help={HELP_MAP['/connections/seerr']} />

<section aria-labelledby="seerr-links-section-title" class="w-full">
	<h2 id="seerr-links-section-title" class="sr-only">{m.seerr_links_title()}</h2>
	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-3 xl:col-span-7">
			<h2 class="text-lg font-semibold">{m.seerr_links_title()}</h2>
			{#if links.isPending}
				<p aria-live="polite">{m.loading()}</p>
			{:else if links.isError}
				<ProblemNote error={links.error} />
			{:else if (links.data ?? []).length === 0}
				<p class="card">{m.seerr_links_empty()}</p>
			{:else}
				<ul class="grid grid-cols-1 gap-3 xl:grid-cols-2">
					{#each links.data ?? [] as link (link.id)}
						<LinkCard
							{link}
							testPending={probe.isPending}
							deletePending={remove.isPending}
							onedit={() => editor.open(link)}
							ontest={() => probe.mutate(link.id)}
							ondelete={() => remove.mutate(link)}
						>
							{#snippet statusExtra()}
								{#if link.last_sync_at}
									· {m.seerr_last_sync({ when: formatWhen(link.last_sync_at, getLocale()) })}
								{/if}
							{/snippet}
							{#snippet extraActions()}
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
							{/snippet}
						</LinkCard>
					{/each}
				</ul>
			{/if}
		</div>

		<div class="xl:col-span-5">
			<AdminFormSlot {editor} {save} probe={probeInput}>
				{#snippet form(props)}
					<SeerrLinkForm link={editor.editing} instances={instances.data ?? []} {...props} />
				{/snippet}
			</AdminFormSlot>
		</div>
	</div>
</section>

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
					{#if session.isAdmin}
						<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
					{/if}
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
						{#if session.isAdmin}
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
						{/if}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
