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

	type CleanuparrLink = components['schemas']['CleanuparrLink'];

	// SnatchArr only reads from Cleanuparr (ADR-0007): this page manages SnatchArr's own link
	// record; every call below goes to SnatchArr's API, none changes anything in Cleanuparr.
	const links = createSentioQuery({
		queryKey: keys.cleanuparrLinks,
		queryFn: async () => (await api.GET('/cleanuparr')).data ?? []
	});
	const status = createSentioQuery({
		queryKey: keys.cleanuparr,
		queryFn: async () => (await api.GET('/cleanuparr/status')).data ?? [],
		refetchInterval: 60_000
	});

	/** The link in the form; null adds a new one. */
	let editing = $state<CleanuparrLink | null>(null);
	/** Bumped to remount an emptied create form after a save. */
	let formGeneration = $state(0);

	const save = createSentioMutation({
		mutationFn: async (body: LinkBody) => {
			const target = editing;
			const res = target
				? await api.PUT('/cleanuparr/{linkId}', { params: { path: { linkId: target.id } }, body })
				: await api.POST('/cleanuparr', { body });
			return res.data;
		},
		invalidates: [keys.cleanuparrLinks, keys.cleanuparr],
		onSuccess: (link) => {
			toast.success(m.form_saved({ name: link?.name ?? '' }));
			closeForm();
		}
	});

	const remove = createSentioMutation({
		mutationFn: async (link: CleanuparrLink) => {
			await api.DELETE('/cleanuparr/{linkId}', { params: { path: { linkId: link.id } } });
			return link;
		},
		invalidates: [keys.cleanuparrLinks, keys.cleanuparr],
		onSuccess: (link) => {
			toast.success(m.form_deleted({ name: link.name }));
			if (editing?.id === link.id) closeForm();
		},
		onError: (err) => toast.error(describeError(err))
	});

	const probe = createSentioMutation({
		mutationFn: async (linkId: string) =>
			(await api.POST('/cleanuparr/{linkId}/test', { params: { path: { linkId } } })).data,
		invalidates: [keys.cleanuparrLinks],
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => toast.error(m.probe_failed({ detail: describeError(err) }))
	});

	const probeInput = createSentioMutation({
		mutationFn: async (body: LinkBody) => (await api.POST('/cleanuparr/test', { body })).data,
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => toast.error(m.probe_failed({ detail: describeError(err) }))
	});

	function closeForm(): void {
		editing = null;
		formGeneration += 1;
		save.reset();
	}

	async function edit(link: CleanuparrLink): Promise<void> {
		save.reset();
		editing = link;
		await tick();
		document.getElementById('cleanuparr-name')?.focus();
	}

	function seekerStateLabel(state: components['schemas']['CleanuparrSeekerState']): string {
		switch (state) {
			case 'active':
				return m.cleanuparr_seeker_state_active();
			case 'inactive':
				return m.cleanuparr_seeker_state_inactive();
			case 'dry_run':
				return m.cleanuparr_seeker_state_dry_run();
			case 'unknown':
			default:
				return m.cleanuparr_seeker_state_unknown();
		}
	}
</script>

<h1 class="mb-2 text-2xl font-semibold">{m.cleanuparr_title()}</h1>
<p class="mb-4 text-sm text-subtle">{m.cleanuparr_readonly_hint()}</p>

<h2 class="mb-3 text-lg font-semibold">{m.cleanuparr_links_title()}</h2>
{#if links.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if links.isError}
	<ProblemNote error={links.error} />
{:else if (links.data ?? []).length === 0}
	<p class="card mb-6">{m.cleanuparr_links_empty()}</p>
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
		idPrefix="cleanuparr"
		link={editing}
		pending={save.isPending}
		error={save.error}
		onsave={(body) => save.mutate(body)}
		oncancel={closeForm}
		ontest={(body) => probeInput.mutate(body)}
		testing={probeInput.isPending}
	/>
{/key}

<h2 class="mt-8 mb-3 text-lg font-semibold">{m.cleanuparr_status_title()}</h2>
{#if status.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if status.isError}
	<ProblemNote error={status.error} />
{:else if (status.data ?? []).length === 0}
	<p class="card">{m.cleanuparr_empty()}</p>
{:else}
	{#each status.data ?? [] as link (link.link_id)}
		<section class="card mb-4" aria-labelledby={`cleanuparr-${link.link_id}`}>
			<h3 id={`cleanuparr-${link.link_id}`} class="font-semibold">
				{link.name} <span class="text-sm text-subtle">{link.version ?? ''}</span>
			</h3>
			<p class="text-sm" role="status">
				{link.reachable ? m.dlclients_reachable() : m.dlclients_unreachable()}
				{#if link.error}<span class="text-danger"> {link.error}</span>{/if}
			</p>
			<p class="text-sm">
				{m.cleanuparr_struck({ n: link.struck_downloads })} · {m.cleanuparr_marked({
					n: link.marked_for_removal
				})}
			</p>
			{#if link.recent_strikes.length > 0}
				<ul class="mt-2 text-sm">
					{#each link.recent_strikes as s (s.id)}
						<li>
							<span class="text-subtle">{formatWhen(s.created_at, getLocale())}</span>
							{s.type} · {s.title}
						</li>
					{/each}
				</ul>
			{/if}
			{#if link.seeker}
				<div class="mt-4 border-t border-line pt-3">
					<h4 class="text-sm font-semibold">{m.cleanuparr_seeker_title()}</h4>
					<p class="text-sm text-subtle">
						{#if link.seeker.reason}
							{m.cleanuparr_seeker_status_reason({
								state: seekerStateLabel(link.seeker.state),
								reason: link.seeker.reason
							})}
						{:else}
							{m.cleanuparr_seeker_status({ state: seekerStateLabel(link.seeker.state) })}
						{/if}
					</p>
					{#if link.seeker.instances && link.seeker.instances.length > 0}
						<table class="mt-2 table">
							<caption class="sr-only">{m.cleanuparr_seeker_instances_title()}</caption>
							<thead>
								<tr>
									<th scope="col">{m.cleanuparr_seeker_col_instance()}</th>
									<th scope="col">{m.cleanuparr_seeker_col_type()}</th>
									<th scope="col">{m.cleanuparr_seeker_col_status()}</th>
									<th scope="col">{m.cleanuparr_seeker_col_overlap()}</th>
								</tr>
							</thead>
							<tbody>
								{#each link.seeker.instances as inst (inst.name)}
									<tr>
										<td>{inst.name}</td>
										<td>{inst.type}</td>
										<td>
											{#if inst.active}
												<span>{m.cleanuparr_seeker_state_active()}</span>
											{:else}
												<span class="text-subtle">{m.cleanuparr_seeker_state_inactive()}</span>
											{/if}
										</td>
										<td>
											{#if inst.matched}
												<span class="font-medium text-danger">{m.cleanuparr_seeker_matched()}</span>
											{:else}
												<span class="text-subtle">{m.cleanuparr_seeker_unmatched()}</span>
											{/if}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{/if}
				</div>
			{/if}
		</section>
	{/each}
{/if}
