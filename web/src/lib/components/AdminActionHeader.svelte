<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { session } from '#lib/session.svelte.js';
</script>

{#if session.isAdmin}
	<th scope="col"><span class="sr-only">{m.action_actions()}</span></th>
{/if}
