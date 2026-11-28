// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { z } from 'zod';
import type { components } from './api/schema.js';

export type LibraryProcessingRule = components['schemas']['LibraryProcessingRule'];
export type LibraryProcessingRulePage = components['schemas']['LibraryProcessingRulePage'];
export type LibraryProcessingRuleInput = components['schemas']['LibraryProcessingRuleInput'];
export type LibraryProcessingScope = components['schemas']['LibraryProcessingScope'];
export type LibraryProcessingMediaType = components['schemas']['LibraryProcessingMediaType'];

/** Default bounded page size requested by the system UI for stored rules. */
export const LIBRARY_PROCESSING_RULE_PAGE_LIMIT = 50;

/** Precedence scopes, exactly as the API enum declares them. */
export const LIBRARY_PROCESSING_SCOPES = [
	'item_override',
	'user_language',
	'library',
	'instance',
	'media_type',
	'global_default'
] as const satisfies readonly LibraryProcessingScope[];

/** Media type targets, exactly as the API enum declares them. */
export const LIBRARY_PROCESSING_MEDIA_TYPES = [
	'movie',
	'episode',
	'anime',
	'music',
	'audiobook',
	'stash_scene'
] as const satisfies readonly LibraryProcessingMediaType[];

const TARGET_KEYS = ['instance_id', 'item_id', 'user_language', 'root_path', 'media_type'] as const;
type TargetKey = (typeof TARGET_KEYS)[number];

/** Target fields each scope requires; every other target field is forbidden for it. */
const SCOPE_TARGETS: Record<LibraryProcessingScope, readonly TargetKey[]> = {
	item_override: ['instance_id', 'item_id'],
	user_language: ['user_language'],
	library: ['instance_id', 'root_path'],
	instance: ['instance_id'],
	media_type: ['media_type'],
	global_default: []
};

/** Mirrors MaxRuleLanguages in api/internal/libraryprocessing/types.go (HISS-02). */
const MAX_RULE_LANGUAGES = 64;
const MAX_ITEM_ID_CHARS = 128;
const MAX_ROOT_PATH_BYTES = 4096;
const NIL_UUID = '00000000-0000-0000-0000-000000000000';
const EDGE_WHITESPACE = /^\p{White_Space}|\p{White_Space}$/u;
const LANGUAGE_SHAPE = /^[a-z]{2,3}$/;
/** ISO 639-2 reserved local-use range qaa..qtz. */
const LOCAL_USE_LANGUAGE = /^q[a-t][a-z]$/;
const DOT_SPACE_ONLY = /^[. ]+$/;

/**
 * Shape-only ISO 639 check. Canonical aliases (iw, ger, chi, ...) and 639-3 codes are
 * valid and are never resolved here; no language registry is embedded, so whether a
 * well-shaped code is actually assigned stays the server's decision.
 */
function isLanguageCode(code: string): boolean {
	return LANGUAGE_SHAPE.test(code) && code !== 'und' && !LOCAL_USE_LANGUAGE.test(code);
}

const languageCodeSchema = z.string().refine(isLanguageCode, {
	message: 'must be a lowercase ISO 639 code of 2 or 3 letters (not und, not local-use qaa-qtz)'
});

function hasControlCharacter(value: string): boolean {
	for (const ch of value) {
		const code = ch.codePointAt(0) ?? 0;
		if (code < 0x20 || code === 0x7f) return true;
	}
	return false;
}

function hasDotSpaceSegment(path: string): boolean {
	return path.split('/').some((seg) => seg.includes(' ') && DOT_SPACE_ONLY.test(seg));
}

function isCleanPosixAbsolute(path: string): boolean {
	if (path.includes('\0') || !path.startsWith('/')) return false;
	if (path === '/') return true;
	return path
		.slice(1)
		.split('/')
		.every((seg) => seg !== '' && seg !== '.' && seg !== '..');
}

function utf8Length(value: string): number {
	return new TextEncoder().encode(value).length;
}

const scopeSchema = z.enum(LIBRARY_PROCESSING_SCOPES);
const mediaTypeSchema = z.enum(LIBRARY_PROCESSING_MEDIA_TYPES);

const instanceIdSchema = z
	.guid()
	.refine((id) => id !== NIL_UUID, { message: 'instance_id must not be the nil UUID' });

const itemIdSchema = z
	.string()
	.refine(
		(id) => {
			const chars = [...id].length;
			return chars >= 1 && chars <= MAX_ITEM_ID_CHARS;
		},
		{ message: `item_id must be 1..${MAX_ITEM_ID_CHARS} characters` }
	)
	.refine((id) => !EDGE_WHITESPACE.test(id), {
		message: 'item_id must not have leading or trailing whitespace'
	})
	.refine((id) => !hasControlCharacter(id), {
		message: 'item_id must not contain control characters'
	});

const rootPathSchema = z
	.string()
	.refine((p) => utf8Length(p) >= 1 && utf8Length(p) <= MAX_ROOT_PATH_BYTES, {
		message: `root_path must be 1..${MAX_ROOT_PATH_BYTES} bytes`
	})
	.refine((p) => isCleanPosixAbsolute(p) && !hasDotSpaceSegment(p), {
		message: 'root_path must be a clean POSIX absolute path'
	});

function rejectDuplicates(list: string[], ctx: z.core.$RefinementCtx<string[]>): void {
	const seen = new Set<string>();
	list.forEach((code, index) => {
		if (seen.has(code)) {
			ctx.addIssue({ code: 'custom', path: [index], message: `duplicate language ${code}` });
		}
		seen.add(code);
	});
}

const trackRuleSchema = z.strictObject({
	languages: z
		.array(languageCodeSchema)
		.min(1)
		.max(MAX_RULE_LANGUAGES)
		.superRefine(rejectDuplicates)
});

const ruleSchema = z.strictObject({ track: trackRuleSchema });

const inputShapeSchema = z.strictObject({
	scope: scopeSchema,
	instance_id: instanceIdSchema.optional(),
	item_id: itemIdSchema.optional(),
	user_language: languageCodeSchema.optional(),
	root_path: rootPathSchema.optional(),
	media_type: mediaTypeSchema.optional(),
	rule: ruleSchema
});

type ScopeInput = z.output<typeof inputShapeSchema>;

/** Absent is the only way to omit a target: null and explicit undefined are both invalid. */
function targetProblem(input: ScopeInput, key: TargetKey, required: boolean): string | undefined {
	if (!(key in input)) return required ? `${key} is required for scope ${input.scope}` : undefined;
	if (input[key] === undefined) return `${key} must be omitted, not set to undefined`;
	return required ? undefined : `${key} is forbidden for scope ${input.scope}`;
}

function checkScopeTargets(input: ScopeInput, ctx: z.core.$RefinementCtx<ScopeInput>): void {
	const required = SCOPE_TARGETS[input.scope];
	for (const key of TARGET_KEYS) {
		const message = targetProblem(input, key, required.includes(key));
		if (message) ctx.addIssue({ code: 'custom', path: [key], message });
	}
}

/**
 * Create/replace payload for one processing rule. Operates on already-decoded data,
 * so JSON and YAML transports share it; no parsing happens here.
 */
export const libraryProcessingRuleInputSchema = inputShapeSchema.superRefine(
	checkScopeTargets
) satisfies z.ZodType<LibraryProcessingRuleInput>;

/** Validates decoded request data; throws a ZodError. Values are never trimmed or case-folded. */
export function parseRuleInput(value: unknown): LibraryProcessingRuleInput {
	return libraryProcessingRuleInputSchema.parse(value);
}

/** Strips only the server-managed fields (id, created_at, updated_at); nothing else changes. */
export function toRuleInput(stored: LibraryProcessingRule): LibraryProcessingRuleInput {
	// Explicit projection onto the input DTO fields, so server-managed fields are never bound.
	// Optional keys are copied only when present, which keeps absent keys absent.
	const { scope, instance_id, item_id, user_language, root_path, media_type, rule } = stored;
	// Bounded copy of rule/track/languages: structuredClone throws on $state proxies, and the
	// existing contract ("does not validate or normalize") rules out re-parsing stored data.
	// Spreading yields plain objects/arrays without rewriting any value.
	const { track } = rule;
	return {
		scope,
		...(instance_id !== undefined && { instance_id }),
		...(item_id !== undefined && { item_id }),
		...(user_language !== undefined && { user_language }),
		...(root_path !== undefined && { root_path }),
		...(media_type !== undefined && { media_type }),
		rule: { ...rule, track: { ...track, languages: [...track.languages] } }
	};
}

const SCOPE_RANK: Record<LibraryProcessingScope, number> = {
	item_override: 1,
	user_language: 2,
	library: 3,
	instance: 4,
	media_type: 5,
	global_default: 6
};

const UTC_TIMESTAMP_PARTS = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z$/;

function canonicalCreatedKey(createdAt: string): string {
	const match = UTC_TIMESTAMP_PARTS.exec(createdAt);
	if (!match) return createdAt;
	const [, seconds, fraction = ''] = match;
	return `${seconds}.${fraction.padEnd(9, '0')}Z`;
}

/** Orders stored rules by scope precedence, created_at ascending, then id ascending. */
export function compareStoredRules(a: LibraryProcessingRule, b: LibraryProcessingRule): number {
	const rankDiff = SCOPE_RANK[a.scope] - SCOPE_RANK[b.scope];
	if (rankDiff !== 0) return rankDiff;
	const createdA = canonicalCreatedKey(a.created_at);
	const createdB = canonicalCreatedKey(b.created_at);
	if (createdA !== createdB) return createdA < createdB ? -1 : 1;
	if (a.id === b.id) return 0;
	return a.id < b.id ? -1 : 1;
}

/** Appends incoming page items without duplicating already-loaded rule IDs. */
export function appendRulePage(
	existing: readonly LibraryProcessingRule[],
	incoming: readonly LibraryProcessingRule[]
): LibraryProcessingRule[] {
	const seen = new Set(existing.map((rule) => rule.id));
	const merged = [...existing];
	for (const rule of incoming) {
		if (!seen.has(rule.id)) {
			seen.add(rule.id);
			merged.push(rule);
		}
	}
	return merged;
}

/** Inserts an off-page selected rule at its precedence position until its page is loaded. */
export function mergePinnedRule(
	loaded: readonly LibraryProcessingRule[],
	pinned: LibraryProcessingRule | null
): LibraryProcessingRule[] {
	if (!pinned || loaded.some((rule) => rule.id === pinned.id)) {
		return [...loaded];
	}
	const out = [...loaded];
	const insertAt = out.findIndex((rule) => compareStoredRules(pinned, rule) < 0);
	if (insertAt === -1) {
		out.push(pinned);
	} else {
		out.splice(insertAt, 0, pinned);
	}
	return out;
}
