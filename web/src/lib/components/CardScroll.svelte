<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';

	// The bounded body of a dashboard card. It holds every row, shows the first `cap` of them
	// and scrolls to the rest inside the card, as the activity log box does. Once its content
	// overflows it becomes a labelled region in the tab order (WCAG 2.1.1, axe
	// scrollable-region-focusable). A fade over the cut-off row marks that more rows follow; it
	// covers only that row, never one of the first `cap`, so no control is hidden under it. The
	// card's header says how many rows the list holds below (CardScrollHint).

	interface Props {
		/** Accessible name of the scrolling region, usually the card title. */
		label: string;
		/** Rows the body shows before it scrolls. */
		cap: number;
		/** Rows the card holds. */
		total: number;
		/** Selector of one row inside the body. */
		rowSelector: string;
		children: Snippet;
	}

	const { label, cap, total, rowSelector, children }: Props = $props();

	// Part of the first hidden row stays visible, so a cut-off row shows that the list goes on.
	const PEEK_PX = 8;

	let body: HTMLDivElement | undefined = $state();
	let maxHeight: number | undefined = $state();
	let scrollable = $state(false);
	let atEnd = $state(true);

	function measure(node: HTMLDivElement): void {
		const rows = node.querySelectorAll(rowSelector);
		if (cap <= 0 || rows.length <= cap) {
			maxHeight = undefined;
		} else {
			const last = rows[cap - 1].getBoundingClientRect();
			const top = node.getBoundingClientRect().top;
			maxHeight = Math.ceil(last.bottom - top + node.scrollTop) + PEEK_PX;
		}
		track(node);
	}

	function track(node: HTMLDivElement): void {
		scrollable = node.scrollHeight > node.clientHeight || node.scrollWidth > node.clientWidth;
		atEnd = node.scrollTop + node.clientHeight >= node.scrollHeight - 1;
	}

	$effect(() => {
		const node = body;
		if (node === undefined) return;
		// Re-measure whenever the rows change.
		void total;
		measure(node);
		if (typeof ResizeObserver === 'undefined') return;
		const observer = new ResizeObserver(() => measure(node));
		observer.observe(node);
		if (node.firstElementChild) observer.observe(node.firstElementChild);
		return () => observer.disconnect();
	});
</script>

<div class="relative min-w-0">
	<!-- A scrolling region must take keyboard focus to scroll without a pointer (axe scrollable-region-focusable). -->
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<div
		bind:this={body}
		onscroll={() => body && track(body)}
		class="card-scroll overflow-auto"
		style:max-height={maxHeight === undefined ? undefined : `${maxHeight}px`}
		role={scrollable ? 'region' : undefined}
		aria-label={scrollable ? m.overview_scroll_region({ title: label }) : undefined}
		tabindex={scrollable ? 0 : undefined}
		data-testid="card-scroll"
	>
		{@render children()}
	</div>
	{#if scrollable && !atEnd}
		<div
			aria-hidden="true"
			class="card-scroll-fade pointer-events-none absolute inset-x-0 bottom-0"
			style:height={`${PEEK_PX}px`}
		></div>
	{/if}
</div>

<style>
	.card-scroll-fade {
		background: linear-gradient(to bottom, transparent, var(--color-card));
	}
</style>
