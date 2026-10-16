<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { api, describeError } from '#lib/api/client.js';
	import Field from '#lib/components/Field.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	let ssoOidc = $state(false);
	let ssoFailed = $derived(page.url.searchParams.get('sso') === 'failed');

	$effect(() => {
		void (async () => {
			try {
				const res = await api.GET('/auth/sso');
				if (res.data) {
					ssoOidc = res.data.oidc;
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

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await session.login(username, password);
			await goto(resolve('/'));
		} catch (err) {
			error = describeError(err);
		} finally {
			busy = false;
		}
	}
</script>

<div class="mx-auto max-w-sm">
	<h1 class="mb-4 text-2xl font-semibold">{m.login_title()}</h1>
	{#if ssoFailed}
		<p role="alert" class="mb-4 text-sm text-danger">{m.login_sso_failed()}</p>
	{/if}
	<form class="card flex flex-col gap-4" onsubmit={submit}>
		<Field id="username" label={m.username()}>
			<input id="username" class="input" autocomplete="username" required bind:value={username} />
		</Field>
		<Field id="password" label={m.password()}>
			<input
				id="password"
				class="input"
				type="password"
				autocomplete="current-password"
				required
				bind:value={password}
			/>
		</Field>
		{#if error}
			<p role="alert" class="text-sm text-danger">{error}</p>
		{/if}
		<button type="submit" class="btn btn-primary" disabled={busy}>{m.login_submit()}</button>
		{#if ssoOidc}
			<a
				href="/api/v1/auth/oidc/login"
				rel="external"
				class="btn justify-center text-center"
				data-sveltekit-reload
			>
				{m.login_sso()}
			</a>
		{/if}
	</form>
</div>
