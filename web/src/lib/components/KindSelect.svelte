<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Field from '#lib/components/Field.svelte';
	import { APP_KINDS, type AppKind, isLegacyKind, kindLabel } from '#lib/kinds.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		value: AppKind;
		disabled?: boolean;
	}

	let { value = $bindable(), disabled = false }: Props = $props();
	const hint = $derived(isLegacyKind(value) ? m.instance_legacy_hint() : undefined);
</script>

<Field id="kind" label={m.instance_kind()} {hint}>
	<select
		id="kind"
		class="select"
		bind:value
		{disabled}
		aria-describedby={hint ? 'kind-hint' : undefined}
	>
		{#each APP_KINDS as k (k)}
			<option value={k}>{kindLabel(k)}</option>
		{/each}
	</select>
</Field>
