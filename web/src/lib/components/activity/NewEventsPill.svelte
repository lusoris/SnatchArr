<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		count: number;
		onsnap: () => void;
	}

	const { count, onsnap }: Props = $props();
</script>

{#if count > 0}
	<button
		type="button"
		class="bg-brand text-on-brand absolute bottom-3 left-1/2 z-20 flex -translate-x-1/2 cursor-pointer items-center gap-1.5 rounded-full px-3.5 py-1 text-xs font-semibold shadow-md transition hover:opacity-90"
		onclick={onsnap}
		aria-label={m.activity_new_events({ count })}
	>
		<span aria-hidden="true">↓</span>
		<span>{m.activity_new_events({ count })}</span>
	</button>
{/if}
