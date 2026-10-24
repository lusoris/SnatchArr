<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { splitTextUrls } from '#lib/url-segments.js';
	import UrlText from './UrlText.svelte';

	interface Props {
		text: string;
		class?: string;
	}

	const { text, class: className = '' }: Props = $props();

	const parts = $derived(splitTextUrls(text));
</script>

<span class={className}
	>{#each parts as part, i (i)}{#if part.url}<UrlText
				url={part.text}
			/>{:else}{part.text}{/if}{/each}</span
>
