// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { resolve } from '$app/paths';
import type { ResolvedPathname, RouteId } from '$app/types';
import type { components } from '#lib/api/schema.js';

export type PageRoute = RouteId extends `${string}[${string}` ? never : RouteId;

export type InstanceTab = 'policy' | 'queue' | 'activity' | 'memory';

export interface EntityTypeDef<TTab extends string = never> {
	readonly route?: string;
	readonly tabs?: readonly TTab[];
}

export type RoutableEntityType<TTypes extends Record<string, EntityTypeDef<string>>> = {
	[K in Extract<keyof TTypes, string>]: TTypes[K] extends { readonly route: string } ? K : never;
}[Extract<keyof TTypes, string>];

export interface EntityRelation<
	TFrom extends string = string,
	TTo extends string = string,
	TKey extends string = string
> {
	readonly from: TFrom;
	readonly to: TTo;
	readonly key: TKey;
}

export interface RelatedEntityLink<TType extends string = string> {
	readonly type: TType;
	readonly id: string;
	readonly href: ResolvedPathname;
}

export interface EntityGraph<
	TTypes extends Record<string, EntityTypeDef<string>> = Record<string, EntityTypeDef<string>>,
	TPageRoute extends string = PageRoute
> {
	readonly types: TTypes;
	readonly relations: readonly EntityRelation<
		Extract<keyof TTypes, string>,
		Extract<keyof TTypes, string>
	>[];
	pageHref(route: TPageRoute, search?: string): ResolvedPathname;
	entityHref<K extends RoutableEntityType<TTypes>>(
		type: K,
		id: string,
		tab?: TTypes[K] extends EntityTypeDef<infer TTab> ? TTab : never,
		search?: string
	): ResolvedPathname;
	entityHref<K extends Extract<keyof TTypes, string>>(
		type: K,
		id: string,
		tab?: string,
		search?: string
	): ResolvedPathname | undefined;
	entityHref(type: string, id: string, tab?: string, search?: string): ResolvedPathname | undefined;
	entityHrefOrNull(
		type: string,
		id: string,
		tab?: string,
		search?: string
	): ResolvedPathname | null;
	related<K extends Extract<keyof TTypes, string>>(
		type: K,
		row: object | null | undefined
	): RelatedEntityLink<Extract<keyof TTypes, string>>[];
}

function appendSearch(path: string, search?: string): ResolvedPathname {
	if (!search) return path as ResolvedPathname;
	const suffix = search.startsWith('?') ? search : `?${search}`;
	return `${path}${suffix}` as ResolvedPathname;
}

function resolveEntityHref<TTypes extends Record<string, EntityTypeDef<string>>>(
	types: TTypes,
	type: string,
	id: string,
	tab?: string,
	search?: string
): ResolvedPathname | undefined {
	const def = types[type];
	if (!def?.route || !id) return undefined;
	let routeTemplate = def.route;
	if (tab !== undefined) {
		if (!def.tabs?.includes(tab)) return undefined;
		routeTemplate = `${def.route}/${tab}`;
	}
	const resolved = String(resolve(routeTemplate as '/instances/[id]', { id }));
	return appendSearch(resolved, search);
}

function resolveRelatedLinks<TTypes extends Record<string, EntityTypeDef<string>>>(
	types: TTypes,
	relations: readonly EntityRelation<
		Extract<keyof TTypes, string>,
		Extract<keyof TTypes, string>
	>[],
	type: Extract<keyof TTypes, string>,
	row: object | null | undefined
): RelatedEntityLink<Extract<keyof TTypes, string>>[] {
	if (!row || typeof row !== 'object') return [];
	const record = row as Record<string, unknown>;
	const out: RelatedEntityLink<Extract<keyof TTypes, string>>[] = [];
	for (const rel of relations) {
		if (rel.from !== type) continue;
		const rawId = record[rel.key];
		if (typeof rawId !== 'string' || rawId.length === 0) continue;
		const href = resolveEntityHref(types, rel.to, rawId);
		if (!href) continue;
		out.push({ type: rel.to, id: rawId, href });
	}
	return out;
}

export function createEntityGraph<
	const TTypes extends Record<string, EntityTypeDef<string>>,
	TPageRoute extends string = PageRoute
>(
	types: TTypes,
	relations: readonly EntityRelation<Extract<keyof TTypes, string>, Extract<keyof TTypes, string>>[]
): EntityGraph<TTypes, TPageRoute> {
	return {
		types,
		relations,
		pageHref(route, search) {
			return appendSearch(String(resolve(route as '/')), search);
		},
		entityHref(type: string, id: string, tab?: string, search?: string) {
			return resolveEntityHref(types, type, id, tab, search) as never;
		},
		entityHrefOrNull(type: string, id: string, tab?: string, search?: string) {
			return resolveEntityHref(types, type, id, tab, search) ?? null;
		},
		related(type, row) {
			return resolveRelatedLinks(types, relations, type, row);
		}
	};
}

export interface EntityRowMap {
	instance: components['schemas']['Instance'];
	event: components['schemas']['Event'];
	run: components['schemas']['Run'];
	schedule: components['schemas']['Schedule'];
	download_client: components['schemas']['DownloadClient'];
	seerr_request: components['schemas']['SeerrRequest'];
}

export type InstanceLinkedRowType = Exclude<keyof EntityRowMap, 'instance'>;

type InstanceIdKey = {
	[K in InstanceLinkedRowType]: 'instance_id' extends keyof EntityRowMap[K] ? 'instance_id' : never;
}[InstanceLinkedRowType];

const INSTANCE_ID_KEY: InstanceIdKey = 'instance_id';

export const INSTANCE_TABS = [
	'policy',
	'queue',
	'activity',
	'memory'
] as const satisfies readonly InstanceTab[];

export const entityTypes = {
	instance: {
		route: '/instances/[id]',
		tabs: INSTANCE_TABS
	},
	event: {},
	run: {},
	schedule: {},
	download_client: {},
	seerr_request: {}
} as const satisfies Record<keyof EntityRowMap, EntityTypeDef<InstanceTab>>;

export type EntityType = keyof typeof entityTypes;

export const entityRelations = [
	{ from: 'event', to: 'instance', key: INSTANCE_ID_KEY },
	{ from: 'run', to: 'instance', key: INSTANCE_ID_KEY },
	{ from: 'schedule', to: 'instance', key: INSTANCE_ID_KEY },
	{ from: 'download_client', to: 'instance', key: INSTANCE_ID_KEY },
	{ from: 'seerr_request', to: 'instance', key: INSTANCE_ID_KEY }
] as const satisfies readonly EntityRelation<EntityType, EntityType, InstanceIdKey>[];

export const entities = createEntityGraph(entityTypes, entityRelations);

export const pageHref = entities.pageHref;

export function entityHref<K extends RoutableEntityType<typeof entityTypes>>(
	type: K,
	id: string,
	tab?: (typeof entityTypes)[K] extends EntityTypeDef<infer TTab> ? TTab : never,
	search?: string
): ResolvedPathname;
export function entityHref<K extends Extract<keyof typeof entityTypes, string>>(
	type: K,
	id: string,
	tab?: string,
	search?: string
): ResolvedPathname | undefined;
export function entityHref(
	type: string,
	id: string,
	tab?: string,
	search?: string
): ResolvedPathname | undefined;
export function entityHref(
	type: string,
	id: string,
	tab?: string,
	search?: string
): ResolvedPathname | undefined {
	return entities.entityHref(type, id, tab, search);
}

export function entityHrefOrNull(
	type: string,
	id: string,
	tab?: string,
	search?: string
): ResolvedPathname | null {
	return entities.entityHrefOrNull(type, id, tab, search);
}

export const related = entities.related;
