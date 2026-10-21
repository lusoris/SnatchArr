<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		connected: boolean;
	}

	const { connected }: Props = $props();
</script>

<p class="flex items-center gap-2 text-sm" role="status">
	<span
		class="inline-block h-3 w-3 rounded-full"
		class:bg-brand={connected}
		class:bg-warning={!connected}
		aria-hidden="true"
	></span>
	{connected ? m.live_connected() : m.live_disconnected()}
</p>
