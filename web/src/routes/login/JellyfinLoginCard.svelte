<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, describeError } from '#lib/api/client.js';
	import CredentialFields from '#lib/components/CredentialFields.svelte';
	import { pageHref } from '#lib/entities.js';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';
	import { pollQuickConnect } from './quick-connect.js';

	let {
		quickConnectEnabled = false,
		ssoOidc = false
	}: {
		quickConnectEnabled?: boolean;
		ssoOidc?: boolean;
	} = $props();

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	let qcCode = $state('');
	let qcBusy = $state(false);
	let qcAbort: AbortController | null = null;

	onDestroy(() => {
		qcAbort?.abort();
	});

	function cancelQuickConnect(): void {
		qcAbort?.abort();
		qcAbort = null;
		qcCode = '';
		qcBusy = false;
	}

	async function submitJellyfin(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		cancelQuickConnect();
		busy = true;
		error = '';
		try {
			const res = await api.POST('/auth/jellyfin', { body: { username, password } });
			if (res.data) {
				await session.refresh();
				await goto(pageHref('/'));
			}
		} catch (err) {
			error = describeError(err);
		} finally {
			busy = false;
		}
	}

	async function startQuickConnect(): Promise<void> {
		cancelQuickConnect();
		error = '';
		qcBusy = true;
		const controller = new AbortController();
		qcAbort = controller;
		try {
			const init = await api.POST('/auth/jellyfin/quickconnect/initiate');
			if (!init.data || controller.signal.aborted) return;
			qcCode = init.data.code;
			const outcome = await pollQuickConnect(init.data.secret, controller.signal);
			if (outcome === 'authenticated') {
				await session.refresh();
				await goto(pageHref('/'));
			} else if (outcome === 'timeout') {
				qcCode = '';
				error = m.login_jellyfin_quick_connect_timeout();
			}
		} catch (err) {
			if (!controller.signal.aborted) {
				qcCode = '';
				error = describeError(err);
			}
		} finally {
			if (qcAbort === controller) {
				qcBusy = false;
				qcAbort = null;
			}
		}
	}
</script>

<div class="card flex flex-col gap-4">
	<form class="flex flex-col gap-4" onsubmit={submitJellyfin}>
		<CredentialFields
			usernameId="jellyfin-username"
			passwordId="jellyfin-password"
			usernameLabel={m.login_jellyfin_username()}
			passwordLabel={m.login_jellyfin_password()}
			bind:username
			bind:password
			{error}
		/>
		<button type="submit" class="btn btn-primary" disabled={busy}>
			{m.login_jellyfin_submit()}
		</button>
	</form>

	{#if quickConnectEnabled}
		{#if qcCode}
			<div class="border-border flex flex-col gap-2 rounded border p-3" aria-live="polite">
				<p class="font-mono text-base font-semibold">
					{m.login_jellyfin_quick_connect_code({ code: qcCode })}
				</p>
				<p class="text-xs text-subtle">{m.login_jellyfin_quick_connect_hint()}</p>
				<p class="text-xs text-subtle">{m.login_jellyfin_quick_connect_waiting()}</p>
				<button type="button" class="btn" onclick={cancelQuickConnect}>
					{m.login_jellyfin_quick_connect_cancel()}
				</button>
			</div>
		{:else}
			<button type="button" class="btn" disabled={qcBusy} onclick={startQuickConnect}>
				{m.login_jellyfin_quick_connect()}
			</button>
		{/if}
	{/if}

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
</div>
