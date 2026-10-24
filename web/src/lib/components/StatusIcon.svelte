<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import CircleCheck from '@lucide/svelte/icons/circle-check';
	import CircleDashed from '@lucide/svelte/icons/circle-dashed';
	import CircleSlash from '@lucide/svelte/icons/circle-slash';
	import CircleX from '@lucide/svelte/icons/circle-x';
	import Info from '@lucide/svelte/icons/info';
	import LoaderCircle from '@lucide/svelte/icons/loader-circle';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';

	export type StatusKind = 'ok' | 'warn' | 'danger' | 'info' | 'running' | 'idle' | 'disabled';

	interface Props {
		status: StatusKind;
		label: string;
		showLabel?: boolean;
		size?: number;
		class?: string;
	}

	const { status, label, showLabel = false, size = 18, class: className = '' }: Props = $props();

	const STATUS_CONFIG = {
		ok: {
			component: CircleCheck,
			colorClass: 'text-success',
			glyph: 'circle-check'
		},
		warn: {
			component: TriangleAlert,
			colorClass: 'text-warning',
			glyph: 'triangle-alert'
		},
		danger: {
			component: CircleX,
			colorClass: 'text-danger',
			glyph: 'circle-x'
		},
		info: {
			component: Info,
			colorClass: 'text-info',
			glyph: 'info'
		},
		running: {
			component: LoaderCircle,
			colorClass: 'text-running',
			glyph: 'loader-circle'
		},
		idle: {
			component: CircleDashed,
			colorClass: 'text-subtle',
			glyph: 'circle-dashed'
		},
		disabled: {
			component: CircleSlash,
			colorClass: 'text-subtle',
			glyph: 'circle-slash'
		}
	} as const;

	const config = $derived(STATUS_CONFIG[status]);
	const IconComponent = $derived(config.component);
	const isRunning = $derived(status === 'running');
</script>

{#if showLabel}
	<span
		class="inline-flex items-center gap-1.5 {config.colorClass} {className}"
		data-status={status}
		data-glyph={config.glyph}
	>
		<IconComponent
			{size}
			aria-hidden="true"
			class="shrink-0 {isRunning ? 'status-icon-running' : ''}"
		/>
		<span>{label}</span>
	</span>
{:else}
	<span
		role="img"
		aria-label={label}
		class="inline-flex items-center {config.colorClass} {className}"
		data-status={status}
		data-glyph={config.glyph}
	>
		<IconComponent
			{size}
			aria-hidden="true"
			class="shrink-0 {isRunning ? 'status-icon-running' : ''}"
		/>
		<span class="sr-only">{label}</span>
	</span>
{/if}

<style>
	@media (prefers-reduced-motion: no-preference) {
		:global(.status-icon-running) {
			animation: status-spin 1.5s linear infinite;
		}
	}

	@keyframes status-spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
