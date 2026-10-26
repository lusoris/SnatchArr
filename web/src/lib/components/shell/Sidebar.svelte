<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { isApplePlatform } from '@sveltesentio/ui/cmd';
	import { FALLBACK_BRAND_MARK, type BrandMarkKind } from '#lib/brand-mark.js';
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { filterByModule, getNavItems, type NavItem } from './nav.js';
	import NavIcon from './NavIcon.svelte';
	import NavList from './NavList.svelte';
	import KofiLink from './KofiLink.svelte';
	import NavFooter from './NavFooter.svelte';

	interface Props {
		navItems?: NavItem[];
		activeModules?: ReadonlySet<string>;
		onLogout: () => void;
		onOpenSearch?: () => void;
		/** Effective brand mark, resolved by the layout (ADR-0042). Defaults to the fail-closed fallback. */
		mark?: BrandMarkKind;
	}

	const {
		navItems,
		activeModules = new Set(),
		onLogout,
		onOpenSearch,
		mark = FALLBACK_BRAND_MARK
	}: Props = $props();

	const effectiveNavItems = $derived(
		filterByModule(navItems ?? getNavItems(activeModules), activeModules)
	);
	const shortcutLabel = isApplePlatform() ? m.nav_search_shortcut_apple() : m.nav_search_shortcut();
</script>

<aside
	class="fixed inset-y-0 left-0 z-20 hidden w-60 flex-col border-r border-line bg-surface lg:flex"
	aria-label={m.app_name()}
>
	<div
		class="header-band flex h-14 items-center justify-between border-b border-line px-4"
		data-testid="header-band"
	>
		<a
			href={resolve('/')}
			class="flex items-center gap-2.5 font-sans text-base font-semibold tracking-[-0.01em] text-fg"
		>
			<span class="text-brand flex items-center">
				<BrandMark size={24} {mark} />
			</span>
			<span>{m.app_name()}</span>
		</a>
	</div>

	{#if onOpenSearch}
		<div class="px-3 pt-3">
			<button
				type="button"
				onclick={onOpenSearch}
				class="flex h-9 w-full items-center justify-between rounded-md border border-line bg-bg px-2.5 text-xs text-subtle transition-colors hover:border-control hover:text-fg"
				aria-keyshortcuts="Control+K Meta+K"
			>
				<span class="flex items-center gap-2">
					<NavIcon name="search" size={14} class="text-subtle" />
					<span>{m.nav_search()}</span>
				</span>
				<kbd
					class="rounded border border-line bg-surface px-1.5 py-0.5 text-[10px] font-medium text-subtle"
				>
					{shortcutLabel}
				</kbd>
			</button>
		</div>
	{/if}

	<nav class="flex-1 overflow-y-auto px-3 py-3" aria-label={m.app_name()}>
		<NavList items={effectiveNavItems} />

		<KofiLink />
	</nav>

	<NavFooter localeId="desktop-lang" {onLogout} />
</aside>
