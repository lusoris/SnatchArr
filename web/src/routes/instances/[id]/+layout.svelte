<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { problemStatus } from '#lib/api/client.js';
	import AppLogo from '#lib/components/AppLogo.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import UrlText from '#lib/components/UrlText.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { instanceQuery } from '#lib/components/instance/instance-query.svelte.js';
	import { getInstanceTabs, isTabActive } from '#lib/components/instance/tabs.js';
	import { m } from '#lib/paraglide/messages.js';
	import { instanceKindLabel } from '#lib/products.js';

	const { children }: { children: Snippet } = $props();

	const instanceId = $derived(page.params.id ?? '');
	const instance = instanceQuery(() => instanceId);

	const isNotFound = $derived(
		(instance.isError && problemStatus(instance.error) === 404) ||
			(instance.isSuccess && !instance.data)
	);

	const tabs = $derived(getInstanceTabs(instanceId, instance.data?.kind));

	const currentHelp = $derived.by(() => {
		const path = page.url.pathname;
		if (path.endsWith('/policy')) return HELP_MAP['/instances/[id]/policy'];
		if (path.endsWith('/queue')) return HELP_MAP['/instances/[id]/queue'];
		if (path.endsWith('/activity')) return HELP_MAP['/instances/[id]/activity'];
		if (path.endsWith('/memory')) return HELP_MAP['/instances/[id]/memory'];
		return HELP_MAP['/instances/[id]'];
	});
</script>

{#key page.params.id}
	{#if instance.isPending}
		<div role="status" aria-label={m.instance_loading()} class="flex animate-pulse flex-col gap-4">
			<div class="h-8 w-64 rounded bg-subtle/20"></div>
			<div class="h-4 w-96 rounded bg-subtle/20"></div>
			<span class="sr-only">{m.instance_loading()}</span>
		</div>
	{:else if isNotFound}
		<div class="card">
			<h1 class="mb-2 text-2xl font-semibold">{m.instance_not_found_title()}</h1>
			<p class="mb-4 text-sm text-subtle">{m.instance_not_found_text()}</p>
			<a href={resolve('/instances')} class="btn btn-primary">{m.instance_not_found_back()}</a>
		</div>
	{:else if instance.isError}
		<ProblemNote error={instance.error} />
	{:else if instance.data}
		{@const inst = instance.data}
		<PageHeader title={inst.name} sticky help={currentHelp}>
			{#snippet status()}
				<span
					class="inline-flex items-center gap-1.5 rounded-full border border-line px-2.5 py-0.5 text-xs text-subtle"
				>
					<AppLogo kind={inst.kind} size={16} />
					{instanceKindLabel(inst.kind, inst.last_seen_product)}
				</span>
			{/snippet}
			{#snippet actions()}
				<span class="text-sm text-subtle">
					<UrlText url={inst.base_url} />
				</span>
			{/snippet}
			<nav aria-label={m.instance_tabs_label()} class="subtab-bar">
				{#each tabs as tab (tab.key)}
					{@const active = isTabActive(page.url.pathname, tab.href, tab.key === 'overview')}
					{#if tab.key === 'overview'}
						<a
							href={resolve('/instances/[id]', { id: instanceId })}
							class="subtab-link"
							aria-current={active ? 'page' : undefined}
						>
							{tab.label}
						</a>
					{:else if tab.key === 'policy'}
						<a
							href={resolve('/instances/[id]/policy', { id: instanceId })}
							class="subtab-link"
							aria-current={active ? 'page' : undefined}
						>
							{tab.label}
						</a>
					{:else if tab.key === 'queue'}
						<a
							href={resolve('/instances/[id]/queue', { id: instanceId })}
							class="subtab-link"
							aria-current={active ? 'page' : undefined}
						>
							{tab.label}
						</a>
					{:else if tab.key === 'activity'}
						<a
							href={resolve('/instances/[id]/activity', { id: instanceId })}
							class="subtab-link"
							aria-current={active ? 'page' : undefined}
						>
							{tab.label}
						</a>
					{:else}
						<a
							href={resolve('/instances/[id]/memory', { id: instanceId })}
							class="subtab-link"
							aria-current={active ? 'page' : undefined}
						>
							{tab.label}
						</a>
					{/if}
				{/each}
			</nav>
		</PageHeader>

		{@render children()}
	{/if}
{/key}
