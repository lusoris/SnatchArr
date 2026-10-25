<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { splitUrl } from '#lib/url-segments.js';

	interface Props {
		url: string;
		title?: string;
		class?: string;
	}

	const { url, title, class: className = '' }: Props = $props();

	const segments = $derived(splitUrl(url));
</script>

<span
	data-url
	title={title ?? (url || undefined)}
	class={className ? `${className} [overflow-wrap:break-word]` : '[overflow-wrap:break-word]'}
	>{#each segments as segment, i (i)}{#if i > 0}<wbr />{/if}{segment}{/each}</span
>
