<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';
	import ApiKeyDialog from './ApiKeyDialog.svelte';

	type CreatedApiKey = components['schemas']['CreatedApiKey'];

	const createKeyMutation = ['api-keys', 'create'] as const;
	const client = useQueryClient();

	const apiKeys = createSentioQuery({
		queryKey: keys.apiKeys,
		queryFn: async () => (await api.GET('/auth/api-keys')).data ?? []
	});

	let name = $state('');
	let createdKey = $state<CreatedApiKey | null>(null);
	let revokingId = $state<string | null>(null);

	const createKey = createSentioMutation({
		mutationKey: createKeyMutation,
		mutationFn: async (keyName: string) => {
			const res = await api.POST('/auth/api-keys', {
				body: { name: keyName }
			});
			return res.data;
		},
		invalidates: [keys.apiKeys],
		onSuccess: (data) => {
			if (data) {
				createdKey = data;
				toast.success(m.api_key_created_success());
			}
			name = '';
		},

		onError: (err) => toast.error(describeError(err))
	});

	const revokeKey = createSentioMutation({
		mutationFn: async (id: string) => {
			await api.DELETE('/auth/api-keys/{id}', {
				params: { path: { id } }
			});
		},
		invalidates: [keys.apiKeys],
		onSuccess: () => {
			toast.success(m.api_key_revoked());
			revokingId = null;
		},
		onError: (err) => toast.error(describeError(err))
	});

	// The secret exists only in the create response: drop it from component state and the
	// mutation cache so nothing can render it again after the dialog closes.
	function closeDialog(): void {
		createdKey = null;
		createKey.reset();
		const cache = client.getMutationCache();
		for (const mutation of cache.findAll({ mutationKey: createKeyMutation }))
			cache.remove(mutation);
	}
</script>

{#if apiKeys.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if apiKeys.isError}
	<ProblemNote error={apiKeys.error} />
{:else if (apiKeys.data ?? []).length === 0}
	<p class="card mb-4">{m.api_keys_empty()}</p>
{:else}
	<div class="relative overflow-x-auto">
		<table class="mb-6 table">
			<caption class="sr-only">{m.api_keys_title()}</caption>
			<thead>
				<tr>
					<th scope="col">{m.api_key_name()}</th>
					<th scope="col">{m.api_key_created()}</th>
					<th scope="col">{m.api_key_last_used()}</th>
					<th scope="col">{m.api_key_expires()}</th>
					<th scope="col"><span class="sr-only">{m.api_key_actions()}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each apiKeys.data ?? [] as key (key.id)}
					<tr>
						<td>{key.name}</td>
						<td>{formatWhen(key.created_at, getLocale())}</td>
						<td>
							{key.last_used_at
								? formatWhen(key.last_used_at, getLocale())
								: m.api_key_never_used()}
						</td>
						<td>
							{key.expires_at ? formatWhen(key.expires_at, getLocale()) : m.api_key_never_expires()}
						</td>
						<td>
							{#if revokingId === key.id}
								<div class="flex flex-col gap-2">
									<p class="text-sm font-medium text-danger">
										{m.api_key_revoke_confirm_prompt({ name: key.name })}
									</p>
									<div class="flex gap-2">
										<button
											type="button"
											class="btn text-danger hover:border-danger"
											disabled={revokeKey.isPending}
											onclick={() => revokeKey.mutate(key.id)}
										>
											{m.api_key_revoke_confirm_btn()}
										</button>
										<button
											type="button"
											class="btn"
											disabled={revokeKey.isPending}
											onclick={() => {
												revokingId = null;
											}}
										>
											{m.api_key_cancel()}
										</button>
									</div>
								</div>
							{:else}
								<button
									type="button"
									class="btn"
									disabled={revokeKey.isPending}
									onclick={() => {
										revokingId = key.id;
									}}
								>
									{m.api_key_revoke()}
								</button>
							{/if}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<h3 class="mt-6 mb-3 text-lg font-semibold">{m.api_key_create()}</h3>
<form
	class="card flex max-w-xl flex-col gap-4"
	onsubmit={(e) => {
		e.preventDefault();
		const trimmed = name.trim();
		if (trimmed) createKey.mutate(trimmed);
	}}
>
	<Field id="key-name" label={m.api_key_name()}>
		<input id="key-name" class="input" required minlength="1" maxlength="100" bind:value={name} />
	</Field>
	<div>
		<button type="submit" class="btn btn-primary" disabled={createKey.isPending}>
			{m.api_key_create()}
		</button>
	</div>
</form>

{#if createdKey}
	<ApiKeyDialog secret={createdKey.secret} name={createdKey.name} onclose={closeDialog} />
{/if}
