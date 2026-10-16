<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import type { components } from '#lib/api/schema.js';
	import { m } from '#lib/paraglide/messages.js';
	import { fieldErrors, filledPart } from '#lib/problem.js';
	import InstanceSelect from './InstanceSelect.svelte';
	import LinkForm, { type LinkBody } from './LinkForm.svelte';

	type SeerrLink = components['schemas']['SeerrLink'];
	type SeerrLinkInput = components['schemas']['SeerrLinkInput'];
	type Instance = components['schemas']['Instance'];

	interface Props {
		/** The stored link being edited, or null to add one. The parent remounts per link. */
		link: SeerrLink | null;
		instances: readonly Instance[];
		pending: boolean;
		error: unknown;
		onsave: (body: SeerrLinkInput) => void;
		oncancel: () => void;
		ontest?: (body: SeerrLinkInput) => void;
		testing?: boolean;
	}

	const { link, instances, pending, error, onsave, oncancel, ontest, testing }: Props = $props();

	const initial = untrack(() => link);
	let sonarrFallback = $state(initial?.sonarr_instance_id ?? '');
	let radarrFallback = $state(initial?.radarr_instance_id ?? '');
	const errors = $derived(fieldErrors(error));

	function withFallbacks(body: LinkBody): SeerrLinkInput {
		return {
			...body,
			...filledPart('sonarr_instance_id', sonarrFallback),
			...filledPart('radarr_instance_id', radarrFallback)
		};
	}
</script>

<LinkForm
	idPrefix="seerr"
	{link}
	{pending}
	{error}
	onsave={(body) => onsave(withFallbacks(body))}
	{oncancel}
	ontest={ontest && ((body) => ontest(withFallbacks(body)))}
	{testing}
>
	{#snippet extra()}
		<InstanceSelect
			id="seerr-sonarr-fallback"
			label={m.seerr_sonarr_fallback()}
			instances={instances.filter((i) => i.kind === 'sonarr')}
			noneLabel={m.seerr_no_fallback()}
			error={errors.sonarr_instance_id}
			bind:value={sonarrFallback}
		/>
		<InstanceSelect
			id="seerr-radarr-fallback"
			label={m.seerr_radarr_fallback()}
			instances={instances.filter((i) => i.kind === 'radarr')}
			noneLabel={m.seerr_no_fallback()}
			error={errors.radarr_instance_id}
			bind:value={radarrFallback}
		/>
	{/snippet}
</LinkForm>
