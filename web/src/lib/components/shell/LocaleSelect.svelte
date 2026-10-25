<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale, locales, setLocale, type Locale } from '#lib/paraglide/runtime.js';

	const { id }: { id: string } = $props();

	function changeLocale(event: Event): void {
		const value = (event.currentTarget as HTMLSelectElement).value as Locale;
		setLocale(value);
	}
</script>

<div class="flex items-center justify-between gap-2">
	<label for={id} class="font-medium text-subtle">{m.language()}</label>
	<select
		{id}
		class="select h-11 min-h-11 w-24 rounded border border-control bg-bg px-2 text-xs md:h-8 md:min-h-0"
		value={getLocale()}
		onchange={changeLocale}
	>
		{#each locales as loc (loc)}
			<option value={loc}>{loc}</option>
		{/each}
	</select>
</div>
