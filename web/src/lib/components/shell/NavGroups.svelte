<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isRouteActive, type NavGroup } from './nav.js';
	import NavIcon from './NavIcon.svelte';

	interface Props {
		navGroups: NavGroup[];
		onNavigate?: () => void;
		groupLabelClass?: string;
		itemListClass?: string;
	}

	const {
		navGroups,
		onNavigate,
		groupLabelClass = 'mb-1.5 px-3 text-[11px] font-bold tracking-wider text-subtle uppercase',
		itemListClass = 'flex flex-col gap-0.5'
	}: Props = $props();
</script>

<div class="flex flex-col gap-5">
	{#each navGroups as group (group.label)}
		<div>
			<div class={groupLabelClass}>
				{group.label}
			</div>
			<ul class={itemListClass}>
				{#each group.items as item (item.href)}
					{@const active = isRouteActive(page.url.pathname, item.href)}
					<li>
						<a
							href={resolve(item.href)}
							aria-current={active ? 'page' : undefined}
							onclick={onNavigate}
							class="nav-link flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium"
						>
							<NavIcon name={item.icon} size={16} class={active ? 'text-brand' : 'text-subtle'} />
							<span>{item.label}</span>
						</a>
					</li>
				{/each}
			</ul>
		</div>
	{/each}
</div>
