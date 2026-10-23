<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import TextField from './TextField.svelte';

	interface Props {
		value: string;
	}

	let { value = $bindable() }: Props = $props();
</script>

<TextField
	id="new-password"
	label={m.new_password()}
	hint={m.password_min_hint()}
	type="password"
	autocomplete="new-password"
	required
	minlength={12}
	bind:value
/>
