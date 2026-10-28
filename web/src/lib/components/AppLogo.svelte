<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { asset } from '$app/paths';
	import type { AssetPath } from '$app/types';
	import ArrowDownUp from '@lucide/svelte/icons/arrow-down-up';
	import MessageSquare from '@lucide/svelte/icons/message-square';
	import MonitorPlay from '@lucide/svelte/icons/monitor-play';
	import logosData from '#lib/logos/logos.json';

	// Decorative mark beside an app name the caller already shows. Vendors whose terms bar
	// integration-icon use are declared exceptions in logos.json and get a category icon.
	// tools/logos/fetch.py --check proves every manifest file exists under static/logos/.
	interface Props {
		kind: string;
		size?: number;
		class?: string;
	}

	const { kind, size = 20, class: className = '' }: Props = $props();

	interface LogoEntry {
		file?: string;
		exception?: 'chat' | 'torrent' | 'media-server';
	}

	const categoryIcons = { chat: MessageSquare, torrent: ArrowDownUp, 'media-server': MonitorPlay };
	const entry = $derived((logosData as Record<string, LogoEntry>)[kind]);
	const Icon = $derived(entry?.exception ? categoryIcons[entry.exception] : undefined);
</script>

{#if entry?.file}
	<img
		src={asset(`logos/${entry.file}` as AssetPath)}
		alt=""
		width={size}
		height={size}
		loading="lazy"
		decoding="async"
		class="shrink-0 {className}"
	/>
{:else if Icon}
	<Icon {size} aria-hidden="true" class="shrink-0 {className}" />
{/if}
