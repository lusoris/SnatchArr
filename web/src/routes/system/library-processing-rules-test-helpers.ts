// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { fireEvent, screen } from '@testing-library/svelte';
import { vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';

export type StoredRule = components['schemas']['LibraryProcessingRule'];
type RulePage = components['schemas']['LibraryProcessingRulePage'];

export const RULES_PATH = '/library-processing/rules';
export const RULES_PAGE_PATH = '/library-processing/rules/page';
export const RULE_DETAIL_PATH = '/library-processing/rules/{ruleId}';
const INSTANCE_ID = '018f0000-0000-7000-8000-000000000099';

// The writable part of a rule: what an operator pastes and what the API receives.
export const ruleInput = {
	scope: 'global_default',
	rule: { track: { languages: ['en', 'de'] } }
};

export const jsonDocument = JSON.stringify(ruleInput, null, 2);
export const yamlDocument = [
	'scope: global_default',
	'rule:',
	'  track:',
	'    languages:',
	'      - en',
	'      - de',
	''
].join('\n');

export const storedRule = {
	...ruleInput,
	id: '018f0000-0000-7000-8000-000000000001',
	created_at: '2026-10-25T00:00:00Z',
	updated_at: '2026-10-25T00:00:00Z'
} as unknown as StoredRule;

export const highPrecedenceRule = {
	scope: 'item_override',
	instance_id: INSTANCE_ID,
	item_id: 'ep-101',
	rule: { track: { languages: ['ja', 'en'] } },
	id: '018f0000-0000-7000-8000-000000000002',
	created_at: '2026-10-25T00:00:00Z',
	updated_at: '2026-10-25T00:00:00Z'
} as unknown as StoredRule;

export const secondPageRule = {
	scope: 'media_type',
	media_type: 'anime',
	rule: { track: { languages: ['ja'] } },
	id: '018f0000-0000-7000-8000-000000000003',
	created_at: '2026-10-25T00:01:00Z',
	updated_at: '2026-10-25T00:01:00Z'
} as unknown as StoredRule;

export const optionLabel = `${storedRule.scope} · ${storedRule.id}`;

export function pageResult(items: StoredRule[], nextCursor?: string): { data: RulePage } {
	if (nextCursor) {
		return { data: { items, has_more: true, next_cursor: nextCursor } };
	}
	return { data: { items, has_more: false } };
}

export function documentField(): HTMLTextAreaElement {
	return screen.getByLabelText(m.modules_rules_document_label()) as HTMLTextAreaElement;
}

export async function typeDocument(value: string): Promise<void> {
	await fireEvent.input(documentField(), { target: { value } });
}

export async function chooseFormat(label: 'YAML' | 'JSON'): Promise<void> {
	await fireEvent.click(screen.getByRole('radio', { name: label }));
}

export const RULE_PATH = '/library-processing/rules/{ruleId}';

export const otherRule = {
	...storedRule,
	id: 'rule-2',
	rule: { track: { languages: ['it'] } }
} as unknown as StoredRule;

export const editedInput = {
	scope: 'global_default',
	rule: { track: { languages: ['fr'] } }
};
export const editedDocument = JSON.stringify(editedInput, null, 2);

export function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason?: unknown) => void;
	const promise = new Promise<T>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

export const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

// A request that never settles on its own and only ends when its signal aborts.
export function hangUntilAborted(_path: string, init?: { signal?: AbortSignal }): Promise<never> {
	return new Promise((_resolve, reject) => {
		const signal = init?.signal;
		if (!signal) return;
		if (signal.aborted) {
			reject(signal.reason);
			return;
		}
		signal.addEventListener('abort', () => reject(signal.reason), { once: true });
	});
}

export function saveButton(): HTMLElement {
	return screen.getByRole('button', { name: m.modules_rules_save() });
}

export async function openRuleAsJson(id: string): Promise<void> {
	const select = await screen.findByLabelText(m.modules_rules_select_label());
	await fireEvent.change(select, { target: { value: id } });
	await chooseFormat('JSON');
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
}

export const REQUEST_TIMEOUT_MS = 15_000;

// Each AbortSignal.timeout() call gets its own controller, so a retry never inherits a spent deadline.
export function mockDeadlines() {
	const deadlines: AbortController[] = [];
	const timeoutSpy = vi.spyOn(AbortSignal, 'timeout').mockImplementation(() => {
		const controller = new AbortController();
		deadlines.push(controller);
		return controller.signal;
	});
	return { deadlines, timeoutSpy };
}

export function expireDeadline(controller: AbortController): void {
	controller.abort(new DOMException('The operation timed out.', 'TimeoutError'));
}

// A GET that behaves like fetch: an already-aborted signal rejects instead of answering.
export function pageHonoringSignal(items: StoredRule[]) {
	return (_path: string, init?: { signal?: AbortSignal }): Promise<unknown> => {
		if (init?.signal?.aborted) return Promise.reject(init.signal.reason);
		return Promise.resolve(pageResult(items));
	};
}

export function importButton(): HTMLElement {
	return screen.getByRole('button', { name: m.modules_rules_import() });
}

export function problemError(status: number, title: string, detail: string): ProblemError {
	return new ProblemError({ type: 'about:blank', title, status, detail });
}

export function ruleSelect(): HTMLSelectElement {
	return screen.getByLabelText(m.modules_rules_select_label()) as HTMLSelectElement;
}

export function selectOptionValues(select: HTMLSelectElement = ruleSelect()): string[] {
	return Array.from(select.options).map((opt) => opt.value);
}

export function itemOverrideFixture(itemId: string): StoredRule {
	return {
		scope: 'item_override',
		instance_id: INSTANCE_ID,
		item_id: itemId,
		rule: { track: { languages: ['en'] } },
		id: storedRule.id,
		created_at: storedRule.created_at,
		updated_at: storedRule.updated_at
	} as unknown as StoredRule;
}

export function savedGlobalFixture(updatedAt: string): StoredRule {
	return {
		...editedInput,
		id: storedRule.id,
		created_at: storedRule.created_at,
		updated_at: updatedAt
	} as unknown as StoredRule;
}

export function userLanguageFixture(id: string, lang: string, createdAt: string): StoredRule {
	return {
		scope: 'user_language',
		user_language: lang,
		rule: { track: { languages: [lang] } },
		id,
		created_at: createdAt,
		updated_at: createdAt
	} as unknown as StoredRule;
}

export function mockScopeChangeDetailFallback(initial: StoredRule) {
	return vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult([initial, highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'page refresh down'))
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockRejectedValueOnce(problemError(503, 'Service Unavailable', 'detail lookup unavailable'));
}

export function mockPinnedAndOffPageSaveFailure(pinnedOther: StoredRule) {
	return vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([pinnedOther, storedRule], 'cursor-3') as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'initial save refresh down'))
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce({ data: pinnedOther } as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'refresh failed with other pinned'));
}

export function mockSelectionChangePages(initial: StoredRule, updated: StoredRule) {
	return vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult([initial, highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([secondPageRule], 'cursor-3') as never)
		.mockResolvedValueOnce(pageResult([updated]) as never);
}

export function mockMovedBehindStaleCursorPages(trailingGlobal: StoredRule) {
	const firstPage = [highPrecedenceRule, secondPageRule];
	return vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult(firstPage, 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([storedRule], 'cursor-3') as never)
		.mockResolvedValueOnce(pageResult(firstPage, 'cursor-2') as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'refresh down after scope move'))
		.mockResolvedValueOnce(pageResult([trailingGlobal]) as never)
		.mockResolvedValueOnce(pageResult([...firstPage, trailingGlobal]) as never);
}
