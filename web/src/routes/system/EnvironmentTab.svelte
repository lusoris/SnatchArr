<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdminSaveButton from '#lib/components/AdminSaveButton.svelte';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys, queryClient } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type RuntimeSetting = components['schemas']['RuntimeSetting'];
	type RuntimeSettingClass = components['schemas']['RuntimeSettingClass'];
	type RuntimeSettingSource = components['schemas']['RuntimeSettingSource'];

	const CLASSES: readonly RuntimeSettingClass[] = ['bootstrap', 'runtime', 'secret'];

	// Only admins may read the settings; a viewer is told so instead of being refused.
	const runtimeSettingsQuery = createSentioQuery<RuntimeSetting[]>(() => ({
		queryKey: keys.runtimeSettings,
		queryFn: async () => (await api.GET('/settings/runtime')).data ?? [],
		enabled: session.isAdmin
	}));

	let draftValues = $state<Record<string, string>>({});
	let secretDrafts = $state<Record<string, string>>({});
	let initialValues = $state<Record<string, string>>({});
	let initialized = $state(false);
	let hasPendingRestart = $state(false);
	let saveError = $state<unknown>(null);
	let isSaving = $state(false);

	function sourceLabel(source: RuntimeSettingSource): string {
		switch (source) {
			case 'env':
				return m.environment_source_env();
			case 'db':
				return m.environment_source_db();
			case 'default':
				return m.environment_source_default();
		}
	}

	function classHeading(cls: RuntimeSettingClass): string {
		switch (cls) {
			case 'bootstrap':
				return m.environment_class_bootstrap();
			case 'runtime':
				return m.environment_class_runtime();
			case 'secret':
				return m.environment_class_secret();
		}
	}

	function getReason(setting: RuntimeSetting): string | undefined {
		if (setting.class === 'bootstrap') {
			return m.environment_bootstrap_reason();
		}
		if (setting.locked) {
			return m.environment_locked_by_env({ env: setting.env });
		}
		if (setting.class === 'secret' && session.isAdmin) {
			return m.secret_keep_hint();
		}
		return undefined;
	}

	function syncData(settings: RuntimeSetting[]): void {
		const drafts: Record<string, string> = {};
		const inits: Record<string, string> = {};
		const secDrafts: Record<string, string> = {};
		for (const s of settings) {
			if (s.class !== 'secret') {
				const val = s.value ?? s.default ?? '';
				drafts[s.key] = val;
				inits[s.key] = val;
			} else {
				secDrafts[s.key] = '';
			}
		}
		draftValues = drafts;
		initialValues = inits;
		secretDrafts = secDrafts;
	}

	$effect(() => {
		if (runtimeSettingsQuery.data && !initialized) {
			syncData(runtimeSettingsQuery.data);
			initialized = true;
		}
	});

	function collectChanged(settings: RuntimeSetting[]): Record<string, string> {
		const changed: Record<string, string> = {};
		for (const s of settings) {
			if (s.locked || s.class === 'bootstrap') continue;
			if (s.class === 'secret') {
				const sec = secretDrafts[s.key];
				if (sec !== undefined && sec !== '') {
					changed[s.key] = sec;
				}
			} else {
				const cur = draftValues[s.key] ?? '';
				const init = initialValues[s.key] ?? '';
				if (cur !== init) {
					changed[s.key] = cur;
				}
			}
		}
		return changed;
	}

	async function handleSubmit(e: SubmitEvent, settings: RuntimeSetting[]): Promise<void> {
		e.preventDefault();
		if (!session.isAdmin || isSaving) return;
		saveError = null;
		const changed = collectChanged(settings);
		if (Object.keys(changed).length === 0) return;
		isSaving = true;
		try {
			const res = await api.PUT('/settings/runtime', {
				body: { values: changed }
			});
			if (res.data?.pending_restart) {
				hasPendingRestart = true;
			}
			toast.success(m.environment_saved());
			if (res.data?.settings) {
				queryClient.setQueryData(keys.runtimeSettings, res.data.settings);
				syncData(res.data.settings);
			} else {
				await queryClient.invalidateQueries({ queryKey: keys.runtimeSettings });
				for (const [k, v] of Object.entries(changed)) {
					initialValues[k] = v;
				}
				secretDrafts = {};
			}
		} catch (err) {
			saveError = err;
		} finally {
			isSaving = false;
		}
	}
</script>

<section class="w-full" aria-labelledby="environment-main-title">
	<h2 id="environment-main-title" class="mb-4 text-2xl font-semibold">
		{m.environment_title()}
	</h2>

	{#if !session.isAdmin}
		<p class="card text-sm text-subtle">{m.environment_admin_only()}</p>
	{:else}
		<QueryGuard query={runtimeSettingsQuery}>
			{#snippet children(settings)}
				{#if settings.length === 0}
					<section class="card w-full" aria-labelledby="environment-main-title">
						<p class="text-sm text-subtle">{m.environment_empty()}</p>
					</section>
				{:else if initialized}
					<form class="flex w-full flex-col gap-6" onsubmit={(e) => handleSubmit(e, settings)}>
						{#if hasPendingRestart}
							<div
								role="status"
								class="rounded-md border border-line bg-surface p-4 text-sm text-fg"
							>
								{m.environment_restart_notice()}
							</div>
						{/if}

						{#if saveError}
							<ProblemNote error={saveError} />
						{/if}

						{#each CLASSES as cls (cls)}
							{@const items = settings.filter((s) => s.class === cls)}
							{#if items.length > 0}
								<section class="card flex flex-col gap-3" aria-labelledby={`class-${cls}-heading`}>
									<h3 id={`class-${cls}-heading`} class="text-lg font-semibold">
										{classHeading(cls)}
									</h3>
									<div class="relative overflow-x-auto">
										<table class="table-stack table">
											<caption class="sr-only">{classHeading(cls)}</caption>
											<thead>
												<tr>
													<th scope="col">{m.environment_col_env()}</th>
													<th scope="col">{m.environment_col_key()}</th>
													<th scope="col">{m.environment_col_source()}</th>
													<th scope="col">{m.environment_col_value()}</th>
												</tr>
											</thead>
											<tbody>
												{#each items as setting (setting.key)}
													<tr>
														<td class="font-medium" data-label={m.environment_col_env()}>
															<span class="font-mono text-sm text-fg">{setting.env}</span>
														</td>
														<td data-label={m.environment_col_key()}>
															<span class="font-mono text-xs text-subtle">{setting.key}</span>
														</td>
														<td data-label={m.environment_col_source()}>
															<span
																class="rounded-full border border-line px-2 py-0.5 text-xs text-subtle"
															>
																{sourceLabel(setting.source)}
															</span>
														</td>
														<td data-label={m.environment_col_value()}>
															{#if setting.class === 'secret'}
																<div class="flex flex-col gap-1.5">
																	<div class="flex items-center gap-2">
																		<span
																			class="rounded-full border border-line px-2 py-0.5 text-xs {setting.is_set
																				? 'font-medium text-fg'
																				: 'text-subtle'}"
																		>
																			{setting.is_set
																				? m.environment_secret_set()
																				: m.environment_secret_unset()}
																		</span>
																	</div>
																	<div class="[&_label]:sr-only">
																		<Field
																			id={`setting-${setting.key}`}
																			label={setting.key}
																			hint={getReason(setting)}
																		>
																			<input
																				id={`setting-${setting.key}`}
																				class="input text-sm"
																				type="password"
																				autocomplete="new-password"
																				placeholder={setting.locked || !session.isAdmin
																					? undefined
																					: m.secret_keep_hint()}
																				disabled={!session.isAdmin || setting.locked}
																				bind:value={secretDrafts[setting.key]}
																			/>
																		</Field>
																	</div>
																</div>
															{:else}
																<div class="[&_label]:sr-only">
																	<Field
																		id={`setting-${setting.key}`}
																		label={setting.key}
																		hint={getReason(setting)}
																	>
																		<input
																			id={`setting-${setting.key}`}
																			class="input text-sm"
																			type="text"
																			disabled={!session.isAdmin ||
																				setting.locked ||
																				setting.class === 'bootstrap'}
																			bind:value={draftValues[setting.key]}
																		/>
																	</Field>
																</div>
															{/if}
														</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								</section>
							{/if}
						{/each}

						<div>
							<AdminSaveButton disabled={isSaving} />
						</div>
					</form>
				{/if}
			{/snippet}
		</QueryGuard>
	{/if}
</section>
