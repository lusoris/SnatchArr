<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createSentioQuery } from '@sveltesentio/query';
	import { CommandRegistry, type Command } from '@sveltesentio/ui/cmd';
	import CommandPalette from '@sveltesentio/ui/cmd/palette';
	import { api } from '#lib/api/client.js';
	import { docsUrl } from '#lib/docs.js';
	import { DOCS_ENTRIES } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';
	import { filterByModule, getNavItems, type ModuledItem, type NavItem } from './nav.js';

	interface ConnectionPageItem extends ModuledItem {
		href:
			| '/connections/download-clients'
			| '/connections/seerr'
			| '/connections/prowlarr'
			| '/connections/configarr';
		label: string;
	}

	interface SettingsPageItem extends ModuledItem {
		href: '/settings/requests';
		label: string;
	}

	interface Props {
		navItems?: NavItem[];
		activeModules?: ReadonlySet<string>;
		open?: boolean;
	}

	let { navItems, activeModules = new Set(), open = $bindable(false) }: Props = $props();

	const effectiveNavItems = $derived(
		filterByModule(navItems ?? getNavItems(activeModules), activeModules)
	);

	let openerEl: HTMLElement | null = null;
	let prevOpen = false;

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	const connectionPages: readonly ConnectionPageItem[] = [
		{ href: '/connections/download-clients', label: m.nav_download_clients() },
		{ href: '/connections/seerr', label: m.nav_seerr() },
		{ href: '/connections/prowlarr', label: m.nav_prowlarr() },
		{ href: '/connections/configarr', label: m.nav_configarr() }
	];

	const visibleConnectionPages = $derived(filterByModule(connectionPages, activeModules));

	const settingsPages: readonly SettingsPageItem[] = [
		{ href: '/settings/requests', label: m.requests_settings_title() }
	];

	const visibleSettingsPages = $derived(filterByModule(settingsPages, activeModules));

	const registry = $derived.by(() => {
		const cmds: Command[] = [];

		for (const item of effectiveNavItems) {
			cmds.push({
				id: `nav-${item.href}`,
				title: item.label,
				subtitle: item.label,
				group: m.nav_overview(),
				keywords: [item.href.replace('/', ''), item.label.toLowerCase()],
				run: async () => {
					await goto(resolve(item.href));
				}
			});
		}

		for (const page of visibleConnectionPages) {
			cmds.push({
				id: `nav-${page.href}`,
				title: page.label,
				subtitle: m.nav_connections(),
				group: m.nav_connections(),
				run: async () => {
					await goto(resolve(page.href));
				}
			});
		}

		for (const page of visibleSettingsPages) {
			cmds.push({
				id: `nav-${page.href}`,
				title: page.label,
				subtitle: m.nav_system(),
				group: m.nav_system(),
				keywords: ['settings', 'requests', 'community', page.label.toLowerCase()],
				run: async () => {
					await goto(resolve(page.href));
				}
			});
		}

		cmds.push({
			id: 'nav-/account',
			title: m.nav_account(),
			subtitle: m.nav_account(),
			group: m.nav_account(),
			run: async () => {
				await goto(resolve('/account'));
			}
		});

		for (const inst of instances.data ?? []) {
			cmds.push({
				id: `instance-${inst.id}`,
				title: inst.name,
				subtitle: inst.kind,
				group: m.nav_instances(),
				keywords: [inst.kind, inst.name],
				run: async () => {
					await goto(resolve('/instances/[id]', { id: inst.id }));
				}
			});
		}

		for (const doc of DOCS_ENTRIES) {
			const title = doc.title();
			cmds.push({
				id: `docs-${doc.docsPath}`,
				title: m.docs_cmd_prefix({ title }),
				subtitle: docsUrl(doc.docsPath),
				group: m.nav_documentation(),
				keywords: ['docs', 'documentation', title],
				run: () => {
					window.open(docsUrl(doc.docsPath), '_blank', 'noopener');
				}
			});
		}

		return new CommandRegistry().register(...cmds);
	});

	$effect.pre(() => {
		if (open && !prevOpen) {
			openerEl = document.activeElement as HTMLElement | null;
		}
		prevOpen = open;
	});

	$effect(() => {
		if (open) {
			const input = document.getElementById('ssentio-cmd-input');
			input?.focus();
		} else if (openerEl) {
			const target = openerEl;
			openerEl = null;
			target.focus();
		}
	});
</script>

<CommandPalette {registry} bind:open placeholder={m.nav_search_placeholder()} />
