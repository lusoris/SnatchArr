<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { isApplePlatform } from '@sveltesentio/ui/cmd';
	import { page } from '$app/state';
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale, locales, setLocale, type Locale } from '#lib/paraglide/runtime.js';
	import { isRouteActive, type NavGroup } from './nav.js';
	import NavIcon from './NavIcon.svelte';

	interface Props {
		navGroups: NavGroup[];
		onLogout: () => void;
		onOpenSearch?: () => void;
	}

	const { navGroups, onLogout, onOpenSearch }: Props = $props();
	const shortcutLabel = isApplePlatform() ? m.nav_search_shortcut_apple() : m.nav_search_shortcut();

	function changeLocale(event: Event): void {
		const value = (event.currentTarget as HTMLSelectElement).value as Locale;
		setLocale(value);
	}
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
				<BrandMark size={24} />
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
				aria-label={m.nav_search()}
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
		<div class="flex flex-col gap-5">
			{#each navGroups as group (group.label)}
				<div>
					<div class="mb-1.5 px-3 text-[11px] font-bold tracking-wider text-subtle uppercase">
						{group.label}
					</div>
					<ul class="flex flex-col gap-0.5">
						{#each group.items as item (item.href)}
							{@const active = isRouteActive(page.url.pathname, item.href)}
							<li>
								<a
									href={resolve(item.href)}
									aria-current={active ? 'page' : undefined}
									class="nav-link flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium"
								>
									<NavIcon
										name={item.icon}
										size={16}
										class={active ? 'text-brand' : 'text-subtle'}
									/>
									<span>{item.label}</span>
								</a>
							</li>
						{/each}
					</ul>
				</div>
			{/each}
		</div>
	</nav>

	<div class="flex flex-col gap-3 border-t border-line p-3 text-xs">
		<div class="flex items-center justify-between gap-2">
			<label for="desktop-lang" class="font-medium text-subtle">{m.language()}</label>
			<select
				id="desktop-lang"
				class="select h-8 min-h-0 w-24 rounded border border-control bg-bg px-2 text-xs"
				value={getLocale()}
				onchange={changeLocale}
			>
				{#each locales as loc (loc)}
					<option value={loc}>{loc}</option>
				{/each}
			</select>
		</div>

		<div class="flex items-center justify-between gap-2 pt-1">
			<a
				href={resolve('account')}
				aria-current={isRouteActive(page.url.pathname, '/account') ? 'page' : undefined}
				class="nav-link flex items-center gap-2 rounded px-2 py-1.5 text-sm text-fg"
			>
				<NavIcon name="user" size={16} class="text-subtle" />
				<span>{m.nav_account()}</span>
			</a>
			<button
				type="button"
				class="btn inline-flex min-h-0 items-center gap-1.5 rounded px-2 py-1.5 text-sm text-subtle hover:border-danger hover:text-danger"
				onclick={onLogout}
			>
				<NavIcon name="log-out" size={15} />
				<span>{m.nav_logout()}</span>
			</button>
		</div>
	</div>
</aside>
