<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Field from '#lib/components/Field.svelte';
	import { BRAND_MARKS, type BrandMarkKind } from '#lib/brand-mark.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		id?: string;
		label?: string;
		hint?: string;
		dataField?: string;
		value: BrandMarkKind | '' | null | undefined;
		allowDefault?: boolean;
		disabled?: boolean;
	}

	let {
		id = 'brand-mark',
		label = m.settings_brand_mark(),
		hint = m.settings_brand_mark_hint(),
		dataField = 'brand_mark',
		value = $bindable(),
		allowDefault = false,
		disabled = false
	}: Props = $props();

	function markLabel(k: BrandMarkKind): string {
		switch (k) {
			case 'vulva':
				return m.brand_mark_vulva();
			case 'penis':
				return m.brand_mark_penis();
			case 'hand':
				return m.brand_mark_hand();
		}
	}
</script>

<Field {id} {label} {hint}>
	<select
		{id}
		data-field={dataField}
		class="select"
		bind:value
		{disabled}
		aria-describedby={hint ? `${id}-hint` : undefined}
	>
		{#if allowDefault}
			<option value="">{m.preferences_follow_default()}</option>
		{/if}
		{#each BRAND_MARKS as k (k)}
			<option value={k}>{markLabel(k)}</option>
		{/each}
	</select>
</Field>
