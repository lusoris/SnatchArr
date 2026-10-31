<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { FALLBACK_BRAND_MARK, type BrandMarkKind } from '#lib/brand-mark.js';
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { pageHref } from '#lib/entities.js';
	import { m } from '#lib/paraglide/messages.js';
	import { ADULT_NAV_ITEMS, filterByModule, type ModuledItem } from './nav.js';
	import NavIcon from './NavIcon.svelte';
	import NavList from './NavList.svelte';
	import KofiLink from './KofiLink.svelte';
	import NavFooter from './NavFooter.svelte';

	interface DrawerItem extends ModuledItem {
		href:
			| '/automation'
			| '/connections'
			| '/adult/sources'
			| '/adult/image-sets'
			| '/adult/performers'
			| '/adult/studios'
			| '/system'
			| '/account';
		label: string;
		icon: string;
	}

	interface Props {
		open: boolean;
		onClose: () => void;
		onLogout: () => void;
		menuButtonEl: HTMLButtonElement | null;
		activeModules?: ReadonlySet<string>;
		/** Effective brand mark, resolved by the layout (ADR-0042). Defaults to the fail-closed fallback. */
		mark?: BrandMarkKind;
	}

	const {
		open,
		onClose,
		onLogout,
		menuButtonEl,
		activeModules = new Set(),
		mark = FALLBACK_BRAND_MARK
	}: Props = $props();

	let dialogEl = $state<HTMLDialogElement | null>(null);
	let closeButtonEl = $state<HTMLButtonElement | null>(null);

	const drawerItems: readonly DrawerItem[] = [
		{ href: '/automation', label: m.nav_automation(), icon: 'clock' },
		{ href: '/connections', label: m.nav_connections(), icon: 'link' },
		...ADULT_NAV_ITEMS,
		{ href: '/system', label: m.nav_system(), icon: 'settings' },
		{ href: '/account', label: m.nav_account(), icon: 'user' }
	];

	const visibleItems = $derived(filterByModule(drawerItems, activeModules));

	$effect(() => {
		if (!dialogEl) return;
		if (open && !dialogEl.open) {
			dialogEl.showModal();
			closeButtonEl?.focus();
		} else if (!open && dialogEl.open) {
			dialogEl.close();
		}
	});

	function handleClose(): void {
		onClose();
		menuButtonEl?.focus();
	}

	function handleDialogClick(event: MouseEvent): void {
		const rect = dialogEl?.getBoundingClientRect();
		if (!rect) return;
		const isInside =
			rect.top <= event.clientY &&
			event.clientY <= rect.bottom &&
			rect.left <= event.clientX &&
			event.clientX <= rect.right;
		if (!isInside) {
			dialogEl?.close();
		}
	}
</script>

<dialog
	bind:this={dialogEl}
	id="mobile-nav-dialog"
	aria-label={m.app_name()}
	onclose={handleClose}
	onclick={handleDialogClick}
	class="fixed inset-y-0 left-0 z-50 m-0 hidden h-[100dvh] max-h-[100dvh] w-72 max-w-[85vw] flex-col border-0 border-r border-line bg-surface p-0 text-fg shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-xs open:flex lg:hidden"
>
	<div class="flex h-14 items-center justify-between border-b border-line px-4">
		<a
			href={pageHref('/')}
			class="flex items-center gap-2.5 font-sans text-base font-semibold tracking-[-0.01em] text-fg"
			onclick={onClose}
		>
			<span class="text-brand flex items-center">
				<BrandMark size={24} {mark} />
			</span>
			<span>{m.app_name()}</span>
		</a>
		<button
			bind:this={closeButtonEl}
			type="button"
			class="btn inline-flex h-11 min-h-11 w-11 items-center justify-center rounded border border-control p-0 md:h-8 md:min-h-0 md:w-8"
			onclick={onClose}
			aria-label={m.nav_close_menu()}
		>
			<NavIcon name="x" size={18} />
		</button>
	</div>

	<nav class="flex-1 overflow-y-auto px-3 py-4" aria-label={m.app_name()}>
		<NavList items={visibleItems} onItemClick={onClose} gapClass="gap-1" />

		<KofiLink onclick={onClose} />
	</nav>

	<NavFooter localeId="mobile-lang" {onLogout} onNavigate={onClose} />
</dialog>
