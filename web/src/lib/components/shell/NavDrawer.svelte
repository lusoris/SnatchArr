<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { isRouteActive, type NavGroup } from './nav.js';
	import NavIcon from './NavIcon.svelte';
	import NavGroups from './NavGroups.svelte';
	import LocaleSelect from './LocaleSelect.svelte';
	import KofiLink from './KofiLink.svelte';
	import VersionLabel from './VersionLabel.svelte';

	interface Props {
		open: boolean;
		navGroups: NavGroup[];
		onClose: () => void;
		onLogout: () => void;
		menuButtonEl: HTMLButtonElement | null;
	}

	const { open, navGroups, onClose, onLogout, menuButtonEl }: Props = $props();

	let dialogEl = $state<HTMLDialogElement | null>(null);
	let closeButtonEl = $state<HTMLButtonElement | null>(null);

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
		<NavGroups
			{navGroups}
			onNavigate={onClose}
			groupLabelClass="mb-2 px-3 text-[11px] font-bold tracking-wider text-subtle uppercase"
			itemListClass="flex flex-col gap-1"
		/>

		<KofiLink onclick={onClose} />
	</nav>

	<div class="flex flex-col gap-3 border-t border-line p-3 text-xs">
		<LocaleSelect id="mobile-lang" />

		<div class="flex items-center justify-between gap-2 pt-1">
			<a
				href={resolve('account')}
				aria-current={isRouteActive(page.url.pathname, '/account') ? 'page' : undefined}
				onclick={onClose}
				class="nav-link flex items-center gap-2 rounded px-2 py-1.5 text-sm text-fg"
			>
				<NavIcon name="user" size={16} class="text-subtle" />
				<span>{m.nav_account()}</span>
			</a>
			<button
				type="button"
				class="btn inline-flex min-h-0 items-center gap-1.5 rounded px-2 py-1.5 text-sm text-subtle hover:border-danger hover:text-danger"
				onclick={() => {
					onClose();
					onLogout();
				}}
			>
				<NavIcon name="log-out" size={15} />
				<span>{m.nav_logout()}</span>
			</button>
		</div>

		<VersionLabel />
	</div>
</dialog>
