<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createAuthSubmit } from '#lib/auth-submit.svelte.js';
	import CredentialFields from '#lib/components/CredentialFields.svelte';
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
	const auth = createAuthSubmit(() => session.login(username, password));
</script>

<form
	class={embedded ? 'mt-4 flex flex-col gap-4' : 'card flex flex-col gap-4'}
	onsubmit={auth.submit}
>
	<CredentialFields
		usernameLabel={m.username()}
		passwordLabel={m.password()}
		bind:username
		bind:password
		error={auth.error}
	/>
	<button type="submit" class="btn btn-primary" disabled={auth.busy}>{m.login_submit()}</button>
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
