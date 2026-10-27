<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import BrandMarkSelect from '#lib/components/BrandMarkSelect.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import SfwModeSelect from '#lib/components/SfwModeSelect.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type BrandMark = components['schemas']['BrandMark'];
	type SfwMode = components['schemas']['SfwMode'];

	const preferences = createSentioQuery({
		queryKey: keys.preferences,
		queryFn: async () => (await api.GET('/me/preferences')).data
	});

	let brandMarkValue = $state<BrandMark | ''>('');
	let sfwModeValue = $state<SfwMode | ''>('');
	let initialized = $state(false);

	$effect(() => {
		if (preferences.data !== undefined && !initialized) {
			brandMarkValue = preferences.data?.brand_mark ?? '';
			sfwModeValue = preferences.data?.sfw_mode ?? '';
			initialized = true;
		}
	});

	const savePreferences = createSentioMutation({
		mutationFn: async (body: { brand_mark?: BrandMark | null; sfw_mode?: SfwMode | null }) => {
			return (await api.PUT('/me/preferences', { body })).data;
		},
		invalidates: [keys.preferences],
		onSuccess: () => {
			toast.success(m.preferences_saved());
		},
		onError: (err) => {
			toast.error(describeError(err));
		}
	});

	function handleSubmit(e: SubmitEvent): void {
		e.preventDefault();
		savePreferences.mutate({
			brand_mark: brandMarkValue === '' ? null : (brandMarkValue as BrandMark),
			sfw_mode: sfwModeValue === '' ? null : (sfwModeValue as SfwMode)
		});
	}
</script>

<QueryGuard query={preferences}>
	<form class="card flex flex-col gap-4" onsubmit={handleSubmit}>
		{#if savePreferences.isError}
			<ProblemNote error={savePreferences.error} />
		{/if}

		<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
			<BrandMarkSelect
				id="user-brand-mark"
				label={m.preferences_brand_mark()}
				hint={m.preferences_brand_mark_hint()}
				dataField="user_brand_mark"
				allowDefault
				bind:value={brandMarkValue}
			/>

			<SfwModeSelect
				id="user-sfw-mode"
				label={m.preferences_sfw_mode()}
				hint={m.preferences_sfw_mode_hint()}
				dataField="user_sfw_mode"
				allowDefault
				bind:value={sfwModeValue}
			/>
		</div>

		<div>
			<button type="submit" class="btn btn-primary" disabled={savePreferences.isPending}>
				{m.preferences_save()}
			</button>
		</div>
	</form>
</QueryGuard>
