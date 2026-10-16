<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import TextField from './TextField.svelte';

	interface Props {
		id: string;
		label: string;
		/** Starts empty, always: the API never returns a secret, and the form never prefills one. */
		value: string;
		/** Editing a stored record: empty keeps the stored secret, so the field is optional. */
		editing: boolean;
		/** Whether a new record needs the secret. */
		requiredOnCreate?: boolean;
		error?: string;
	}

	let {
		id,
		label,
		value = $bindable(),
		editing,
		requiredOnCreate = false,
		error
	}: Props = $props();
</script>

<TextField
	{id}
	{label}
	type="password"
	autocomplete="new-password"
	required={!editing && requiredOnCreate}
	hint={editing ? m.secret_keep_hint() : m.secret_new_hint()}
	{error}
	bind:value
/>
