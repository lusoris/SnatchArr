<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import '../app.css';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { QueryClientProvider, setQueryClientContext } from '@tanstack/svelte-query';
	import { Toaster } from 'svelte-sonner';
	import { toastPreset } from '@sveltesentio/ui/toast';
	import { createSentioQuery } from '@sveltesentio/query';
	import { ProblemError } from '@sveltesentio/core';
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { getNavItems, isRouteDisabled } from '#lib/components/shell/nav.js';
	import BottomBar from '#lib/components/shell/BottomBar.svelte';
	import CommandDialog from '#lib/components/shell/CommandDialog.svelte';
	import NavDrawer from '#lib/components/shell/NavDrawer.svelte';
	import Sidebar from '#lib/components/shell/Sidebar.svelte';
	import TopBar from '#lib/components/shell/TopBar.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { api, PUBLIC_ROUTES } from '#lib/api/client.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys, queryClient } from '#lib/query.js';
	import { session } from '#lib/session.svelte.js';

	setQueryClientContext(queryClient);

	const { children } = $props();
	const toast = toastPreset('dashboard');
	const toastStyle = Object.entries(toast.style)
		.map(([k, v]) => `${k}:${v}`)
		.join(';');

	const isPublic = $derived(PUBLIC_ROUTES.has(page.url.pathname));

	let drawerOpen = $state(false);
	let cmdOpen = $state(false);
	let menuButtonEl = $state<HTMLButtonElement | null>(null);

	const modulesQuery = createSentioQuery(() => ({
		queryKey: keys.modules,
		enabled: session.state === 'authenticated',
		queryFn: async () => {
			const res = await api.GET('/system/modules');
			return res.data ?? [];
		}
	}));

	const activeModules = $derived.by(() => {
		const list = modulesQuery.data;
		if (!list || modulesQuery.isError) {
			return new SvelteSet<string>();
		}
		const set = new SvelteSet<string>();
		for (const mod of list) {
			if (mod.enabled) {
				set.add(mod.id);
			}
		}
		return set;
	});

	const navItems = $derived(getNavItems(activeModules));
	const routeGuard = $derived(isRouteDisabled(page.url.pathname, activeModules));

	const disabledRouteProblem = $derived.by(() => {
		if (!routeGuard.disabled) return null;
		return new ProblemError({
			type: 'urn:problem:not-found',
			status: 404,
			title: m.page_not_found_title(),
			detail: m.module_disabled_detail({ module: routeGuard.moduleId ?? '' })
		});
	});

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
		} else if (state === 'anonymous' && !session.error && !PUBLIC_ROUTES.has(here)) {
			await goto(resolve('login'));
		} else if (state === 'authenticated' && PUBLIC_ROUTES.has(here)) {
			await goto(resolve('/'));
		}
	}

	onMount(() => {
		// app.html is built with lang="en"; setLocale reloads the page, so setting it once per
		// load keeps html[lang] on the active locale (WCAG 3.1.1).
		document.documentElement.lang = getLocale();
		void checkSession();
	});

	$effect(() => {
		const here = page.url.pathname;
		drawerOpen = false;
		if (session.state === 'anonymous' && !session.error && !PUBLIC_ROUTES.has(here)) {
			void goto(resolve('login'));
		}
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

			<Sidebar {navItems} {activeModules} onLogout={logout} onOpenSearch={openSearch} />

			<NavDrawer
				open={drawerOpen}
				onClose={closeDrawer}
				onLogout={logout}
				{menuButtonEl}
				{activeModules}
			/>

			<BottomBar onOpenDrawer={openDrawer} {activeModules} />

			<CommandDialog {navItems} {activeModules} bind:open={cmdOpen} />

			<main
				id="main"
				class="min-h-screen p-4 pb-[calc(4rem+env(safe-area-inset-bottom,0px))] md:p-6 md:pb-6 lg:pl-[16.5rem]"
				tabindex="-1"
			>
				<div class="mx-auto max-w-[80rem]">
					<svelte:boundary>
						{#if session.state === 'unknown' || session.state === 'anonymous'}
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
						{:else if routeGuard.disabled}
							<div class="flex flex-col gap-4 py-8" data-testid="not-found-view">
								<h1 class="text-2xl font-bold tracking-tight">{m.page_not_found_title()}</h1>
								<p class="text-sm text-subtle">{m.page_not_found_text()}</p>
								<ProblemNote error={disabledRouteProblem} />
								<div>
									<a href={resolve('/')} class="btn btn-outline inline-flex items-center gap-2">
										{m.page_not_found_back()}
									</a>
								</div>
							</div>
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
