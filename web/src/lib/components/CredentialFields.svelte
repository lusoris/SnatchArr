<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import Field from './Field.svelte';

	interface Props {
		usernameId?: string;
		passwordId?: string;
		usernameLabel: string;
		passwordLabel: string;
		username?: string;
		password?: string;
		error?: string;
		usernameMinlength?: number;
	}

	let {
		usernameId = 'username',
		passwordId = 'password',
		usernameLabel,
		passwordLabel,
		username = $bindable(''),
		password = $bindable(''),
		error = '',
		usernameMinlength
	}: Props = $props();
</script>

<Field id={usernameId} label={usernameLabel}>
	<input
		id={usernameId}
		class="input"
		autocomplete="username"
		required
		minlength={usernameMinlength}
		bind:value={username}
	/>
</Field>
<Field id={passwordId} label={passwordLabel}>
	<input
		id={passwordId}
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
