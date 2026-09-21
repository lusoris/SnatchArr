<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { describeError } from '$lib/api/client.js';
	import { m } from '$lib/paraglide/messages.js';

	interface Props {
		error: unknown;
	}

	const { error }: Props = $props();
</script>

{#if error}
	<p role="alert" class="rounded-md border border-danger bg-danger/10 px-3 py-2 text-sm">
		{m.error_generic({ detail: describeError(error) })}
	</p>
{/if}
