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
	import LinkForm, { type LinkBody } from '#lib/components/LinkForm.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { probeMessage } from '#lib/products.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

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

	/** The link in the form; null adds a new one. */
	let editing = $state<ProwlarrLink | null>(null);
	/** Bumped to remount an emptied create form after a save. */
	let formGeneration = $state(0);

	const save = createSentioMutation({
		mutationFn: async (body: LinkBody) => {
			const target = editing;
			const res = target
				? await api.PUT('/prowlarr/{linkId}', { params: { path: { linkId: target.id } }, body })
				: await api.POST('/prowlarr', { body });
			return res.data;
		},
		invalidates: [keys.prowlarrLinks, keys.prowlarr],
		onSuccess: (link) => {
			toast.success(m.form_saved({ name: link?.name ?? '' }));
			closeForm();
		}
	});

	const remove = createSentioMutation({
		mutationFn: async (link: ProwlarrLink) => {
			await api.DELETE('/prowlarr/{linkId}', { params: { path: { linkId: link.id } } });
			return link;
		},
		invalidates: [keys.prowlarrLinks, keys.prowlarr],
		onSuccess: (link) => {
			toast.success(m.form_deleted({ name: link.name }));
			if (editing?.id === link.id) closeForm();
		},
		onError: (err) => toast.error(describeError(err))
	});

	const probe = createSentioMutation({
		mutationFn: async (linkId: string) =>
			(await api.POST('/prowlarr/{linkId}/test', { params: { path: { linkId } } })).data,
		invalidates: [keys.prowlarrLinks],
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => toast.error(m.probe_failed({ detail: describeError(err) }))
	});

	const probeInput = createSentioMutation({
		mutationFn: async (body: LinkBody) => (await api.POST('/prowlarr/test', { body })).data,
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => toast.error(m.probe_failed({ detail: describeError(err) }))
	});

	function isExhausted(idx: ProwlarrIndexerStatus): boolean {
		if (idx.exhausted) return true;
		if (!idx.query_limit || idx.query_limit <= 0) return false;
		return idx.used_queries >= idx.query_limit;
	}

	function isNearLimit(idx: ProwlarrIndexerStatus): boolean {
		if (!idx.query_limit || idx.query_limit <= 0) return false;
		return idx.used_queries / idx.query_limit >= 0.8;
	}

	function indexerState(idx: ProwlarrIndexerStatus): {
		label: string;
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
		if (isExhausted(idx)) {
			return { label: m.prowlarr_state_exhausted(), level: 'exhausted' };
		}
		if (isNearLimit(idx)) {
			return { label: m.prowlarr_state_near_limit(), level: 'near' };
		}
		return { label: m.prowlarr_state_ok(), level: 'ok' };
	}

	function closeForm(): void {
		editing = null;
		formGeneration += 1;
		save.reset();
	}

	async function edit(link: ProwlarrLink): Promise<void> {
		save.reset();
		editing = link;
		await tick();
		document.getElementById('prowlarr-name')?.focus();
	}
</script>

<h1 class="mb-2 text-2xl font-semibold">{m.prowlarr_title()}</h1>
<p class="mb-4 text-sm text-subtle">{m.prowlarr_readonly_hint()}</p>

<h2 class="mb-3 text-lg font-semibold">{m.prowlarr_links_title()}</h2>
{#if links.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if links.isError}
	<ProblemNote error={links.error} />
{:else if (links.data ?? []).length === 0}
	<p class="card mb-6">{m.prowlarr_links_empty()}</p>
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
	<LinkForm
		idPrefix="prowlarr"
		link={editing}
		pending={save.isPending}
		error={save.error}
		onsave={(body) => save.mutate(body)}
		oncancel={closeForm}
		ontest={(body) => probeInput.mutate(body)}
		testing={probeInput.isPending}
	/>
{/key}

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
								<th scope="col">{m.prowlarr_percent_col()}</th>
								<th scope="col">{m.prowlarr_state_col()}</th>
							</tr>
						</thead>
						<tbody>
							{#each link.indexers as idx (idx.id)}
								{@const st = indexerState(idx)}
								{@const hasLimit = idx.query_limit !== undefined && idx.query_limit > 0}
								<tr>
									<td class="font-medium">{idx.name}</td>
									<td>
										{#if hasLimit}
											{idx.used_queries} / {idx.query_limit} ({idx.limits_unit === 1
												? m.prowlarr_unit_hour()
												: m.prowlarr_unit_day()})
										{:else}
											<span>{idx.used_queries}</span> / <span>{m.prowlarr_no_limit()}</span>
										{/if}
									</td>
									<td>
										{#if hasLimit}
											{Math.round((idx.used_queries / (idx.query_limit ?? 1)) * 100)} %
										{:else}
											<span aria-hidden="true">–</span><span class="sr-only"
												>{m.prowlarr_no_limit()}</span
											>
										{/if}
									</td>
									<td>
										<span class="chip chip-{st.level}">
											{st.label}
										</span>
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
		border-color: oklch(0.75 0.16 80);
		color: oklch(0.75 0.16 80);
	}
	.chip-exhausted {
		border-color: var(--color-danger);
		color: var(--color-danger);
	}
	.chip-disabled {
		opacity: 0.75;
	}
</style>
