<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import NavIcon from './NavIcon.svelte';

	interface Props {
		onclick?: () => void;
	}

	const { onclick }: Props = $props();
</script>

<div class="mt-4 border-t border-line pt-3">
	<a
		href="https://ko-fi.com/lusoris"
		target="_blank"
		rel="noopener noreferrer"
		aria-label={m.nav_kofi_aria()}
		{onclick}
		class="nav-link flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium"
	>
		<NavIcon name="heart" size={16} class="text-subtle" />
		<span>{m.nav_kofi()}</span>
	</a>
</div>
