// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createLucideLoader } from '@sveltesentio/ui/icons';
import Activity from '@lucide/svelte/icons/activity';
import BookOpen from '@lucide/svelte/icons/book-open';
import Clock from '@lucide/svelte/icons/clock';
import CloudDownload from '@lucide/svelte/icons/cloud-download';
import Ellipsis from '@lucide/svelte/icons/ellipsis';
import ExternalLink from '@lucide/svelte/icons/external-link';
import FileCode from '@lucide/svelte/icons/file-code';
import Film from '@lucide/svelte/icons/film';
import Heart from '@lucide/svelte/icons/heart';
import Images from '@lucide/svelte/icons/images';
import History from '@lucide/svelte/icons/history';
import Info from '@lucide/svelte/icons/info';
import LayoutDashboard from '@lucide/svelte/icons/layout-dashboard';
import Link from '@lucide/svelte/icons/link';
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
	Activity,
	BookOpen,
	Clock,
	CloudDownload,
	Ellipsis,
	ExternalLink,
	FileCode,
	Film,
	Heart,
	Images,
	History,
	Info,
	LayoutDashboard,
	Link,
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
export const shellChromeIcons = [
	'menu',
	'x',
	'user',
	'log-out',
	'search',
	'ellipsis',
	'heart',
	'book-open',
	'external-link'
] as const;
