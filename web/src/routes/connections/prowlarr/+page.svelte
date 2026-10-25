<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdminFormSlot from '#lib/components/AdminFormSlot.svelte';
	import LinkCard from '#lib/components/LinkCard.svelte';
	import LinkForm, { type LinkBody } from '#lib/components/LinkForm.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { FormEditor } from '#lib/formEditor.svelte.js';
	import { deleteCallbacks, probeCallbacks, saveSuccess } from '#lib/forms.js';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';
	import { evaluateLimit, isIndexerExhausted, isIndexerNearLimit } from '#lib/prowlarr.js';

	type ProwlarrLink = components['schemas']['ProwlarrLink'];
	type ProwlarrIndexerStatus = components['schemas']['ProwlarrIndexerStatus'];

	const links = createSentioQuery({
		queryKey: keys.prowlarrLinks,
		queryFn: async () => (await api.GET('/prowlarr')).data ?? []
	});
	const status = createSentioQuery({
		queryKey: keys.prowlarr,
		queryFn: async () => (await api.GET('/prowlarr/status')).data ?? [],
		refetchInterval: 30_000
	});

	const editor = new FormEditor<ProwlarrLink>('prowlarr-name', () => save.reset());

	const save = createSentioMutation({
		mutationFn: async (body: LinkBody) => {
			const target = editor.editing;
			const res = target
				? await api.PUT('/prowlarr/{linkId}', { params: { path: { linkId: target.id } }, body })
				: await api.POST('/prowlarr', { body });
			return res.data;
		},
		invalidates: [keys.prowlarrLinks, keys.prowlarr],
		onSuccess: saveSuccess(editor.close)
	});

	const remove = createSentioMutation({
		mutationFn: async (link: ProwlarrLink) => {
			await api.DELETE('/prowlarr/{linkId}', { params: { path: { linkId: link.id } } });
			return link;
		},
		invalidates: [keys.prowlarrLinks, keys.prowlarr],
		...deleteCallbacks(() => editor.editing, editor.close)
	});

	const probe = createSentioMutation({
		mutationFn: async (linkId: string) =>
			(await api.POST('/prowlarr/{linkId}/test', { params: { path: { linkId } } })).data,
		invalidates: [keys.prowlarrLinks],
		...probeCallbacks()
	});

	const probeInput = createSentioMutation({
		mutationFn: async (body: LinkBody) => (await api.POST('/prowlarr/test', { body })).data,
		...probeCallbacks()
	});

	function indexerReason(idx: ProwlarrIndexerStatus): string | undefined {
		if (idx.exhausted_by === 'grab_limit') {
			return m.prowlarr_exhausted_by_grab_limit();
		}
		if (idx.exhausted_by === 'query_limit') {
			return m.prowlarr_exhausted_by_query_limit();
		}
		if (idx.exhausted_by === 'disabled') {
			return m.prowlarr_exhausted_by_disabled();
		}
		const q = evaluateLimit(idx.used_queries, idx.query_limit);
		const g = evaluateLimit(idx.used_grabs, idx.grab_limit);
		if (q.level === 'exhausted') {
			return m.prowlarr_exhausted_by_query_limit();
		}
		if (g.level === 'exhausted') {
			return m.prowlarr_exhausted_by_grab_limit();
		}
		return undefined;
	}

	function indexerState(idx: ProwlarrIndexerStatus): {
		label: string;
		reason?: string;
		level: 'ok' | 'near' | 'exhausted' | 'disabled';
	} {
		if (idx.disabled_till) {
			return {
				label: m.prowlarr_state_disabled_until({
					when: formatWhen(idx.disabled_till, getLocale())
				}),
				level: 'disabled'
			};
		}
		if (isIndexerExhausted(idx)) {
			return {
				label: m.prowlarr_state_exhausted(),
				reason: indexerReason(idx),
				level: 'exhausted'
			};
		}
		if (isIndexerNearLimit(idx)) {
			return { label: m.prowlarr_state_near_limit(), level: 'near' };
		}
		return { label: m.prowlarr_state_ok(), level: 'ok' };
	}
</script>

<PageHeader
	title={m.prowlarr_title()}
	description={m.prowlarr_readonly_hint()}
	documentTitle
	help={HELP_MAP['/connections/prowlarr']}
/>

<section aria-labelledby="prowlarr-links-section-title" class="w-full">
	<h2 id="prowlarr-links-section-title" class="sr-only">{m.prowlarr_links_title()}</h2>
	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-3 xl:col-span-7">
			<h2 class="text-lg font-semibold">{m.prowlarr_links_title()}</h2>
			{#if links.isPending}
				<p aria-live="polite">{m.loading()}</p>
			{:else if links.isError}
				<ProblemNote error={links.error} />
			{:else if (links.data ?? []).length === 0}
				<p class="card">{m.prowlarr_links_empty()}</p>
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
						/>
					{/each}
				</ul>
			{/if}
		</div>

		<div class="xl:col-span-5">
			<AdminFormSlot {editor} {save} probe={probeInput}>
				{#snippet form(props)}
					<LinkForm idPrefix="prowlarr" link={editor.editing} {...props} />
				{/snippet}
			</AdminFormSlot>
		</div>
	</div>
</section>

<h2 class="mt-8 mb-3 text-lg font-semibold">{m.prowlarr_status_title()}</h2>
{#if status.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if status.isError}
	<ProblemNote error={status.error} />
{:else if (status.data ?? []).length === 0}
	<p class="card">{m.prowlarr_empty()}</p>
{:else}
	{#each status.data ?? [] as link (link.link_id)}
		<section class="card mb-4" aria-labelledby={`prowlarr-${link.link_id}`}>
			<h3 id={`prowlarr-${link.link_id}`} class="font-semibold">
				{link.name} <span class="text-sm text-subtle">{link.version ?? ''}</span>
			</h3>
			<p class="text-sm" role="status">
				{link.reachable ? m.dlclients_reachable() : m.dlclients_unreachable()}
				{#if link.error}<span class="text-danger"> {link.error}</span>{/if}
			</p>
			{#if link.indexers.length === 0}
				<p class="mt-2 text-sm text-subtle">{m.prowlarr_indexers_empty()}</p>
			{:else}
				<div class="mt-3 overflow-x-auto">
					<table class="table">
						<caption class="sr-only">{m.prowlarr_status_title()}</caption>
						<thead>
							<tr>
								<th scope="col">{m.prowlarr_indexer_col()}</th>
								<th scope="col">{m.prowlarr_queries_col()}</th>
								<th scope="col">{m.prowlarr_grabs_col()}</th>
								<th scope="col">{m.prowlarr_state_col()}</th>
							</tr>
						</thead>
						<tbody>
							{#each link.indexers as idx (idx.id)}
								{@const st = indexerState(idx)}
								{@const q = evaluateLimit(idx.used_queries, idx.query_limit)}
								{@const g = evaluateLimit(idx.used_grabs, idx.grab_limit)}
								{@const unit =
									idx.limits_unit === 1 ? m.prowlarr_unit_hour() : m.prowlarr_unit_day()}
								<tr>
									<td class="font-medium">{idx.name}</td>
									<td>
										{#if q.hasLimit}
											<div class="flex items-baseline gap-2">
												<span class="limit-{q.level}">
													{idx.used_queries} / {idx.query_limit} ({unit})
												</span>
												<span class="limit-{q.level} text-xs">
													{q.percent} %
												</span>
											</div>
										{:else}
											<span>{idx.used_queries}</span> / <span>{m.prowlarr_no_limit()}</span>
										{/if}
									</td>
									<td>
										{#if g.hasLimit}
											<div class="flex items-baseline gap-2">
												<span class="limit-{g.level}">
													{idx.used_grabs} / {idx.grab_limit} ({unit})
												</span>
												<span class="limit-{g.level} text-xs">
													{g.percent} %
												</span>
											</div>
										{:else}
											<span>{idx.used_grabs}</span> / <span>{m.prowlarr_no_limit()}</span>
										{/if}
									</td>
									<td>
										<div class="flex flex-col items-start gap-1">
											<span class="chip chip-{st.level}">
												{st.label}
											</span>
											{#if st.reason}
												<span class="text-xs text-subtle">{st.reason}</span>
											{/if}
										</div>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>
	{/each}
{/if}

<style>
	.chip {
		display: inline-flex;
		align-items: center;
		padding: 0.125rem 0.5rem;
		font-size: 0.75rem;
		line-height: 1rem;
		border-radius: 9999px;
		border: 1px solid var(--color-line);
		color: var(--color-subtle);
	}
	.chip-near {
		border-color: var(--color-warning);
		color: var(--color-warning);
	}
	.chip-exhausted {
		border-color: var(--color-danger);
		color: var(--color-danger);
	}
	.chip-disabled {
		opacity: 0.75;
	}
	.limit-near {
		color: var(--color-warning);
	}
	.limit-exhausted {
		color: var(--color-danger);
		font-weight: 600;
	}
</style>
