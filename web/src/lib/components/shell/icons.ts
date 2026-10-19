// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createLucideLoader } from '@sveltesentio/ui/icons';
import CloudDownload from '@lucide/svelte/icons/cloud-download';
import Clock from '@lucide/svelte/icons/clock';
import FileCode from '@lucide/svelte/icons/file-code';
import Film from '@lucide/svelte/icons/film';
import History from '@lucide/svelte/icons/history';
import LayoutDashboard from '@lucide/svelte/icons/layout-dashboard';
import LogOut from '@lucide/svelte/icons/log-out';
import Menu from '@lucide/svelte/icons/menu';
import Radio from '@lucide/svelte/icons/radio';
import Search from '@lucide/svelte/icons/search';
import Server from '@lucide/svelte/icons/server';
import Settings from '@lucide/svelte/icons/settings';
import Terminal from '@lucide/svelte/icons/terminal';
import User from '@lucide/svelte/icons/user';
import X from '@lucide/svelte/icons/x';

/**
 * Icons the app shell uses, imported per path so the build bundles and
 * tree-shakes them and they resolve on the first frame. lucideDynamicLoader
 * builds a bare runtime specifier that a static build never bundles, so its
 * icons never load in production.
 */
export const shellIconLoader = createLucideLoader({
	CloudDownload,
	Clock,
	FileCode,
	Film,
	History,
	LayoutDashboard,
	LogOut,
	Menu,
	Radio,
	Search,
	Server,
	Settings,
	Terminal,
	User,
	X
});

/** Icon names the shell renders outside the nav groups. */
export const shellChromeIcons = ['menu', 'x', 'user', 'log-out'] as const;
