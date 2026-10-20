<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import NavIcon from './NavIcon.svelte';

	interface Props {
		drawerOpen: boolean;
		onOpenDrawer: () => void;
		onOpenSearch?: () => void;
		menuButtonEl?: HTMLButtonElement | null;
	}

	let { drawerOpen, onOpenDrawer, onOpenSearch, menuButtonEl = $bindable(null) }: Props = $props();
</script>

<header
	class="header-band sticky top-0 z-30 flex h-14 w-full items-center justify-between border-b border-line bg-surface/95 px-4 backdrop-blur-md lg:hidden"
	data-testid="header-band"
>
	<a
		href={resolve('/')}
		class="flex items-center gap-2.5 font-sans text-base font-semibold tracking-[-0.01em] text-fg"
		aria-label={m.app_name()}
	>
		<span class="text-brand flex items-center">
			<BrandMark size={24} />
		</span>
		<span>{m.app_name()}</span>
	</a>

	<div class="flex items-center gap-1.5">
		{#if onOpenSearch}
			<button
				type="button"
				class="btn inline-flex h-9 min-h-0 w-9 items-center justify-center rounded-md border border-control p-0"
				onclick={onOpenSearch}
				aria-label={m.nav_search()}
			>
				<NavIcon name="search" size={18} />
			</button>
		{/if}
		<button
			bind:this={menuButtonEl}
			type="button"
			class="btn inline-flex h-9 min-h-0 w-9 items-center justify-center rounded-md border border-control p-0"
			onclick={onOpenDrawer}
			aria-label={m.nav_open_menu()}
			aria-expanded={drawerOpen}
			aria-controls="mobile-nav-dialog"
		>
			<NavIcon name="menu" size={20} />
		</button>
	</div>
</header>
