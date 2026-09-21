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
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, locales, setLocale, type Locale } from '$lib/paraglide/runtime.js';
	import { queryClient } from '$lib/query.js';
	import { session } from '$lib/session.svelte.js';

	const { children } = $props();
	const toast = toastPreset('dashboard');
	const toastStyle = Object.entries(toast.style)
		.map(([k, v]) => `${k}:${v}`)
		.join(';');

	const publicRoutes = new Set(['/login', '/setup']);
	let ready = $state(false);

	/** Sends the visitor where the session state says they belong. */
	async function route(): Promise<void> {
		const state = await session.refresh();
		const here = page.url.pathname;
		if (state === 'setup' && here !== '/setup') await goto(resolve('/setup'));
		else if (state === 'anonymous' && !publicRoutes.has(here)) await goto(resolve('/login'));
		else if (state === 'authenticated' && publicRoutes.has(here)) await goto(resolve('/'));
		ready = true;
	}

	onMount(() => {
		void route();
	});

	const nav = $derived([
		{ href: '/', label: m.nav_dashboard() },
		{ href: '/instances', label: m.nav_instances() },
		{ href: '/history', label: m.nav_history() },
		{ href: '/live', label: m.nav_live() },
		{ href: '/schedules', label: m.nav_schedules() },
		{ href: '/download-clients', label: m.nav_download_clients() },
		{ href: '/seerr', label: m.nav_seerr() },
		{ href: '/cleanuparr', label: m.nav_cleanuparr() },
		{ href: '/settings', label: m.nav_settings() }
	] as const);

	async function logout(): Promise<void> {
		await session.logout();
		await goto(resolve('/login'));
	}

	function changeLocale(event: Event): void {
		const value = (event.currentTarget as HTMLSelectElement).value as Locale;
		setLocale(value);
	}
</script>

<svelte:head>
	<title>{m.app_name()}</title>
</svelte:head>

<QueryClientProvider client={queryClient}>
	<a
		href="#main"
		class="btn sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50"
		>{m.skip_to_content()}</a
	>
	<div class="flex min-h-screen flex-col md:flex-row">
		{#if session.state === 'authenticated'}
			<nav
				aria-label={m.app_name()}
				class="border-b border-line p-4 md:w-56 md:border-r md:border-b-0"
			>
				<a href={resolve('/')} class="mb-4 flex items-center gap-2 text-lg font-semibold">
					<img src="/brand/mark.svg" alt="" width="28" height="28" />
					{m.app_name()}
				</a>
				<ul class="flex flex-wrap gap-1 md:flex-col">
					{#each nav as item (item.href)}
						<li>
							<a
								href={resolve(item.href)}
								aria-current={page.url.pathname === item.href ? 'page' : undefined}
								class="block rounded-md px-3 py-2 hover:bg-surface aria-[current=page]:bg-surface aria-[current=page]:font-semibold"
								>{item.label}</a
							>
						</li>
					{/each}
				</ul>
				<div class="mt-6 flex flex-col gap-2 text-sm">
					<label class="flex flex-col gap-1">
						<span>{m.language()}</span>
						<select class="select" value={getLocale()} onchange={changeLocale}>
							{#each locales as loc (loc)}
								<option value={loc}>{loc}</option>
							{/each}
						</select>
					</label>
					<button type="button" class="btn" onclick={logout}>{m.nav_logout()}</button>
				</div>
			</nav>
		{/if}
		<main id="main" class="flex-1 p-4 md:p-6" tabindex="-1">
			{#if ready}
				{@render children()}
			{:else}
				<p aria-live="polite">{m.loading()}</p>
			{/if}
		</main>
	</div>
	<Toaster position={toast.position} toastOptions={{ style: toastStyle }} richColors />
</QueryClientProvider>
