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
	import Field from '$lib/components/Field.svelte';
	import ProblemNote from '$lib/components/ProblemNote.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { keys } from '$lib/query.js';

	type AppKind = components['schemas']['AppKind'];
	const kinds: AppKind[] = ['sonarr', 'radarr', 'lidarr', 'readarr', 'whisparr_v2', 'whisparr_v3'];

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
		onSuccess: (r) =>
			toast.success(m.instance_probe_ok({ app: r?.app_name ?? '', version: r?.version ?? '' })),
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

<h1 class="mb-4 text-2xl font-semibold">{m.nav_instances()}</h1>

{#if instances.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if instances.isError}
	<ProblemNote error={instances.error} />
{:else}
	<table class="mb-6 table">
		<caption class="sr-only">{m.nav_instances()}</caption>
		<thead>
			<tr>
				<th scope="col">{m.instance_name()}</th>
				<th scope="col">{m.instance_kind()}</th>
				<th scope="col">{m.instance_url()}</th>
				<th scope="col">{m.instance_source()}</th>
				<th scope="col">{m.instance_enabled()}</th>
				<th scope="col"><span class="sr-only">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each instances.data ?? [] as inst (inst.id)}
				<tr>
					<td><a class="underline" href={resolve(`/instances/${inst.id}`)}>{inst.name}</a></td>
					<td>{inst.kind}</td>
					<td class="break-all">{inst.base_url}</td>
					<td>{inst.source}</td>
					<td>{inst.enabled ? '✓' : '–'}</td>
					<td>
						<button
							type="button"
							class="btn"
							onclick={() => remove.mutate(inst.id)}
							disabled={inst.source === 'configarr'}>{m.instance_delete()}</button
						>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
{/if}

<section class="card max-w-xl" aria-labelledby="add-instance">
	<h2 id="add-instance" class="mb-3 text-lg font-semibold">{m.instance_add()}</h2>
	<form
		class="flex flex-col gap-3"
		onsubmit={(e) => {
			e.preventDefault();
			create.mutate();
		}}
	>
		<Field id="kind" label={m.instance_kind()}>
			<select id="kind" class="select" bind:value={kind}>
				{#each kinds as k (k)}
					<option value={k}>{k}</option>
				{/each}
			</select>
		</Field>
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
		<div class="flex gap-2">
			<button type="button" class="btn" onclick={() => probe.mutate()} disabled={probe.isPending}
				>{m.instance_test()}</button
			>
			<button type="submit" class="btn btn-primary" disabled={create.isPending}
				>{m.instance_save()}</button
			>
		</div>
	</form>
</section>
