<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { useSSE } from '@sveltesentio/realtime/use-sse';
	import { m } from '$lib/paraglide/messages.js';

	interface Frame {
		ts?: string;
		level?: string;
		type?: string;
		title?: string;
		detail?: string;
	}

	const sse = useSSE({
		url: '/api/v1/events/stream',
		withCredentials: true,
		bufferMs: 100,
		historyLimit: 500
	});

	function parse(data: string): Frame {
		try {
			return JSON.parse(data) as Frame;
		} catch {
			return { title: data };
		}
	}
	const frames = $derived(
		sse.messages.map((msg) => ({ event: msg.type, ...parse(msg.data) })).reverse()
	);
</script>

<div class="mb-4 flex items-center justify-between">
	<h1 class="text-2xl font-semibold">{m.live_title()}</h1>
	<p class="flex items-center gap-2 text-sm" role="status">
		<span
			class="inline-block h-3 w-3 rounded-full"
			class:bg-green-600={sse.connected}
			class:bg-amber-500={!sse.connected}
			aria-hidden="true"
		></span>
		{sse.connected ? m.live_connected() : m.live_disconnected()}
	</p>
</div>

<ol
	role="log"
	aria-live="polite"
	aria-relevant="additions"
	class="flex flex-col gap-1 font-mono text-sm"
>
	{#each frames as f, i (i)}
		<li class="card px-3 py-1" data-level={f.level}>
			<span class="text-subtle">{f.ts?.slice(11, 19) ?? ''}</span>
			<span class="ml-2 font-semibold">{f.event}</span>
			<span class="ml-2">{f.title ?? ''}</span>
			{#if f.detail}<span class="ml-2 text-subtle">{f.detail}</span>{/if}
		</li>
	{/each}
</ol>
