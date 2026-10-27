<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Field from '#lib/components/Field.svelte';
	import { SFW_MODES, type SfwMode } from '#lib/brand-mark.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		id?: string;
		label?: string;
		hint?: string;
		dataField?: string;
		value: SfwMode | '' | null | undefined;
		allowDefault?: boolean;
		disabled?: boolean;
	}

	let {
		id = 'sfw-mode',
		label = m.settings_sfw_mode(),
		hint = m.settings_sfw_mode_hint(),
		dataField = 'sfw_mode',
		value = $bindable(),
		allowDefault = false,
		disabled = false
	}: Props = $props();

	function modeLabel(mode: SfwMode): string {
		switch (mode) {
			case 'auto':
				return m.sfw_mode_auto();
			case 'on':
				return m.sfw_mode_on();
			case 'off':
				return m.sfw_mode_off();
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
		{#each SFW_MODES as mode (mode)}
			<option value={mode}>{modeLabel(mode)}</option>
		{/each}
	</select>
</Field>
