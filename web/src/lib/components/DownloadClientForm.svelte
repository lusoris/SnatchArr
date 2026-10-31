<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import type { components } from '#lib/api/schema.js';
	import {
		DOWNLOAD_CLIENT_KINDS,
		downloadClientName,
		type DownloadClientKind
	} from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';
	import { fieldErrors, filledPart } from '#lib/problem.js';
	import AppLogo from './AppLogo.svelte';
	import FormCard from './FormCard.svelte';
	import InstanceSelect from './InstanceSelect.svelte';
	import SecretField from './SecretField.svelte';
	import TextField from './TextField.svelte';

	type DownloadClient = components['schemas']['DownloadClient'];
	type DownloadClientInput = components['schemas']['DownloadClientInput'];
	type Instance = components['schemas']['Instance'];

	interface Props {
		/** The stored client being edited, or null to add one. The parent remounts per client. */
		client: DownloadClient | null;
		instances: readonly Instance[];
		pending: boolean;
		error: unknown;
		onsave: (body: DownloadClientInput) => void;
		oncancel: () => void;
		/** Probes the typed values; offered on the create form only, where the secret is at hand. */
		ontest?: (body: DownloadClientInput) => void;
		testing?: boolean;
	}

	const { client, instances, pending, error, onsave, oncancel, ontest, testing }: Props = $props();

	const kinds = DOWNLOAD_CLIENT_KINDS.map((value) => ({ value, label: downloadClientName(value) }));

	const initial = untrack(() => client);
	let kind = $state<DownloadClientKind>(initial?.kind ?? 'qbittorrent');
	let name = $state(initial?.name ?? '');
	let baseUrl = $state(initial?.base_url ?? '');
	let username = $state(initial?.username ?? '');
	let secret = $state('');
	let instanceId = $state(initial?.instance_id ?? '');
	let enabled = $state(initial?.enabled ?? true);
	let maxActive = $state<number | null>(initial?.max_active ?? 0);
	let maxQueued = $state<number | null>(initial?.max_queued ?? 0);
	let bandwidth = $state<number | null>(initial?.bandwidth_budget_bps ?? 0);
	const errors = $derived(fieldErrors(error));

	function body(): DownloadClientInput {
		return {
			kind,
			name,
			base_url: baseUrl,
			username,
			enabled,
			max_active: maxActive ?? 0,
			max_queued: maxQueued ?? 0,
			bandwidth_budget_bps: bandwidth ?? 0,
			...filledPart('secret', secret),
			...filledPart('instance_id', instanceId)
		};
	}
</script>

<FormCard
	id="dlclient-form-title"
	title={initial ? m.form_edit_title({ name: initial.name }) : m.dlclients_add()}
	editing={initial !== null}
	{pending}
	{error}
	onsubmit={() => onsave(body())}
	{oncancel}
	ontest={initial || !ontest ? undefined : () => ontest(body())}
	{testing}
>
	<!-- Two field columns from md: the add form fits the 1280x800 scroll budget. -->
	<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
		<div class="field flex flex-col gap-1">
			<div class="flex items-center gap-2">
				<label for="dlclient-kind" class="text-sm font-medium">{m.dlclients_kind()}</label>
				<AppLogo {kind} size={18} />
			</div>
			<select id="dlclient-kind" class="select" bind:value={kind}>
				{#each kinds as k (k.value)}
					<option value={k.value}>{k.label}</option>
				{/each}
			</select>
		</div>
		<TextField
			id="dlclient-name"
			label={m.instance_name()}
			required
			maxlength={64}
			error={errors.name}
			bind:value={name}
		/>
		<TextField
			id="dlclient-base-url"
			label={m.instance_url()}
			required
			inputmode="url"
			hint="http://qbittorrent:8080"
			error={errors.base_url}
			bind:value={baseUrl}
		/>
		<TextField
			id="dlclient-username"
			label={m.username()}
			autocomplete="off"
			error={errors.username}
			bind:value={username}
		/>
		<SecretField
			id="dlclient-secret"
			label={m.dlclients_secret()}
			editing={initial !== null}
			error={errors.secret}
			bind:value={secret}
		/>
		<InstanceSelect
			id="dlclient-instance"
			label={m.dlclients_instance()}
			{instances}
			noneLabel={m.dlclients_all_instances()}
			error={errors.instance_id}
			bind:value={instanceId}
		/>
		<TextField
			id="dlclient-max-active"
			label={m.dlclients_max_active()}
			type="number"
			min={0}
			max={1000}
			error={errors.max_active}
			bind:value={maxActive}
		/>
		<TextField
			id="dlclient-max-queued"
			label={m.dlclients_max_queued()}
			type="number"
			min={0}
			max={1000}
			error={errors.max_queued}
			bind:value={maxQueued}
		/>
		<TextField
			id="dlclient-bandwidth"
			label={m.dlclients_bandwidth()}
			type="number"
			min={0}
			error={errors.bandwidth_budget_bps}
			bind:value={bandwidth}
		/>
	</div>
	<label class="flex items-center gap-2 text-sm">
		<input type="checkbox" bind:checked={enabled} />
		<span>{m.form_enabled()}</span>
	</label>
</FormCard>
