<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { describeError } from '#lib/api/client.js';
	import Field from '#lib/components/Field.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';

	let {
		showOidc = false,
		embedded = false
	}: {
		showOidc?: boolean;
		embedded?: boolean;
	} = $props();

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);

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

<form class={embedded ? 'mt-4 flex flex-col gap-4' : 'card flex flex-col gap-4'} onsubmit={submit}>
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
	{#if showOidc}
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
