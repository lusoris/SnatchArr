<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import CheckState from '#lib/components/CheckState.svelte';
	import Field from '#lib/components/Field.svelte';
	import KindSelect from '#lib/components/KindSelect.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
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
	<div class="relative w-full overflow-x-auto rounded-lg border border-line">
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
					<tr>
						<td>
							<div class="flex items-center gap-2">
								<a class="underline" href={resolve(`instances/${inst.id}`)}>{inst.name}</a>
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
								<button
									type="button"
									class="btn"
									onclick={() => remove.mutate(inst.id)}
									disabled={inst.source === 'configarr'}>{m.instance_delete()}</button
								>
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

<style>
	.compact-table tbody tr {
		height: 2.75rem;
	}
	@media (min-width: 1280px) {
		.compact-table tbody tr {
			height: 2.25rem;
		}
		.compact-table :global(.btn) {
			min-height: 1.75rem;
			padding: 0.15rem 0.5rem;
			font-size: 0.75rem;
		}
	}
	.compact-table th,
	.compact-table td {
		vertical-align: middle;
	}
</style>
