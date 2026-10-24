<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import CheckState from '#lib/components/CheckState.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import StaminaGauge from '#lib/components/StaminaGauge.svelte';
	import UrlText from '#lib/components/UrlText.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { instanceKindLabel } from '#lib/products.js';
	import { countExhausted, countHeadroom } from '#lib/prowlarr.js';
	import { keys } from '#lib/query.js';

	type Instance = components['schemas']['Instance'];
	type Cap = components['schemas']['CapStatus'];

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});
	const caps = createSentioQuery({
		queryKey: keys.caps,
		queryFn: async () => (await api.GET('/hourly-caps')).data ?? [],
		refetchInterval: 30_000
	});
	const prowlarr = createSentioQuery({
		queryKey: keys.prowlarr,
		queryFn: async () => (await api.GET('/prowlarr/status')).data ?? [],
		refetchInterval: 30_000
	});
	const quickie = createSentioMutation({
		mutationFn: async (v: { id: string; name: string; kind: 'missing' | 'upgrade' }) => {
			await api.POST('/instances/{instanceId}/runs', {
				params: { path: { instanceId: v.id } },
				body: { kind: v.kind }
			});
			return v.name;
		},
		invalidates: [keys.runs, keys.caps],
		onSuccess: (name) => toast.success(m.quickie_queued({ name })),
		onError: (err) => toast.error(describeError(err))
	});

	function capFor(inst: Instance, list: Cap[] | undefined): Cap | undefined {
		return list?.find((c) => c.scope === 'instance' && c.instance_id === inst.id);
	}
	const globalCap = $derived(caps.data?.find((c) => c.scope === 'global'));
</script>

<PageHeader title={m.dashboard_title()} />

{#if instances.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if instances.isError}
	<ProblemNote error={instances.error} />
{:else if (instances.data ?? []).length === 0}
	<p class="card">
		{m.empty_instances()} <a class="underline" href={resolve('instances')}>{m.nav_instances()}</a>
	</p>
{:else}
	{#if globalCap}
		<section class="card mb-4" aria-labelledby="global-stamina">
			<h2 id="global-stamina" class="sr-only">{m.stamina_global()}</h2>
			<StaminaGauge label={m.stamina_global()} used={globalCap.used} cap={globalCap.cap} />
		</section>
	{/if}
	{#if (prowlarr.data ?? []).length > 0}
		<section class="card mb-4" aria-labelledby="dashboard-prowlarr-title">
			<h2 id="dashboard-prowlarr-title" class="mb-2 text-base font-semibold">
				<a href={resolve('prowlarr')} class="hover:underline">{m.prowlarr_dashboard_title()}</a>
			</h2>
			<ul class="flex flex-col gap-3">
				{#each prowlarr.data ?? [] as link (link.link_id)}
					{@const total = link.indexers.length}
					{@const exhausted = countExhausted(link.indexers)}
					{@const headroom = countHeadroom(link.indexers)}
					<li class="flex flex-col gap-1">
						<div
							class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-sm"
							data-label-value
						>
							<span class="font-medium">{link.name}</span>
							<span class="text-subtle">
								{m.prowlarr_headroom_count({ count: headroom, total })} · {m.prowlarr_exhausted_count(
									{
										count: exhausted
									}
								)}
							</span>
						</div>
						<div
							role="meter"
							aria-label={m.prowlarr_headroom_label({ name: link.name })}
							aria-valuemin={0}
							aria-valuemax={total}
							aria-valuenow={headroom}
							aria-valuetext={`${headroom} / ${total}`}
							class="h-2 w-full overflow-hidden rounded-full border border-line bg-surface"
						>
							<div
								class="h-full rounded-full"
								style:background="var(--color-brand)"
								style:width={`${total > 0 ? Math.round((headroom / total) * 100) : 0}%`}
							></div>
						</div>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
	<ul class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
		{#each instances.data ?? [] as inst (inst.id)}
			{@const cap = capFor(inst, caps.data)}
			<li class="card flex flex-col gap-3">
				<div class="flex items-start justify-between gap-2">
					<div class="min-w-0">
						<h2 class="text-lg font-semibold">
							<a class="hover:underline" href={resolve(`instances/${inst.id}`)}>{inst.name}</a>
						</h2>
						<p class="text-sm text-subtle">
							{instanceKindLabel(inst.kind, inst.last_seen_product)} · <UrlText
								url={inst.base_url}
							/>
						</p>
					</div>
					<span class="shrink-0 rounded-full border border-line px-2 py-0.5 text-xs"
						>{inst.enabled ? m.instance_enabled() : m.instance_disabled()}</span
					>
				</div>
				{#if cap}
					<StaminaGauge label={m.stamina()} used={cap.used} cap={cap.cap} />
				{/if}
				<CheckState instance={inst} />
				<div class="flex flex-wrap gap-2">
					<button
						type="button"
						class="btn btn-primary"
						disabled={quickie.isPending}
						onclick={() => quickie.mutate({ id: inst.id, name: inst.name, kind: 'missing' })}
						>{m.quickie_missing()}</button
					>
					<button
						type="button"
						class="btn"
						disabled={quickie.isPending}
						onclick={() => quickie.mutate({ id: inst.id, name: inst.name, kind: 'upgrade' })}
						>{m.quickie_upgrade()}</button
					>
				</div>
			</li>
		{/each}
	</ul>
{/if}
