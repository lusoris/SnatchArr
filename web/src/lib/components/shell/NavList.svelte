<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { page } from '$app/state';
	import { pageHref } from '#lib/entities.js';
	import { isRouteActive, type NavItem } from './nav.js';
	import NavIcon from './NavIcon.svelte';

	interface Props {
		items: readonly NavItem[];
		onItemClick?: () => void;
		gapClass?: string;
	}

	const { items, onItemClick, gapClass = 'gap-0.5' }: Props = $props();
</script>

<ul class="flex flex-col {gapClass}">
	{#each items as item (item.href)}
		{@const active = isRouteActive(page.url.pathname, item.href)}
		<li>
			<a
				href={pageHref(item.href)}
				aria-current={active ? 'page' : undefined}
				onclick={onItemClick}
				class="nav-link flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium"
			>
				<NavIcon name={item.icon} size={16} class={active ? 'text-brand' : 'text-subtle'} />
				<span>{item.label}</span>
			</a>
		</li>
	{/each}
</ul>
