<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" module>
	/** The fields a Seerr link and a Cleanuparr link share. */
	export interface LinkBody {
		name: string;
		base_url: string;
		enabled: boolean;
		api_key?: string;
	}
</script>

<script lang="ts">
	import { untrack, type Snippet } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { fieldErrors, filledPart } from '#lib/problem.js';
	import FormCard from './FormCard.svelte';
	import SecretField from './SecretField.svelte';
	import TextField from './TextField.svelte';

	interface Props {
		/** Prefix for element ids, so two forms never share one. */
		idPrefix: string;
		/** The stored link being edited, or null to add one. The parent remounts per link. */
		link: { name: string; base_url: string; enabled: boolean } | null;
		pending: boolean;
		error: unknown;
		onsave: (body: LinkBody) => void;
		oncancel: () => void;
		/** Probes the typed values; offered on the create form only, where the key is at hand. */
		ontest?: (body: LinkBody) => void;
		testing?: boolean;
		/** Link-specific fields, rendered after the shared ones. */
		extra?: Snippet;
	}

	const {
		idPrefix,
		link,
		pending,
		error,
		onsave,
		oncancel,
		ontest,
		testing = false,
		extra
	}: Props = $props();

	const initial = untrack(() => link);
	let name = $state(initial?.name ?? '');
	let baseUrl = $state(initial?.base_url ?? '');
	let apiKey = $state('');
	let enabled = $state(initial?.enabled ?? true);
	const errors = $derived(fieldErrors(error));

	function body(): LinkBody {
		return { name, base_url: baseUrl, enabled, ...filledPart('api_key', apiKey) };
	}
</script>

<FormCard
	id={`${idPrefix}-form-title`}
	title={initial ? m.form_edit_title({ name: initial.name }) : m.link_add()}
	editing={initial !== null}
	{pending}
	{error}
	onsubmit={() => onsave(body())}
	{oncancel}
	ontest={initial || !ontest ? undefined : () => ontest(body())}
	{testing}
>
	<TextField
		id={`${idPrefix}-name`}
		label={m.instance_name()}
		required
		maxlength={64}
		error={errors.name}
		bind:value={name}
	/>
	<TextField
		id={`${idPrefix}-base-url`}
		label={m.instance_url()}
		required
		inputmode="url"
		error={errors.base_url}
		bind:value={baseUrl}
	/>
	<SecretField
		id={`${idPrefix}-api-key`}
		label={m.instance_api_key()}
		editing={initial !== null}
		requiredOnCreate
		error={errors.api_key}
		bind:value={apiKey}
	/>
	<label class="flex items-center gap-2 text-sm">
		<input type="checkbox" bind:checked={enabled} />
		<span>{m.form_enabled()}</span>
	</label>
	{@render extra?.()}
</FormCard>
