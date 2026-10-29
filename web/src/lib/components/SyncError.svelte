<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { isDialTimeout } from '#lib/egress.js';
	import { m } from '#lib/paraglide/messages.js';

	// An error from a call the API made to an outside service. A dial timeout gets a short
	// hint that a network policy may block the egress; the raw error stays one click away.

	interface Props {
		error: string;
		/** Formats the raw error when it is shown on its own. */
		format?: (error: string) => string;
	}

	const { error, format = (e) => e }: Props = $props();
</script>

{#if isDialTimeout(error)}
	<div class="text-danger" data-testid="egress-hint">
		<p>{m.egress_blocked_hint()}</p>
		<details class="text-subtle">
			<summary class="cursor-pointer">{m.egress_raw_error()}</summary>
			<code class="break-all">{error}</code>
		</details>
	</div>
{:else}
	<p class="text-danger">{format(error)}</p>
{/if}
