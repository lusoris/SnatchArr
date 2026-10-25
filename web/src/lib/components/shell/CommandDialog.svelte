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
	import { getNavItems, type NavItem } from './nav.js';

	interface Props {
		navItems?: NavItem[];
		open?: boolean;
	}

	let { navItems = getNavItems(), open = $bindable(false) }: Props = $props();

	let openerEl: HTMLElement | null = null;
	let prevOpen = false;

	const instances = createSentioQuery({
		queryKey: keys.instances,
		queryFn: async () => (await api.GET('/instances')).data ?? []
	});

	const connectionPages = [
		{ href: '/connections/download-clients' as const, label: m.nav_download_clients() },
		{ href: '/connections/seerr' as const, label: m.nav_seerr() },
		{ href: '/connections/prowlarr' as const, label: m.nav_prowlarr() },
		{ href: '/connections/configarr' as const, label: m.nav_configarr() }
	];

	const registry = $derived.by(() => {
		const cmds: Command[] = [];

		for (const item of navItems) {
			cmds.push({
				id: `nav-${item.href}`,
				title: item.label,
				subtitle: item.label,
				group: m.nav_overview(),
				run: async () => {
					await goto(resolve(item.href));
				}
			});
		}

		for (const page of connectionPages) {
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
