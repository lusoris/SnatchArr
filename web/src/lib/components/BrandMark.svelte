<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	interface Props {
		/** Display size in pixels (applied to width and height). Default 28. */
		size?: number;
		/** Optional CSS classes. */
		class?: string;
		/** When provided, the mark is labelled (role="img" + aria-label). Otherwise aria-hidden="true". */
		label?: string;
	}

	const { size = 28, class: className = '', label }: Props = $props();
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	width={size}
	height={size}
	viewBox="0 0 32 32"
	fill="none"
	stroke="currentColor"
	stroke-width="2"
	stroke-linecap="round"
	stroke-linejoin="round"
	class="shrink-0 {className}"
	role={label ? 'img' : undefined}
	aria-label={label}
	aria-hidden={label ? undefined : 'true'}
>
	<!-- M3: Pure Geometry (Vesica Piscis / Almond) -->
	<!-- Outer vesica arc -->
	<path d="M 16 3 C 6 11 6 21 16 29 C 26 21 26 11 16 3 Z" />
	<!-- Inner concentric almond aperture -->
	<path d="M 16 8 C 10 13 10 19 16 24 C 22 19 22 13 16 8 Z" />
	<!-- Center focal jewel -->
	<circle cx="16" cy="14" r="1.6" fill="currentColor" stroke="none" />
	<!-- Lower focal ray -->
	<path d="M 16 18 L 16 21" />
</svg>
