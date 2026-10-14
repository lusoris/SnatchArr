<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import FormCard from '#lib/components/FormCard.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import TextField from '#lib/components/TextField.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { fieldErrors, filledPart } from '#lib/problem.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

	type ImportResult = components['schemas']['ConfigarrImportResult'];

	const status = createSentioQuery({
		queryKey: keys.configarr,
		queryFn: async () => (await api.GET('/configarr/status')).data
	});

	let configPath = $state('');
	let secretsPath = $state('');

	const runImport = createSentioMutation({
		mutationFn: async () => {
			// Without a config file the API re-imports the linked configuration.
			const body = {
				...filledPart('config_path', configPath),
				...filledPart('secrets_path', secretsPath)
			};
			const res = await api.POST('/configarr/import', Object.keys(body).length ? { body } : {});
			return res.data;
		},
		invalidates: [keys.configarr, keys.instances, keys.caps],
		onSuccess: (r) => {
			if (r) toast.success(resultText(r));
		}
	});
	const errors = $derived(fieldErrors(runImport.error));

	function resultText(r: ImportResult): string {
		return m.configarr_result({ imported: r.imported, updated: r.updated, disabled: r.disabled });
	}
</script>

<h1 class="mb-4 text-2xl font-semibold">{m.configarr_title()}</h1>

<section class="card mb-6 max-w-xl" aria-labelledby="configarr-status-title">
	<h2 id="configarr-status-title" class="mb-3 text-lg font-semibold">
		{m.configarr_status_title()}
	</h2>
	{#if status.isPending}
		<p aria-live="polite">{m.loading()}</p>
	{:else if status.isError}
		<ProblemNote error={status.error} />
	{:else if status.data}
		{@const st = status.data}
		<ul class="flex flex-col gap-1 text-sm">
			<li class="break-all">
				{st.linked && st.config_path
					? m.configarr_linked({ path: st.config_path })
					: m.configarr_not_linked()}
			</li>
			{#if st.secrets_path}
				<li class="break-all">{m.configarr_secrets_linked({ path: st.secrets_path })}</li>
			{/if}
			<li>{st.watching ? m.configarr_watching() : m.configarr_not_watching()}</li>
			<li>
				{st.last_import_at
					? m.configarr_last_import({ when: formatWhen(st.last_import_at, getLocale()) })
					: m.configarr_never_imported()}
			</li>
			{#if st.last_result}
				<li>{resultText(st.last_result)}</li>
			{/if}
			{#if st.last_error}
				<li class="text-danger">{m.configarr_last_error({ detail: st.last_error })}</li>
			{/if}
		</ul>
		{#if st.last_result && st.last_result.warnings.length > 0}
			<h3 class="mt-3 text-sm font-semibold">{m.configarr_warnings()}</h3>
			<ul class="list-disc ps-5 text-sm">
				{#each st.last_result.warnings as w, i (i)}
					<li>{w}</li>
				{/each}
			</ul>
		{/if}
	{/if}
</section>

<FormCard
	id="configarr-import-title"
	title={m.configarr_import_title()}
	editing={false}
	submitLabel={m.configarr_import()}
	pending={runImport.isPending}
	error={runImport.error}
	onsubmit={() => runImport.mutate()}
>
	<p class="text-sm text-subtle">{m.configarr_import_hint()}</p>
	<TextField
		id="configarr-config-path"
		label={m.configarr_config_path()}
		placeholder={m.configarr_config_path_placeholder()}
		error={errors.config_path}
		bind:value={configPath}
	/>
	<TextField
		id="configarr-secrets-path"
		label={m.configarr_secrets_path()}
		placeholder={m.configarr_secrets_path_placeholder()}
		error={errors.secrets_path}
		bind:value={secretsPath}
	/>
</FormCard>
