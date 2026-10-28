<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	// The "+n" mark next to a dashboard card's title: how many rows its CardScroll list holds
	// past the ones it shows. It sits in the header so it never covers a row, and stays short
	// so the header never wraps; screen readers and the tooltip get the whole sentence.

	interface Props {
		/** Rows past the card's cap. */
		hidden: number;
	}

	const { hidden }: Props = $props();
</script>

{#if hidden > 0}
	<span
		class="rounded-full border border-line px-1.5 text-xs whitespace-nowrap text-subtle"
		title={m.overview_scroll_more({ count: hidden })}
		data-testid="card-scroll-hint"
	>
		<span aria-hidden="true">+{hidden}</span>
		<span class="sr-only">{m.overview_scroll_more({ count: hidden })}</span>
	</span>
{/if}
