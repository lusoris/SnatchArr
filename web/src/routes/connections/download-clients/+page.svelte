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
	import ConfirmDelete from '#lib/components/ConfirmDelete.svelte';
	import DownloadClientForm from '#lib/components/DownloadClientForm.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import UrlText from '#lib/components/UrlText.svelte';
	import { FormEditor } from '#lib/formEditor.svelte.js';
	import { deleteCallbacks, probeFailed, saveSuccess } from '#lib/forms.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	type DownloadClient = components['schemas']['DownloadClient'];
	type DownloadClientInput = components['schemas']['DownloadClientInput'];
	type DownloadClientStatus = components['schemas']['DownloadClientStatus'];

	const clients = createSentioQuery({
		queryKey: keys.dlclients,
		queryFn: async () => (await api.GET('/download-clients')).data ?? []
	});
	const status = createSentioQuery({
		queryKey: keys.dlstatus,
		queryFn: async () => (await api.GET('/download-clients/status')).data ?? [],
		refetchInterval: 30_000
	});
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

<h1 class="mb-4 text-2xl font-semibold">{m.dlclients_title()}</h1>

{#if session.isAdmin}
	<form
		class="mb-4 flex flex-wrap items-end gap-2"
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
		<button type="submit" class="btn btn-primary" disabled={discover.isPending || !discoverFrom}
			>{m.dlclients_discover()}</button
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
	<ul class="mb-6 grid gap-3 md:grid-cols-2">
		{#each clients.data ?? [] as c (c.id)}
			{@const st = statusFor(c.id)}
			<li class="card min-w-0">
				<h2 class="flex flex-wrap items-baseline gap-x-1.5 font-semibold break-words">
					<span>{c.name}</span> <span class="text-sm text-subtle">{c.kind} · {c.protocol}</span>
				</h2>
				<p class="text-sm text-subtle"><UrlText url={c.base_url} /></p>
				<p class="text-sm text-subtle">{instanceName(c.instance_id)}</p>
				{#if st}
					<p class="mt-1 text-sm" role="status">
						{st.reachable ? m.dlclients_reachable() : m.dlclients_unreachable()}
						{#if st.reachable}· {m.dlclients_active({ active: st.active, queued: st.queued })} · ↓ {Math.round(
								st.download_rate_bps / 1024
							)} KiB/s{/if}
						{#if st.error}<span class="text-danger"> {st.error}</span>{/if}
					</p>
				{/if}
				{#if session.isAdmin}
					<div class="mt-3 flex flex-wrap gap-2">
						<button
							type="button"
							class="btn"
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
				{/if}
			</li>
		{/each}
	</ul>
{/if}

<AdminFormSlot {editor} {save} probe={probeInput}>
	{#snippet form(props)}
		<DownloadClientForm client={editor.editing} instances={instances.data ?? []} {...props} />
	{/snippet}
</AdminFormSlot>
