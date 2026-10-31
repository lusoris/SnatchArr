<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import CircleHelp from '@lucide/svelte/icons/circle-help';
	import { m } from '#lib/paraglide/messages.js';
	import HelpSheet from './HelpSheet.svelte';

	interface Props {
		title: string;
		description?: string;
		status?: Snippet;
		actions?: Snippet;
		help?: { summaryKey: string; docsPath: string };
		documentTitle?: boolean;
		sticky?: boolean;
		class?: string;
		children?: Snippet;
	}

	const {
		title,
		description,
		status,
		actions,
		help,
		documentTitle = false,
		sticky = false,
		class: className = '',
		children
	}: Props = $props();

	let helpOpen = $state(false);
</script>

<svelte:head>
	{#if documentTitle}
		<title>{title} · {m.app_name()}</title>
	{/if}
</svelte:head>

<!-- Sticky only on screens tall enough to keep it short: at 400% zoom or in phone landscape the
     header with its tabs would cover the content and the focused control (WCAG 1.4.10, 2.4.11). -->
<header
	class="mb-6 flex flex-col gap-3 {sticky
		? 'top-0 z-10 -mx-4 bg-bg/95 px-4 pt-1 pb-2 backdrop-blur-md md:-mx-6 md:px-6 [@media(height>=40rem)]:sticky'
		: ''} {className}"
>
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<div class="flex flex-wrap items-center gap-2.5">
				<h1 class="text-2xl font-semibold tracking-tight text-fg">{title}</h1>
				{#if status}
					{@render status()}
				{/if}
				{#if help}
					<button
						type="button"
						class="inline-flex h-11 w-11 items-center justify-center rounded-full border border-control bg-surface text-subtle hover:text-fg"
						aria-label={m.help_button_aria()}
						onclick={() => (helpOpen = true)}
					>
						<CircleHelp size={18} aria-hidden="true" />
					</button>
				{/if}
			</div>
			{#if description}
				<p class="mt-1 text-sm text-subtle">{description}</p>
			{/if}
		</div>
		{#if actions}
			<div class="flex items-center gap-2">
				{@render actions()}
			</div>
		{/if}
	</div>
	{#if children}
		{@render children()}
	{/if}
</header>

{#if help}
	<HelpSheet bind:open={helpOpen} summaryKey={help.summaryKey} docsPath={help.docsPath} />
{/if}
