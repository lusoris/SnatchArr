<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import StatusIcon from '#lib/components/StatusIcon.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys, queryClient } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type ModuleState = components['schemas']['ModuleState'];

	const modulesQuery = createSentioQuery<ModuleState[]>({
		queryKey: keys.modules,
		queryFn: async () => (await api.GET('/system/modules')).data ?? []
	});

	let togglingId = $state<string | null>(null);
	let toggleError = $state<unknown>(null);

	async function toggleModule(mod: ModuleState): Promise<void> {
		if (!session.isAdmin || togglingId !== null) return;
		togglingId = mod.id;
		toggleError = null;

		try {
			const detail = await api.GET('/system/modules/{id}', {
				params: { path: { id: mod.id } }
			});
			const config = detail.data?.config ?? {};

			await api.PUT('/system/modules/{id}', {
				params: { path: { id: mod.id } },
				body: { enabled: !mod.enabled, config }
			});

			toast.success(m.modules_toggle_success());
			await queryClient.invalidateQueries({ queryKey: keys.modules });
		} catch (err) {
			toggleError = err;
			toast.error(describeError(err));
		} finally {
			togglingId = null;
		}
	}
</script>

<section class="w-full" aria-labelledby="modules-main-title">
	<h2 id="modules-main-title" class="mb-4 text-2xl font-semibold">
		{m.modules_title()}
	</h2>

	<div class="grid w-full grid-cols-1 items-start gap-6 xl:grid-cols-12">
		<div class="flex flex-col gap-6 xl:col-span-7">
			{#if modulesQuery.isPending}
				<div data-testid="skeleton" class="card flex flex-col gap-3" aria-busy="true">
					<p aria-live="polite">{m.loading()}</p>
				</div>
			{:else if modulesQuery.isError}
				<ProblemNote error={modulesQuery.error} />
			{:else}
				{#if toggleError}
					<ProblemNote error={toggleError} />
				{/if}
				<section class="card w-full" aria-labelledby="modules-table-heading">
					<h3 id="modules-table-heading" class="sr-only">{m.modules_title()}</h3>
					{#if (modulesQuery.data ?? []).length === 0}
						<p class="text-sm text-subtle">{m.modules_empty()}</p>
					{:else}
						<div class="relative overflow-x-auto">
							<table class="table-stack table">
								<caption class="sr-only">{m.modules_title()}</caption>
								<thead>
									<tr>
										<th scope="col">{m.modules_col_name()}</th>
										<th scope="col">{m.modules_col_status()}</th>
										{#if session.isAdmin}
											<th scope="col" class="text-end">{m.modules_col_action()}</th>
										{/if}
									</tr>
								</thead>
								<tbody>
									{#each modulesQuery.data ?? [] as mod (mod.id)}
										<tr>
											<td class="font-medium" data-label="">
												<div class="flex flex-col">
													<span class="font-mono text-sm text-fg">{mod.id}</span>
												</div>
											</td>
											<td data-label={m.modules_col_status()}>
												<div class="flex items-center gap-2">
													<StatusIcon
														status={mod.enabled ? 'ok' : 'disabled'}
														label={mod.enabled
															? m.modules_status_enabled()
															: m.modules_status_disabled()}
													/>
													<span class="text-sm">
														{mod.enabled ? m.modules_status_enabled() : m.modules_status_disabled()}
													</span>
												</div>
											</td>
											{#if session.isAdmin}
												<td class="text-end" data-label="">
													<button
														type="button"
														class="btn min-h-11 px-3 py-1.5 text-xs md:min-h-0 {mod.enabled
															? ''
															: 'btn-primary'}"
														disabled={togglingId === mod.id}
														onclick={() => toggleModule(mod)}
													>
														{mod.enabled ? m.modules_toggle_disable() : m.modules_toggle_enable()}
													</button>
												</td>
											{/if}
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</section>
			{/if}
		</div>

		<aside class="flex flex-col gap-4 xl:col-span-5" aria-labelledby="modules-help-heading">
			<section class="card flex flex-col gap-3">
				<h3 id="modules-help-heading" class="text-lg font-semibold">
					{m.modules_help_title()}
				</h3>
				<div class="flex flex-col gap-2 text-sm text-subtle">
					<p>{m.modules_help_desc()}</p>
					<h4 class="mt-2 font-medium text-fg">{m.modules_help_config_title()}</h4>
					<p>{m.modules_help_config_desc()}</p>
				</div>
			</section>
		</aside>
	</div>
</section>
