<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import type { ResolvedPathname } from '$app/types';
	import { page } from '$app/state';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Tabs, { type Tab } from '#lib/components/Tabs.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import AboutTab from './AboutTab.svelte';
	import GeneralTab from './GeneralTab.svelte';

	type SystemTabKey = 'general' | 'about';

	const tabs: Tab<SystemTabKey>[] = [
		{ id: 'general', label: m.system_tab_general() },
		{ id: 'about', label: m.system_tab_about() }
	];

	const rawTab = $derived(page.url.searchParams.get('tab'));
	const activeTab = $derived<SystemTabKey>(rawTab === 'about' ? 'about' : 'general');

	function handleTabChange(newTab: SystemTabKey): void {
		const newUrl = new URL(page.url.href);
		newUrl.searchParams.set('tab', newTab);
		const target = `${resolve('/system')}${newUrl.search}` as ResolvedPathname;
		void goto(target, { replaceState: true });
	}
</script>

<PageHeader title={m.nav_system()} documentTitle />

<Tabs {tabs} active={activeTab} onchange={handleTabChange} label={m.nav_system()}>
	{#snippet children(current)}
		{#if current === 'general'}
			<GeneralTab />
		{:else if current === 'about'}
			<AboutTab />
		{/if}
	{/snippet}
</Tabs>
