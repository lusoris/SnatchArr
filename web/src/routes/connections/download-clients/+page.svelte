<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioQuery, createSentioMutation } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import AdminFormSlot from '#lib/components/AdminFormSlot.svelte';
	import AppLogo from '#lib/components/AppLogo.svelte';
	import ConfirmDelete from '#lib/components/ConfirmDelete.svelte';
	import DownloadClientForm from '#lib/components/DownloadClientForm.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import UrlText from '#lib/components/UrlText.svelte';
	import { entityHref } from '#lib/entities.js';
	import { downloadClientQueries } from '#lib/queries/connections.js';
	import { FormEditor } from '#lib/formEditor.svelte.js';
	import { deleteCallbacks, probeFailed, saveSuccess } from '#lib/forms.js';
	import { HELP_MAP } from '#lib/help.js';
	import { downloadClientName } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type DownloadClient = components['schemas']['DownloadClient'];
	type DownloadClientInput = components['schemas']['DownloadClientInput'];
	type DownloadClientStatus = components['schemas']['DownloadClientStatus'];

	const { clients, status } = downloadClientQueries();
	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});
	let discoverFrom = $state('');
	const discover = createSentioMutation({
		mutationFn: async (instanceId: string) =>
			(
				await api.POST('/instances/{instanceId}/download-clients/discover', {
					params: { path: { instanceId } }
				})
			).data,
		invalidates: [keys.dlclients, keys.dlstatus],
		onSuccess: (r) =>
			toast.success(`+${r?.imported ?? 0} / ~${r?.updated ?? 0} / -${r?.removed ?? 0}`),
		onError: (err) => toast.error(describeError(err))
	});

	const editor = new FormEditor<DownloadClient>('dlclient-name', () => save.reset());

	const save = createSentioMutation({
		mutationFn: async (body: DownloadClientInput) => {
			const target = editor.editing;
			const res = target
				? await api.PUT('/download-clients/{clientId}', {
						params: { path: { clientId: target.id } },
						body
					})
				: await api.POST('/download-clients', { body });
			return res.data;
		},
		invalidates: [keys.dlclients, keys.dlstatus],
		onSuccess: saveSuccess(editor.close)
	});

	const remove = createSentioMutation({
		mutationFn: async (c: DownloadClient) => {
			await api.DELETE('/download-clients/{clientId}', { params: { path: { clientId: c.id } } });
			return c;
		},
		invalidates: [keys.dlclients, keys.dlstatus],
		...deleteCallbacks(() => editor.editing, editor.close)
	});

	const probe = createSentioMutation({
		mutationFn: async (c: DownloadClient) => {
			const res = await api.POST('/download-clients/{clientId}/test', {
				params: { path: { clientId: c.id } }
			});
			return { name: c.name, observed: res.data };
		},
		invalidates: [keys.dlclients, keys.dlstatus],
		onSuccess: reportObservation,
		onError: probeFailed
	});

	const probeInput = createSentioMutation({
		mutationFn: async (body: DownloadClientInput) => {
			const res = await api.POST('/download-clients/test', { body });
			return { name: body.name, observed: res.data };
		},
		onSuccess: reportObservation,
		onError: probeFailed
	});

	function reportObservation({
		name,
		observed
	}: {
		name: string;
		observed: DownloadClientStatus | undefined;
	}): void {
		if (observed?.reachable) toast.success(m.dlclients_test_ok({ name }));
		else toast.error(m.dlclients_test_unreachable({ name, detail: observed?.error ?? '' }));
	}

	async function edit(c: DownloadClient): Promise<void> {
		if (c.source === 'configarr') return;
		await editor.open(c);
	}

	function statusFor(id: string) {
		return status.data?.find((s) => s.client_id === id);
	}

	function instanceName(id: string | undefined): string {
		if (!id) return m.dlclients_all_instances();
		const name = instances.data?.find((i) => i.id === id)?.name ?? id;
		return m.dlclients_pinned({ name });
	}
</script>

<PageHeader
	title={m.dlclients_title()}
	documentTitle
	help={HELP_MAP['/connections/download-clients']}
/>

<section aria-label={m.dlclients_title()} class="w-full">
	<div class="grid w-full grid-cols-1 items-start gap-6 2xl:grid-cols-12">
		<div class="flex flex-col gap-4 2xl:col-span-7">
			{#if session.isAdmin}
				<form
					class="flex flex-wrap items-end gap-2"
					onsubmit={(e) => {
						e.preventDefault();
						if (discoverFrom) discover.mutate(discoverFrom);
					}}
				>
					<label class="flex flex-col gap-1 text-sm">
						<span>{m.dlclients_discover()}</span>
						<select class="select" bind:value={discoverFrom} required>
							<option value="" disabled>{m.dlclients_select_instance()}</option>
							{#each instances.data ?? [] as inst (inst.id)}
								<option value={inst.id}>{inst.name}</option>
							{/each}
						</select>
					</label>
					<button
						type="submit"
						class="btn btn-primary"
						disabled={discover.isPending || !discoverFrom}>{m.dlclients_discover()}</button
					>
				</form>
			{/if}

			{#if clients.isPending}
				<p aria-live="polite">{m.loading()}</p>
			{:else if clients.isError}
				<ProblemNote error={clients.error} />
			{:else if (clients.data ?? []).length === 0}
				<p class="card mb-6">{m.dlclients_empty()}</p>
			{:else}
				<div class="relative w-full overflow-x-auto rounded-lg border border-line">
					<table class="compact-table table-stack table w-full">
						<caption class="sr-only">{m.dlclients_title()}</caption>
						<thead>
							<tr>
								<th scope="col">{m.instance_name()}</th>
								<th scope="col">{m.dlclients_kind()}</th>
								<th scope="col">{m.instance_url()}</th>
								<th scope="col">{m.dlclients_instance()}</th>
								<th scope="col">{m.instance_last_check()}</th>
								{#if session.isAdmin}
									<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
								{/if}
							</tr>
						</thead>
						<tbody>
							{#each clients.data ?? [] as c (c.id)}
								{@const st = statusFor(c.id)}
								<tr class:target={session.isAdmin && c.source !== 'configarr'}>
									<td data-label={m.instance_name()}>
										<div class="flex items-center gap-2">
											<AppLogo kind={c.kind} size={18} />
											<h2
												class="text-sm font-semibold break-words md:max-w-22 md:truncate"
												title={c.name}
											>
												{c.name}
											</h2>
										</div>
									</td>
									<td class="text-sm text-subtle" data-label={m.dlclients_kind()}
										>{downloadClientName(c.kind)} · {c.protocol}</td
									>
									<td class="text-sm text-subtle" data-label={m.instance_url()}
										><UrlText
											url={c.base_url}
											title={c.base_url}
											class="block md:max-w-24 md:truncate"
										/></td
									>
									<td class="text-sm text-subtle" data-label={m.dlclients_instance()}
										>{#if c.instance_id}<a
												href={entityHref('instance', c.instance_id)}
												class="hover:underline">{instanceName(c.instance_id)}</a
											>{:else}{instanceName(c.instance_id)}{/if}</td
									>
									<td data-label={m.instance_last_check()}>
										{#if st}
											<div class="min-w-0 text-sm" role="status">
												<div>
													{st.reachable ? m.dlclients_reachable() : m.dlclients_unreachable()}
													{#if st.reachable}· {m.dlclients_active({
															active: st.active,
															queued: st.queued
														})} · ↓ {Math.round(st.download_rate_bps / 1024)} KiB/s{/if}
												</div>
												{#if st.error}
													<details class="text-xs text-danger md:max-w-24">
														<summary
															class="line-clamp-2 cursor-pointer [overflow-wrap:anywhere]"
															title={st.error}
														>
															{st.error}
														</summary>
														<p class="mt-1 break-all whitespace-pre-wrap">{st.error}</p>
													</details>
												{/if}
											</div>
										{:else}
											<span class="text-sm text-subtle">{m.instance_never_tested()}</span>
										{/if}
									</td>
									{#if session.isAdmin}
										<td data-label="">
											<div class="flex flex-wrap items-center gap-1.5">
												<button
													type="button"
													class="btn target-link"
													disabled={c.source === 'configarr'}
													onclick={() => edit(c)}>{m.action_edit()}</button
												>
												<button
													type="button"
													class="btn"
													disabled={probe.isPending}
													onclick={() => probe.mutate(c)}>{m.action_test()}</button
												>
												<ConfirmDelete
													name={c.name}
													disabled={remove.isPending || c.source === 'configarr'}
													onconfirm={() => remove.mutate(c)}
												/>
											</div>
										</td>
									{/if}
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>

		<div class="2xl:col-span-5">
			<AdminFormSlot {editor} {save} probe={probeInput}>
				{#snippet form(props)}
					<DownloadClientForm client={editor.editing} instances={instances.data ?? []} {...props} />
				{/snippet}
			</AdminFormSlot>
		</div>
	</div>
</section>

<style>
	@media (min-width: 1280px) {
		.compact-table th,
		.compact-table td {
			padding: 0.35rem 0.5rem;
		}
	}
</style>
