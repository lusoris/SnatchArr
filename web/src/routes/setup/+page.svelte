<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { describeError } from '#lib/api/client.js';
	import AuthCard from '#lib/components/AuthCard.svelte';
	import Field from '#lib/components/Field.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';

	let username = $state('admin');
	let password = $state('');
	let setupSecret = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await session.setup(username, password, session.secretRequired ? setupSecret : undefined);
			await goto(resolve('/'));
		} catch (err) {
			error = describeError(err);
		} finally {
			busy = false;
		}
	}
</script>

<AuthCard title={m.setup_title()} intro={m.setup_intro()}>
	<form class="card flex flex-col gap-4" onsubmit={submit}>
		<Field id="username" label={m.username()}>
			<input
				id="username"
				class="input"
				autocomplete="username"
				required
				minlength="3"
				bind:value={username}
			/>
		</Field>
		<Field id="password" label={m.password()}>
			<input
				id="password"
				class="input"
				type="password"
				autocomplete="new-password"
				required
				minlength="12"
				bind:value={password}
			/>
		</Field>
		{#if session.secretRequired}
			<Field id="setup-secret" label={m.setup_secret()} hint={m.setup_secret_help()}>
				<input
					id="setup-secret"
					class="input"
					type="password"
					autocomplete="one-time-code"
					required
					bind:value={setupSecret}
				/>
			</Field>
		{/if}
		{#if error}
			<p role="alert" class="text-sm text-danger">{error}</p>
		{/if}
		<button type="submit" class="btn btn-primary" disabled={busy}>{m.setup_submit()}</button>
	</form>
</AuthCard>
