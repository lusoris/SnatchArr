<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { api } from '#lib/api/client.js';
	import AuthCard from '#lib/components/AuthCard.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';
	import JellyfinLoginCard from './JellyfinLoginCard.svelte';
	import LocalLoginForm from './LocalLoginForm.svelte';

	let ssoOidc = $state(false);
	let ssoJellyfin = $state(false);
	let ssoQuickConnect = $state(false);
	let fallbackOpen = $state(false);
	let ssoFailed = $derived(page.url.searchParams.get('sso') === 'failed');

	$effect(() => {
		void (async () => {
			try {
				const res = await api.GET('/auth/sso');
				if (res.data) {
					ssoOidc = res.data.oidc;
					ssoJellyfin = Boolean(res.data.jellyfin);
					ssoQuickConnect = Boolean(res.data.jellyfinQuickConnect);
					if (res.data.proxy) {
						await tryProxyLogin();
					}
				}
			} catch (err) {
				console.debug('sso discovery failed', err);
			}
		})();
	});

	async function tryProxyLogin(): Promise<void> {
		try {
			const res = await api.POST('/auth/proxy-session');
			if (res.data) {
				await session.refresh();
				await goto(resolve('/'));
			}
		} catch (err) {
			console.debug('proxy auth session failed', err);
		}
	}

	function toggleFallback(event: MouseEvent): void {
		event.preventDefault();
		fallbackOpen = !fallbackOpen;
	}
</script>

<AuthCard title={m.login_title()}>
	{#snippet notice()}
		{#if ssoFailed}
			<p role="alert" class="mb-4 text-sm text-danger">{m.login_sso_failed()}</p>
		{/if}
	{/snippet}
	{#if ssoJellyfin}
		<JellyfinLoginCard quickConnectEnabled={ssoQuickConnect} {ssoOidc} />
		<details class="card mt-4" open={fallbackOpen}>
			<summary class="cursor-pointer text-sm font-medium select-none" onclick={toggleFallback}>
				{m.login_admin_fallback()}
			</summary>
			{#if fallbackOpen}
				<LocalLoginForm showOidc={false} embedded={true} />
			{/if}
		</details>
	{:else}
		<LocalLoginForm showOidc={ssoOidc} embedded={false} />
	{/if}
</AuthCard>
