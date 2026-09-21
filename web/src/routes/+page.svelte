<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '$lib/api/client.js';
	import type { components } from '$lib/api/schema.js';
	import ProblemNote from '$lib/components/ProblemNote.svelte';
	import StaminaGauge from '$lib/components/StaminaGauge.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { keys } from '$lib/query.js';
	import { formatWhen } from '$lib/stamina.js';

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

<h1 class="mb-4 text-2xl font-semibold">{m.dashboard_title()}</h1>

{#if instances.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if instances.isError}
	<ProblemNote error={instances.error} />
{:else if (instances.data ?? []).length === 0}
	<p class="card">
		{m.empty_instances()} <a class="underline" href={resolve('/instances')}>{m.nav_instances()}</a>
	</p>
{:else}
	{#if globalCap}
		<section class="card mb-4" aria-labelledby="global-stamina">
			<h2 id="global-stamina" class="sr-only">{m.stamina_global()}</h2>
			<StaminaGauge label={m.stamina_global()} used={globalCap.used} cap={globalCap.cap} />
		</section>
	{/if}
	<ul class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
		{#each instances.data ?? [] as inst (inst.id)}
			{@const cap = capFor(inst, caps.data)}
			<li class="card flex flex-col gap-3">
				<div class="flex items-start justify-between gap-2">
					<div>
						<h2 class="text-lg font-semibold">
							<a class="hover:underline" href={resolve(`/instances/${inst.id}`)}>{inst.name}</a>
						</h2>
						<p class="text-sm text-subtle">{inst.kind} · {inst.base_url}</p>
					</div>
					<span class="rounded-full border border-line px-2 py-0.5 text-xs"
						>{inst.enabled ? m.instance_enabled() : 'off'}</span
					>
				</div>
				{#if cap}
					<StaminaGauge label={m.stamina()} used={cap.used} cap={cap.cap} />
				{/if}
				{#if inst.last_error}
					<p role="status" class="text-sm text-danger">{inst.last_error}</p>
				{:else if inst.last_check_at}
					<p class="text-xs text-subtle">
						{m.instance_last_check()}: {formatWhen(inst.last_check_at, getLocale())} · {inst.last_seen_version ??
							''}
					</p>
				{/if}
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
