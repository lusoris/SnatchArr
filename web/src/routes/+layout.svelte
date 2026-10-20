<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import '../app.css';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { QueryClientProvider } from '@tanstack/svelte-query';
	import { Toaster } from 'svelte-sonner';
	import { toastPreset } from '@sveltesentio/ui/toast';
	import { onMount } from 'svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { getNavGroups } from '#lib/components/shell/nav.js';
	import BottomBar from '#lib/components/shell/BottomBar.svelte';
	import CommandDialog from '#lib/components/shell/CommandDialog.svelte';
	import NavDrawer from '#lib/components/shell/NavDrawer.svelte';
	import Sidebar from '#lib/components/shell/Sidebar.svelte';
	import TopBar from '#lib/components/shell/TopBar.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { queryClient } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	const { children } = $props();
	const toast = toastPreset('dashboard');
	const toastStyle = Object.entries(toast.style)
		.map(([k, v]) => `${k}:${v}`)
		.join(';');

	const publicRoutes = new Set(['/login', '/setup']);
	const isPublic = $derived(publicRoutes.has(page.url.pathname));

	let drawerOpen = $state(false);
	let cmdOpen = $state(false);
	let menuButtonEl = $state<HTMLButtonElement | null>(null);

	const navGroups = $derived(getNavGroups(session.isAdmin));

	function openDrawer(opener?: HTMLButtonElement): void {
		if (opener) {
			menuButtonEl = opener;
		}
		drawerOpen = true;
	}

	function closeDrawer(): void {
		drawerOpen = false;
	}

	function openSearch(): void {
		cmdOpen = true;
	}

	async function checkSession(): Promise<void> {
		const state = await session.refresh();
		const here = page.url.pathname;
		if (state === 'setup' && here !== '/setup') {
			await goto(resolve('setup'));
		} else if (state === 'anonymous' && !session.error && !publicRoutes.has(here)) {
			await goto(resolve('login'));
		} else if (state === 'authenticated' && publicRoutes.has(here)) {
			await goto(resolve('/'));
		}
	}

	onMount(() => {
		void checkSession();
	});

	$effect(() => {
		void page.url.pathname;
		drawerOpen = false;
	});

	async function logout(): Promise<void> {
		await session.logout();
		await goto(resolve('login'));
	}
</script>

<svelte:head>
	<title>{m.app_name()}</title>
</svelte:head>

<QueryClientProvider client={queryClient}>
	<a
		href="#main"
		class="btn sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-50"
		>{m.skip_to_content()}</a
	>

	{#if isPublic}
		<main id="main" class="flex min-h-screen items-center justify-center p-4" tabindex="-1">
			{@render children()}
		</main>
	{:else}
		<div class="min-h-screen bg-bg text-fg">
			<TopBar {drawerOpen} onOpenDrawer={openDrawer} onOpenSearch={openSearch} bind:menuButtonEl />

			<Sidebar {navGroups} onLogout={logout} onOpenSearch={openSearch} />

			<NavDrawer
				open={drawerOpen}
				{navGroups}
				onClose={closeDrawer}
				onLogout={logout}
				{menuButtonEl}
			/>

			<BottomBar onOpenDrawer={openDrawer} />

			<CommandDialog {navGroups} bind:open={cmdOpen} />

			<main
				id="main"
				class="min-h-screen p-4 pb-[calc(4rem+env(safe-area-inset-bottom,0px))] md:p-6 md:pb-6 lg:pl-[16.5rem]"
				tabindex="-1"
			>
				<div class="mx-auto max-w-[80rem]">
					<svelte:boundary>
						{#if session.state === 'unknown'}
							<div
								data-testid="skeleton"
								class="flex animate-pulse flex-col gap-4"
								role="status"
								aria-busy="true"
								aria-label={m.loading()}
							>
								<div data-testid="header-band" class="header-band h-8 w-48 rounded bg-line"></div>
								<div class="h-32 w-full rounded bg-line"></div>
								<div class="h-64 w-full rounded bg-line"></div>
							</div>
						{:else if session.error}
							<ProblemNote error={session.error} />
						{:else}
							{@render children()}
						{/if}
						{#snippet failed(error)}
							<ProblemNote {error} />
						{/snippet}
					</svelte:boundary>
				</div>
			</main>
		</div>
	{/if}
	<Toaster position={toast.position} toastOptions={{ style: toastStyle }} richColors />
</QueryClientProvider>
