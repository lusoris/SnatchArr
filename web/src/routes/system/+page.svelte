<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import Tabs, { type Tab } from '#lib/components/Tabs.svelte';
	import { pageHref } from '#lib/entities.js';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import AboutTab from './AboutTab.svelte';
	import EnvironmentTab from './EnvironmentTab.svelte';
	import GeneralTab from './GeneralTab.svelte';
	import ModulesTab from './ModulesTab.svelte';
	import NotificationsTab from './NotificationsTab.svelte';

	type SystemTabKey = 'general' | 'environment' | 'modules' | 'notifications' | 'about';

	const validTabs: readonly SystemTabKey[] = [
		'general',
		'environment',
		'modules',
		'notifications',
		'about'
	];

	const tabs: Tab<SystemTabKey>[] = [
		{ id: 'general', label: m.system_tab_general() },
		{ id: 'environment', label: m.system_tab_environment() },
		{ id: 'modules', label: m.system_tab_modules() },
		{ id: 'notifications', label: m.system_tab_notifications() },
		// Accounts tab goes here once ADR-0026 user management endpoints land
		{ id: 'about', label: m.system_tab_about() }
	];

	const rawTab = $derived(page.url.searchParams.get('tab'));
	const activeTab = $derived<SystemTabKey>(
		validTabs.includes(rawTab as SystemTabKey) ? (rawTab as SystemTabKey) : 'general'
	);

	type SystemHelpKey = '/system' | `/system?tab=${Exclude<SystemTabKey, 'general'>}`;

	const currentHelp = $derived.by(() => {
		const key: SystemHelpKey = activeTab === 'general' ? '/system' : `/system?tab=${activeTab}`;
		return HELP_MAP[key];
	});

	function handleTabChange(newTab: SystemTabKey): void {
		const newUrl = new URL(page.url.href);
		newUrl.searchParams.set('tab', newTab);
		void goto(pageHref('/system', newUrl.search));
	}
</script>

<PageHeader title={m.nav_system()} documentTitle help={currentHelp} />

<Tabs {tabs} active={activeTab} onchange={handleTabChange} label={m.nav_system()}>
	{#snippet children(current)}
		{#if current === 'general'}
			<GeneralTab />
		{:else if current === 'environment'}
			<EnvironmentTab />
		{:else if current === 'modules'}
			<ModulesTab />
		{:else if current === 'notifications'}
			<NotificationsTab />
		{:else if current === 'about'}
			<AboutTab />
		{/if}
	{/snippet}
</Tabs>
