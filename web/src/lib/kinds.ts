// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from './api/schema.js';
import { m } from './paraglide/messages.js';

export type AppKind = components['schemas']['AppKind'];
export type ArrKind = Exclude<AppKind, 'stash'>;

interface KindSpec {
	readonly legacy: boolean;
	readonly name: string;
}

// Every instance kind of the API, in the order the form offers them, its display name, and
// whether its upstream project is retired (ADR-0008). A kind added to the OpenAPI enumeration
// fails the type check here until it is listed.
const kinds = {
	sonarr: { legacy: false, name: 'Sonarr' },
	radarr: { legacy: false, name: 'Radarr' },
	lidarr: { legacy: false, name: 'Lidarr' },
	readarr: { legacy: true, name: 'Readarr' },
	whisparr_v2: { legacy: false, name: 'Whisparr v2' },
	whisparr_v3: { legacy: false, name: 'Whisparr v3' },
	stash: { legacy: false, name: 'Stash' }
} as const satisfies Record<AppKind, KindSpec>;

export const APP_KINDS = Object.keys(kinds) as AppKind[];
export const ARR_KINDS = APP_KINDS.filter((k): k is ArrKind => k !== 'stash');

/** Reports whether a kind is one of the six *arr kinds. */
export function isArrKind(kind: string): kind is ArrKind {
	return kind !== 'stash' && Object.hasOwn(kinds, kind);
}

/** Reports whether a kind is kept as a frozen legacy target. An unknown kind is not. */
export function isLegacyKind(kind: string): boolean {
	return Object.hasOwn(kinds, kind) && kinds[kind as AppKind].legacy;
}

/** The display name of the kind without legacy qualification. Unknown kinds are returned as given. */
export function kindName(kind: string): string {
	if (kind === 'stash') return m.instance_kind_stash();
	return Object.hasOwn(kinds, kind) ? kinds[kind as AppKind].name : kind;
}

/** The kind as the UI shows it: a legacy kind carries the legacy label, Stash carries its localized name. */
export function kindLabel(kind: string): string {
	if (kind === 'stash') return m.instance_kind_stash();
	const name = kindName(kind);
	return isLegacyKind(kind) ? m.instance_kind_legacy({ kind: name }) : name;
}

export type DownloadClientKind = components['schemas']['DownloadClientKind'];
export type ChannelKind = components['schemas']['NotificationChannelKind'];

// Product names, the same in every locale, in the order the forms offer them.
const downloadClientNames = {
	qbittorrent: 'qBittorrent',
	transmission: 'Transmission',
	deluge: 'Deluge',
	rtorrent: 'rTorrent',
	sabnzbd: 'SABnzbd',
	nzbget: 'NZBGet'
} as const satisfies Record<DownloadClientKind, string>;

const channelNames = {
	webhook: 'Webhook',
	discord: 'Discord',
	slack: 'Slack',
	telegram: 'Telegram',
	ntfy: 'ntfy',
	gotify: 'Gotify',
	teams: 'Microsoft Teams',
	apprise: 'Apprise'
} as const satisfies Record<ChannelKind, string>;

export const DOWNLOAD_CLIENT_KINDS = Object.keys(downloadClientNames) as DownloadClientKind[];
export const CHANNEL_KINDS = Object.keys(channelNames) as ChannelKind[];

/** The product name of a download client kind. Unknown kinds are returned as given. */
export function downloadClientName(kind: string): string {
	return Object.hasOwn(downloadClientNames, kind)
		? downloadClientNames[kind as DownloadClientKind]
		: kind;
}

/** The product name of a notification channel kind. Unknown kinds are returned as given. */
export function channelName(kind: string): string {
	return Object.hasOwn(channelNames, kind) ? channelNames[kind as ChannelKind] : kind;
}
