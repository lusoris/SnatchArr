<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ConfirmDelete from '#lib/components/ConfirmDelete.svelte';
	import FormCard from '#lib/components/FormCard.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import SecretField from '#lib/components/SecretField.svelte';
	import TextField from '#lib/components/TextField.svelte';
	import UrlText from '#lib/components/UrlText.svelte';
	import { FormEditor } from '#lib/formEditor.svelte.js';
	import { deleteCallbacks, saveSuccess } from '#lib/forms.js';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { fieldErrors, filledPart } from '#lib/problem.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';
	import { formatWhen } from '#lib/stamina.js';

	type AdultSource = components['schemas']['AdultSource'];
	type AdultSourceInput = components['schemas']['AdultSourceInput'];

	const sources = createSentioQuery({
		queryKey: keys.adultSources,
		queryFn: async () => (await api.GET('/adult/sources')).data ?? []
	});

	const syncStatus = createSentioQuery({
		queryKey: keys.adultSyncStatus,
		queryFn: async () => (await api.GET('/adult/sync/status')).data
	});

	let name = $state('');
	let endpoint = $state('');
	let apiKey = $state('');
	let priority = $state(0);
	let enabled = $state(true);
	let syncFavorites = $state(true);

	const editor = new FormEditor<AdultSource>('adult-source-name', () => save.reset());

	function loadEdit(src: AdultSource): void {
		name = src.name;
		endpoint = src.endpoint;
		apiKey = '';
		priority = src.priority;
		enabled = src.enabled;
		syncFavorites = src.sync_favorites;
		editor.open(src);
	}

	function resetForm(): void {
		name = '';
		endpoint = '';
		apiKey = '';
		priority = 0;
		enabled = true;
		syncFavorites = true;
		editor.close();
	}

	function buildBody(): AdultSourceInput {
		return {
			name,
			endpoint,
			priority: Number(priority),
			enabled,
			sync_favorites: syncFavorites,
			...filledPart('api_key', apiKey)
		};
	}

	const save = createSentioMutation({
		mutationFn: async () => {
			const target = editor.editing;
			const body = buildBody();
			const res = target
				? await api.PUT('/adult/sources/{sourceId}', {
						params: { path: { sourceId: target.id } },
						body
					})
				: await api.POST('/adult/sources', { body });
			return res.data;
		},
		invalidates: [keys.adultSources, keys.adultSyncStatus],
		onSuccess: saveSuccess(resetForm)
	});

	const remove = createSentioMutation({
		mutationFn: async (src: AdultSource) => {
			await api.DELETE('/adult/sources/{sourceId}', {
				params: { path: { sourceId: src.id } }
			});
			return src;
		},
		invalidates: [keys.adultSources, keys.adultSyncStatus],
		...deleteCallbacks(() => editor.editing, resetForm)
	});

	const syncNow = createSentioMutation({
		mutationFn: async () => (await api.POST('/adult/sync')).data,
		invalidates: [keys.adultSources, keys.adultSyncStatus],
		onSuccess: (summary) => {
			if (summary) {
				toast.success(
					m.adult_sources_synced_toast({
						sources: summary.sources_synced,
						performers: summary.performers_synced,
						studios: summary.studios_synced
					})
				);
			}
		},
		onError: (err) => toast.error(describeError(err))
	});

	const errors = $derived(fieldErrors(save.error));
</script>

<PageHeader
	title={m.adult_sources_title()}
	description={m.adult_sources_subtitle()}
	documentTitle
	help={HELP_MAP['/adult/sources']}
>
	{#snippet actions()}
		{#if session.isAdmin}
			<button
				type="button"
				class="btn"
				onclick={() => syncNow.mutate()}
				disabled={syncNow.isPending}
			>
				{syncNow.isPending ? m.adult_sources_syncing() : m.adult_sources_sync_now()}
			</button>
		{/if}
	{/snippet}
</PageHeader>

<section aria-labelledby="adult-sync-status-title" class="card mb-6 w-full">
	<h2 id="adult-sync-status-title" class="mb-2 text-lg font-semibold">
		{m.adult_sources_status_title()}
	</h2>
	<QueryGuard query={syncStatus}>
		{#snippet children(st)}
			<ul class="flex flex-wrap items-center gap-4 text-sm text-subtle">
				<li>
					{m.adult_sources_status_sources({
						enabled: st.enabled_sources,
						total: st.total_sources
					})}
				</li>
				<li>
					{m.adult_sources_status_counts({
						performers: st.performers_synced,
						studios: st.studios_synced
					})}
				</li>
				<li>
					{st.last_sync_at
						? m.adult_sources_status_last_sync({
								when: formatWhen(st.last_sync_at, getLocale())
							})
						: m.adult_sources_status_never()}
				</li>
				{#if st.last_error}
					<li class="text-danger">{st.last_error}</li>
				{/if}
			</ul>
		{/snippet}
	</QueryGuard>
</section>

<section aria-labelledby="adult-sources-list-title" class="w-full">
	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-3 xl:col-span-7">
			<h2 id="adult-sources-list-title" class="text-lg font-semibold">
				{m.adult_sources_title()}
			</h2>
			<QueryGuard query={sources}>
				{#snippet children(items)}
					{#if items.length === 0}
						<p class="card">{m.adult_sources_none()}</p>
					{:else}
						<ul class="grid grid-cols-1 gap-3 xl:grid-cols-2">
							{#each items as src (src.id)}
								<li class="card flex flex-col justify-between">
									<div class="flex flex-col gap-1.5">
										<div class="flex flex-wrap items-center gap-2">
											<h3 class="font-semibold">{src.name}</h3>
											<span class="text-xs text-subtle">
												{m.adult_sources_priority_badge({ priority: src.priority })}
											</span>
											<span class="text-xs text-subtle">
												{src.enabled ? m.instance_enabled() : m.instance_disabled()}
											</span>
										</div>
										<p class="text-sm text-subtle"><UrlText url={src.endpoint} /></p>
										<p class="text-xs text-subtle">
											{src.last_sync_at
												? m.adult_sources_source_last_sync({
														when: formatWhen(src.last_sync_at, getLocale())
													})
												: m.adult_sources_source_never()}
										</p>
										{#if src.last_sync_error}
											<p class="text-xs text-danger">
												{m.adult_sources_source_error({ error: src.last_sync_error })}
											</p>
										{/if}
									</div>
									{#if session.isAdmin}
										<div
											class="mt-3 flex flex-wrap items-center gap-2 border-t border-line/40 pt-2"
										>
											<button type="button" class="btn" onclick={() => loadEdit(src)}>
												{m.action_edit()}
											</button>
											<ConfirmDelete
												name={src.name}
												disabled={remove.isPending}
												onconfirm={() => remove.mutate(src)}
											/>
										</div>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}
				{/snippet}
			</QueryGuard>
		</div>

		{#if session.isAdmin}
			<div class="xl:col-span-5">
				<FormCard
					id="adult-source-form-title"
					title={editor.editing
						? m.form_edit_title({ name: editor.editing.name })
						: m.adult_sources_add()}
					editing={editor.editing !== null}
					pending={save.isPending}
					error={save.error}
					onsubmit={() => save.mutate()}
					oncancel={resetForm}
				>
					<TextField
						id="adult-source-name"
						label={m.adult_sources_name()}
						required
						maxlength={128}
						error={errors.name}
						bind:value={name}
					/>
					<TextField
						id="adult-source-endpoint"
						label={m.adult_sources_endpoint()}
						placeholder={m.adult_sources_endpoint_placeholder()}
						required
						inputmode="url"
						error={errors.endpoint}
						bind:value={endpoint}
					/>
					<SecretField
						id="adult-source-api-key"
						label={m.adult_sources_api_key()}
						editing={editor.editing !== null}
						requiredOnCreate
						error={errors.api_key}
						bind:value={apiKey}
					/>
					<TextField
						id="adult-source-priority"
						label={m.adult_sources_priority()}
						type="number"
						min={0}
						max={1000}
						step={1}
						error={errors.priority}
						bind:value={priority}
					/>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" bind:checked={enabled} />
						<span>{m.form_enabled()}</span>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" bind:checked={syncFavorites} />
						<span>{m.adult_sources_sync_favorites()}</span>
					</label>
				</FormCard>
			</div>
		{/if}
	</div>
</section>
