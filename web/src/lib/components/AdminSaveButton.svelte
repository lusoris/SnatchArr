<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';

	interface Props {
		disabled?: boolean;
	}

	const { disabled = false }: Props = $props();
</script>

{#if session.isAdmin}
	<div>
		<button type="submit" class="btn btn-primary" {disabled}>
			{m.instance_save()}
		</button>
	</div>
{/if}
