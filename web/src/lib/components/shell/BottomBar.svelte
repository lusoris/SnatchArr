<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';
	import { filterByModule, isRouteActive, type ModuledItem } from './nav.js';
	import NavIcon from './NavIcon.svelte';

	interface TabItem extends ModuledItem {
		href: '/' | '/activity' | '/instances';
		label: string;
		icon: string;
	}

	interface Props {
		onOpenDrawer: (opener?: HTMLButtonElement) => void;
		activeModules?: ReadonlySet<string>;
	}

	const { onOpenDrawer, activeModules = new Set() }: Props = $props();
	let moreButtonEl = $state<HTMLButtonElement | null>(null);

	const primaryTabs: readonly TabItem[] = [
		{ href: '/', label: m.nav_overview(), icon: 'layout-dashboard' },
		{ href: '/activity', label: m.nav_activity(), icon: 'activity' },
		{ href: '/instances', label: m.nav_instances(), icon: 'server' }
	];

	const visibleTabs = $derived(filterByModule(primaryTabs, activeModules));
</script>

<nav
	aria-label={m.nav_bottom_label()}
	class="fixed inset-x-0 bottom-0 z-40 flex h-[calc(4rem+env(safe-area-inset-bottom,0px))] items-center justify-around border-t border-line bg-surface/95 px-2 pb-[env(safe-area-inset-bottom,0px)] backdrop-blur-md md:hidden"
>
	{#each visibleTabs as tab (tab.href)}
		{@const active = isRouteActive(page.url.pathname, tab.href)}
		<a
			href={resolve(tab.href)}
			aria-current={active ? 'page' : undefined}
			class="flex min-h-11 flex-1 flex-col items-center justify-center gap-1 py-1 text-center transition-colors {active
				? 'text-brand font-semibold'
				: 'text-subtle hover:text-fg'}"
		>
			<NavIcon name={tab.icon} size={20} class={active ? 'text-brand' : 'text-subtle'} />
			<span class="text-[11px] leading-none">{tab.label}</span>
		</a>
	{/each}
	<button
		bind:this={moreButtonEl}
		type="button"
		class="flex min-h-11 flex-1 flex-col items-center justify-center gap-1 py-1 text-center text-subtle transition-colors hover:text-fg"
		onclick={() => onOpenDrawer(moreButtonEl ?? undefined)}
		aria-label={m.nav_bottom_more()}
	>
		<NavIcon name="ellipsis" size={20} class="text-subtle" />
		<span class="text-[11px] leading-none">{m.nav_bottom_more()}</span>
	</button>
</nav>
