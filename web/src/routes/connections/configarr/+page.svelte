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
	import QueryGuard from '#lib/components/QueryGuard.svelte';
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

<h1 id="configarr-main-title" class="mb-4 text-2xl font-semibold">{m.configarr_title()}</h1>

<section aria-labelledby="configarr-main-title" class="w-full">
	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-6 xl:col-span-7">
			<section class="card w-full" aria-labelledby="configarr-status-title">
				<h2 id="configarr-status-title" class="mb-3 text-lg font-semibold">
					{m.configarr_status_title()}
				</h2>
				<QueryGuard query={status}>
					{#snippet children(st)}
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
					{/snippet}
				</QueryGuard>
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
		</div>

		<aside class="flex flex-col gap-4 xl:col-span-5" aria-labelledby="configarr-help-heading">
			<section class="card flex flex-col gap-3">
				<h2 id="configarr-help-heading" class="text-lg font-semibold">
					{m.configarr_help_title()}
				</h2>
				<div class="flex flex-col gap-2 text-sm text-subtle">
					<h3 class="font-medium text-fg">{m.configarr_help_linked_title()}</h3>
					<p>{m.configarr_help_linked_desc()}</p>
					<h3 class="mt-2 font-medium text-fg">{m.configarr_help_readonly_title()}</h3>
					<p>{m.configarr_help_readonly_desc()}</p>
				</div>
			</section>
		</aside>
	</div>
</section>
