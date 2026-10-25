<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { isRouteActive } from './nav.js';
	import NavIcon from './NavIcon.svelte';
	import KofiLink from './KofiLink.svelte';
	import NavFooter from './NavFooter.svelte';

	interface Props {
		open: boolean;
		onClose: () => void;
		onLogout: () => void;
		menuButtonEl: HTMLButtonElement | null;
	}

	const { open, onClose, onLogout, menuButtonEl }: Props = $props();

	let dialogEl = $state<HTMLDialogElement | null>(null);
	let closeButtonEl = $state<HTMLButtonElement | null>(null);

	const drawerItems = [
		{ href: '/automation', label: m.nav_automation(), icon: 'clock' },
		{ href: '/connections', label: m.nav_connections(), icon: 'link' },
		{ href: '/system', label: m.nav_system(), icon: 'settings' },
		{ href: '/account', label: m.nav_account(), icon: 'user' }
	] as const;

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
			href={resolve('/')}
			class="flex items-center gap-2.5 font-sans text-base font-semibold tracking-[-0.01em] text-fg"
			onclick={onClose}
		>
			<span class="text-brand flex items-center">
				<BrandMark size={24} />
			</span>
			<span>{m.app_name()}</span>
		</a>
		<button
			bind:this={closeButtonEl}
			type="button"
			class="btn inline-flex h-8 min-h-0 w-8 items-center justify-center rounded border border-control p-0"
			onclick={onClose}
			aria-label={m.nav_close_menu()}
		>
			<NavIcon name="x" size={18} />
		</button>
	</div>

	<nav class="flex-1 overflow-y-auto px-3 py-4" aria-label={m.app_name()}>
		<ul class="flex flex-col gap-1">
			{#each drawerItems as item (item.href)}
				{@const active = isRouteActive(page.url.pathname, item.href)}
				<li>
					<a
						href={resolve(item.href)}
						aria-current={active ? 'page' : undefined}
						onclick={onClose}
						class="nav-link flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium"
					>
						<NavIcon name={item.icon} size={16} class={active ? 'text-brand' : 'text-subtle'} />
						<span>{item.label}</span>
					</a>
				</li>
			{/each}
		</ul>

		<KofiLink onclick={onClose} />
	</nav>

	<NavFooter localeId="mobile-lang" {onLogout} onNavigate={onClose} />
</dialog>
