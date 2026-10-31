<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import AppLogo from '#lib/components/AppLogo.svelte';
	import NavIcon from '#lib/components/shell/NavIcon.svelte';
	import ProblemNote from './ProblemNote.svelte';
	import StatusIcon from './StatusIcon.svelte';
	import type { ServiceStatusSummary } from '#lib/connections.js';
	import { pageHref } from '#lib/entities.js';
	import { m } from '#lib/paraglide/messages.js';

	type KnownRoute =
		| '/connections/download-clients'
		| '/connections/seerr'
		| '/connections/prowlarr'
		| '/connections/configarr';

	interface Props {
		icon: string;
		title: string;
		href: KnownRoute;
		description: string;
		pending: boolean;
		error: unknown;
		summary: ServiceStatusSummary;
		showAdd: boolean;
		kind?: string;
	}

	const { icon, title, href, description, pending, error, summary, showAdd, kind }: Props =
		$props();
</script>

{#if pending}
	<div
		data-testid="skeleton"
		class="card flex min-h-[14rem] animate-pulse flex-col justify-between gap-4 p-5"
		aria-busy="true"
	>
		<div class="flex flex-col gap-3">
			<div class="flex items-center gap-3">
				<div class="h-10 w-10 rounded-lg bg-[var(--color-secondary)]"></div>
				<div class="h-5 w-32 rounded bg-[var(--color-line)]"></div>
			</div>
			<div class="h-4 w-full rounded bg-[var(--color-line)]"></div>
		</div>
		<div class="h-6 w-24 rounded bg-[var(--color-line)]"></div>
	</div>
{:else if error}
	<div class="card flex min-h-[14rem] flex-col justify-between gap-4 p-5">
		<div class="flex items-center gap-3">
			<span
				class="text-brand flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-[var(--color-secondary)]"
			>
				{#if kind}
					<AppLogo {kind} size={22} />
				{:else}
					<NavIcon name={icon} size={22} />
				{/if}
			</span>
			<h3 class="text-base font-semibold text-fg">
				<a href={pageHref(href)} class="hover:underline">{title}</a>
			</h3>
		</div>
		<ProblemNote {error} />
	</div>
{:else}
	<div
		class="card flex min-h-[14rem] flex-col justify-between gap-4 p-5 transition-colors hover:border-control hover:shadow-xs"
	>
		<div class="flex flex-col gap-3">
			<div class="flex items-center gap-3">
				<span
					class="text-brand flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-[var(--color-secondary)]"
				>
					{#if kind}
						<AppLogo {kind} size={22} />
					{:else}
						<NavIcon name={icon} size={22} />
					{/if}
				</span>
				<h3 class="text-base font-semibold text-fg">
					<a href={pageHref(href)} class="hover:underline">{title}</a>
				</h3>
			</div>
			<p class="text-sm text-subtle">{description}</p>
		</div>
		<div class="flex flex-col gap-2 border-t border-line/40 pt-3">
			<div class="flex items-center gap-2">
				<StatusIcon status={summary.status} label={summary.label} showLabel />
			</div>
			{#if summary.countLine}
				<p class="text-xs text-subtle">{summary.countLine}</p>
			{/if}
			{#if showAdd}
				<div class="mt-2">
					<a href={pageHref(href)} class="btn w-full justify-center">
						{m.connections_add_link()}
					</a>
				</div>
			{/if}
		</div>
	</div>
{/if}
