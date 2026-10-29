<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import AppLogo from '#lib/components/AppLogo.svelte';
	import CheckState from '#lib/components/CheckState.svelte';
	import ConfirmDelete from '#lib/components/ConfirmDelete.svelte';
	import Field from '#lib/components/Field.svelte';
	import KindSelect from '#lib/components/KindSelect.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import { entityHref } from '#lib/entities.js';
	import { HELP_MAP } from '#lib/help.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import ResetMemory from '#lib/components/ResetMemory.svelte';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import UrlText from '#lib/components/UrlText.svelte';
	import type { AppKind } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import { instanceKindLabel, probeMessage } from '#lib/products.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	let kind = $state<AppKind>('sonarr');
	let name = $state('');
	let baseUrl = $state('');
	let apiKey = $state('');

	const create = createSentioMutation({
		mutationFn: async () => {
			const res = await api.POST('/instances', {
				body: { kind, name, base_url: baseUrl, api_key: apiKey, enabled: true }
			});
			return res.data;
		},
		invalidates: [keys.instances, keys.caps],
		onSuccess: () => {
			toast.success(name);
			name = '';
			baseUrl = '';
			apiKey = '';
		},
		onError: (err) => toast.error(describeError(err))
	});

	const probe = createSentioMutation({
		mutationFn: async () =>
			(
				await api.POST('/instances/test', {
					body: { kind, name: name || 'probe', base_url: baseUrl, api_key: apiKey, enabled: true }
				})
			).data,
		onSuccess: (r) => toast.success(probeMessage(r)),
		onError: (err) => toast.error(describeError(err))
	});

	const remove = createSentioMutation({
		mutationFn: async (id: string) => {
			await api.DELETE('/instances/{instanceId}', { params: { path: { instanceId: id } } });
		},
		invalidates: [keys.instances, keys.caps],
		onError: (err) => toast.error(describeError(err))
	});
</script>

<PageHeader title={m.nav_instances()} help={HELP_MAP['/instances']} />

{#if instances.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if instances.isError}
	<ProblemNote error={instances.error} />
{:else}
	{#if (instances.data ?? []).length > 0}
		<ul class="space-y-3 md:hidden" aria-label={m.nav_instances()}>
			{#each instances.data ?? [] as inst (inst.id)}
				<li class="card target flex flex-col gap-2">
					<div class="flex items-start justify-between gap-2">
						<div class="flex flex-col gap-0.5">
							<div class="flex items-center gap-2">
								<AppLogo kind={inst.kind} size={18} />
								<a
									class="target-link font-semibold underline"
									href={entityHref('instance', inst.id)}>{inst.name}</a
								>
							</div>
							<div class="flex flex-wrap items-center gap-1.5 text-xs text-subtle">
								<span
									><span class="sr-only">{m.instance_kind()}: </span>{instanceKindLabel(
										inst.kind,
										inst.last_seen_product
									)}</span
								>
								<span aria-hidden="true">·</span>
								<span><span class="sr-only">{m.instance_source()}: </span>{inst.source}</span>
							</div>
						</div>
						{#if inst.enabled}
							<StatusIcon status="ok" label={m.instance_enabled()} size={16} />
						{:else}
							<StatusIcon status="disabled" label={m.instance_disabled()} size={16} />
						{/if}
					</div>
					<div class="text-sm">
						<span class="sr-only">{m.instance_url()}: </span>
						<UrlText url={inst.base_url} />
					</div>
					<div class="text-sm">
						<CheckState instance={inst} />
					</div>
					{#if session.isAdmin}
						<div class="mt-1 flex">
							<ConfirmDelete
								name={inst.name}
								disabled={inst.source === 'configarr' || remove.isPending}
								onconfirm={() => remove.mutate(inst.id)}
							/>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}

	<div class="relative hidden w-full overflow-x-auto rounded-lg border border-line md:block">
		<table class="compact-table table w-full">
			<caption class="sr-only">{m.nav_instances()}</caption>
			<thead>
				<tr>
					<th scope="col">{m.instance_name()}</th>
					<th scope="col">{m.instance_kind()}</th>
					<th scope="col">{m.instance_url()}</th>
					<th scope="col">{m.instance_source()}</th>
					<th scope="col">{m.instance_enabled()}</th>
					<th scope="col">{m.instance_last_check()}</th>
					{#if session.isAdmin}
						<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
					{/if}
				</tr>
			</thead>
			<tbody>
				{#each instances.data ?? [] as inst (inst.id)}
					<tr class="target">
						<td>
							<div class="flex items-center gap-2">
								<AppLogo kind={inst.kind} size={18} />
								<a class="target-link underline" href={entityHref('instance', inst.id)}
									>{inst.name}</a
								>
							</div>
						</td>
						<td>{instanceKindLabel(inst.kind, inst.last_seen_product)}</td>
						<td><UrlText url={inst.base_url} /></td>
						<td>{inst.source}</td>
						<td>
							{#if inst.enabled}
								<StatusIcon status="ok" label={m.instance_enabled()} size={16} />
							{:else}
								<StatusIcon status="disabled" label={m.instance_disabled()} size={16} />
							{/if}
						</td>
						<td><CheckState instance={inst} /></td>
						{#if session.isAdmin}
							<td>
								<ConfirmDelete
									name={inst.name}
									disabled={inst.source === 'configarr' || remove.isPending}
									onconfirm={() => remove.mutate(inst.id)}
								/>
							</td>
						{/if}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

{#if session.isAdmin}
	<div class="mt-8 grid gap-6 xl:grid-cols-12">
		<section class="card xl:col-span-8" aria-labelledby="add-instance">
			<h2 id="add-instance" class="mb-4 text-lg font-semibold">{m.instance_add()}</h2>
			<form
				class="grid gap-4 sm:grid-cols-2"
				onsubmit={(e) => {
					e.preventDefault();
					create.mutate();
				}}
			>
				<div class="sm:col-span-2">
					<KindSelect bind:value={kind} />
				</div>
				<Field id="name" label={m.instance_name()}>
					<input id="name" class="input" required maxlength="64" bind:value={name} />
				</Field>
				<Field id="base_url" label={m.instance_url()} hint="http://sonarr:8989">
					<input
						id="base_url"
						class="input"
						required
						inputmode="url"
						bind:value={baseUrl}
						aria-describedby="base_url-hint"
					/>
				</Field>
				<div class="sm:col-span-2">
					<Field id="api_key" label={m.instance_api_key()}>
						<input
							id="api_key"
							class="input"
							type="password"
							autocomplete="off"
							required
							minlength="8"
							bind:value={apiKey}
						/>
					</Field>
				</div>
				<div class="flex gap-2 sm:col-span-2">
					<button
						type="button"
						class="btn"
						onclick={() => probe.mutate()}
						disabled={probe.isPending}>{m.instance_test()}</button
					>
					<button type="submit" class="btn btn-primary" disabled={create.isPending}
						>{m.instance_save()}</button
					>
				</div>
			</form>
		</section>

		<div class="xl:col-span-4">
			<ResetMemory />
		</div>
	</div>
{/if}
