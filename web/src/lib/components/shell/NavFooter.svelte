<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { docsUrl } from '#lib/docs.js';
	import { m } from '#lib/paraglide/messages.js';
	import { isRouteActive } from './nav.js';
	import NavIcon from './NavIcon.svelte';
	import LocaleSelect from './LocaleSelect.svelte';
	import VersionLabel from './VersionLabel.svelte';

	interface Props {
		localeId: string;
		onLogout: () => void;
		onNavigate?: () => void;
	}

	const { localeId, onLogout, onNavigate }: Props = $props();

	function handleLogout(): void {
		onNavigate?.();
		onLogout();
	}
</script>

<div class="flex flex-col gap-3 border-t border-line p-3 text-xs">
	<LocaleSelect id={localeId} />

	<a
		href={docsUrl('')}
		target="_blank"
		rel="external noopener"
		class="nav-link flex items-center justify-between rounded px-2 py-1.5 text-sm text-fg"
	>
		<span class="flex items-center gap-2">
			<NavIcon name="book-open" size={16} class="text-subtle" />
			<span>{m.nav_documentation()}</span>
		</span>
		<NavIcon name="external-link" size={14} class="text-subtle" />
	</a>

	<div class="flex items-center justify-between gap-2 pt-1">
		<a
			href={resolve('/account')}
			aria-current={isRouteActive(page.url.pathname, '/account') ? 'page' : undefined}
			onclick={onNavigate}
			class="nav-link flex items-center gap-2 rounded px-2 py-1.5 text-sm text-fg"
		>
			<NavIcon name="user" size={16} class="text-subtle" />
			<span>{m.nav_account()}</span>
		</a>
		<button
			type="button"
			class="btn inline-flex min-h-11 items-center gap-1.5 rounded px-2 py-1.5 text-sm text-subtle hover:border-danger hover:text-danger md:min-h-0"
			onclick={handleLogout}
		>
			<NavIcon name="log-out" size={15} />
			<span>{m.nav_logout()}</span>
		</button>
	</div>

	<VersionLabel />
</div>
