// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { toast } from 'svelte-sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { readRuleDocument } from '#lib/library-processing-document.js';
import { LIBRARY_PROCESSING_RULE_PAGE_LIMIT } from '#lib/library-processing-rule.js';
import { m } from '#lib/paraglide/messages.js';
import LibraryProcessingRules from './LibraryProcessingRules.svelte';

type StoredRule = components['schemas']['LibraryProcessingRule'];
type RulePage = components['schemas']['LibraryProcessingRulePage'];

const RULES_PATH = '/library-processing/rules';
const RULES_PAGE_PATH = '/library-processing/rules/page';
const RULE_DETAIL_PATH = '/library-processing/rules/{ruleId}';
const INSTANCE_ID = '018f0000-0000-7000-8000-000000000099';

// The writable part of a rule: what an operator pastes and what the API receives.
const ruleInput = {
	scope: 'global_default',
	rule: { track: { languages: ['en', 'de'] } }
};

const jsonDocument = JSON.stringify(ruleInput, null, 2);
const yamlDocument = [
	'scope: global_default',
	'rule:',
	'  track:',
	'    languages:',
	'      - en',
	'      - de',
	''
].join('\n');

const storedRule = {
	...ruleInput,
	id: '018f0000-0000-7000-8000-000000000001',
	created_at: '2026-10-25T00:00:00Z',
	updated_at: '2026-10-25T00:00:00Z'
} as unknown as StoredRule;

const highPrecedenceRule = {
	scope: 'item_override',
	instance_id: INSTANCE_ID,
	item_id: 'ep-101',
	rule: { track: { languages: ['ja', 'en'] } },
	id: '018f0000-0000-7000-8000-000000000002',
	created_at: '2026-10-25T00:00:00Z',
	updated_at: '2026-10-25T00:00:00Z'
} as unknown as StoredRule;

const secondPageRule = {
	scope: 'media_type',
	media_type: 'anime',
	rule: { track: { languages: ['ja'] } },
	id: '018f0000-0000-7000-8000-000000000003',
	created_at: '2026-10-25T00:01:00Z',
	updated_at: '2026-10-25T00:01:00Z'
} as unknown as StoredRule;

const optionLabel = `${storedRule.scope} · ${storedRule.id}`;

function pageResult(items: StoredRule[], nextCursor?: string): { data: RulePage } {
	if (nextCursor) {
		return { data: { items, has_more: true, next_cursor: nextCursor } };
	}
	return { data: { items, has_more: false } };
}

function documentField(): HTMLTextAreaElement {
	return screen.getByLabelText(m.modules_rules_document_label()) as HTMLTextAreaElement;
}

async function typeDocument(value: string): Promise<void> {
	await fireEvent.input(documentField(), { target: { value } });
}

async function chooseFormat(label: 'YAML' | 'JSON'): Promise<void> {
	await fireEvent.click(screen.getByRole('radio', { name: label }));
}

const RULE_PATH = '/library-processing/rules/{ruleId}';

const otherRule = {
	...storedRule,
	id: 'rule-2',
	rule: { track: { languages: ['it'] } }
} as unknown as StoredRule;

const editedInput = {
	scope: 'global_default',
	rule: { track: { languages: ['fr'] } }
};
const editedDocument = JSON.stringify(editedInput, null, 2);

function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason?: unknown) => void;
	const promise = new Promise<T>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

// A request that never settles on its own and only ends when its signal aborts.
function hangUntilAborted(_path: string, init?: { signal?: AbortSignal }): Promise<never> {
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

function saveButton(): HTMLElement {
	return screen.getByRole('button', { name: m.modules_rules_save() });
}

async function openRuleAsJson(id: string): Promise<void> {
	const select = await screen.findByLabelText(m.modules_rules_select_label());
	await fireEvent.change(select, { target: { value: id } });
	await chooseFormat('JSON');
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
}

const REQUEST_TIMEOUT_MS = 15_000;

// Each AbortSignal.timeout() call gets its own controller, so a retry never inherits a spent deadline.
function mockDeadlines() {
	const deadlines: AbortController[] = [];
	const timeoutSpy = vi.spyOn(AbortSignal, 'timeout').mockImplementation(() => {
		const controller = new AbortController();
		deadlines.push(controller);
		return controller.signal;
	});
	return { deadlines, timeoutSpy };
}

function expireDeadline(controller: AbortController): void {
	controller.abort(new DOMException('The operation timed out.', 'TimeoutError'));
}

// A GET that behaves like fetch: an already-aborted signal rejects instead of answering.
function pageHonoringSignal(items: StoredRule[]) {
	return (_path: string, init?: { signal?: AbortSignal }): Promise<unknown> => {
		if (init?.signal?.aborted) return Promise.reject(init.signal.reason);
		return Promise.resolve(pageResult(items));
	};
}

function importButton(): HTMLElement {
	return screen.getByRole('button', { name: m.modules_rules_import() });
}

function problemError(status: number, title: string, detail: string): ProblemError {
	return new ProblemError({ type: 'about:blank', title, status, detail });
}

function ruleSelect(): HTMLSelectElement {
	return screen.getByLabelText(m.modules_rules_select_label()) as HTMLSelectElement;
}

function selectOptionValues(select: HTMLSelectElement = ruleSelect()): string[] {
	return Array.from(select.options).map((opt) => opt.value);
}

function itemOverrideFixture(itemId: string): StoredRule {
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

function savedGlobalFixture(updatedAt: string): StoredRule {
	return {
		...editedInput,
		id: storedRule.id,
		created_at: storedRule.created_at,
		updated_at: updatedAt
	} as unknown as StoredRule;
}

function userLanguageFixture(id: string, lang: string, createdAt: string): StoredRule {
	return {
		scope: 'user_language',
		user_language: lang,
		rule: { track: { languages: [lang] } },
		id,
		created_at: createdAt,
		updated_at: createdAt
	} as unknown as StoredRule;
}

function mockScopeChangeDetailFallback(initial: StoredRule) {
	return vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult([initial, highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'page refresh down'))
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockRejectedValueOnce(problemError(503, 'Service Unavailable', 'detail lookup unavailable'));
}

function mockPinnedAndOffPageSaveFailure(pinnedOther: StoredRule) {
	return vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([pinnedOther, storedRule], 'cursor-3') as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'initial save refresh down'))
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce({ data: pinnedOther } as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'refresh failed with other pinned'));
}

function mockSelectionChangePages(initial: StoredRule, updated: StoredRule) {
	return vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult([initial, highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([secondPageRule], 'cursor-3') as never)
		.mockResolvedValueOnce(pageResult([updated]) as never);
}

function mockMovedBehindStaleCursorPages(trailingGlobal: StoredRule) {
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

describe('library processing rules import/export and bounded pagination', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('requests the bounded page endpoint on mount and shows empty state when no rules exist', async () => {
		const getSpy = vi.spyOn(api, 'GET').mockResolvedValue(pageResult([]) as never);

		render(LibraryProcessingRules);

		expect(await screen.findByText(m.modules_rules_empty())).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.modules_rules_export() })).toBeNull();
		expect(screen.queryByRole('button', { name: m.modules_rules_load_more() })).toBeNull();
		expect(getSpy).toHaveBeenCalledTimes(1);
		expect(getSpy).toHaveBeenCalledWith(
			RULES_PAGE_PATH,
			expect.objectContaining({
				params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } }
			})
		);
	});

	it('rejects an empty document without calling the API', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue(pageResult([]) as never);
		const postSpy = vi.spyOn(api, 'POST');

		render(LibraryProcessingRules);
		await screen.findByText(m.modules_rules_empty());

		await typeDocument('   \n ');
		await fireEvent.click(importButton());

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain(m.modules_rules_document_empty());
		expect(documentField().getAttribute('aria-invalid')).toBe('true');
		expect(postSpy).not.toHaveBeenCalled();
	});

	it.each([
		{ format: 'JSON' as const, key: 'json' as const, broken: '{"scope": "global", ' },
		{ format: 'YAML' as const, key: 'yaml' as const, broken: 'scope: [unclosed\nname: "oops' }
	])(
		'rejects a malformed $format document validated by the real reader and never POSTs',
		async ({ format, key, broken }) => {
			expect(() => readRuleDocument(broken, key)).toThrow();

			const getSpy = vi.spyOn(api, 'GET').mockResolvedValue(pageResult([]) as never);
			const postSpy = vi.spyOn(api, 'POST');
			const toastSuccessSpy = vi.spyOn(toast, 'success');

			render(LibraryProcessingRules);
			await screen.findByText(m.modules_rules_empty());

			await chooseFormat(format);
			await typeDocument(broken);
			await fireEvent.click(importButton());

			const alert = await screen.findByRole('alert');
			expect((alert.textContent ?? '').trim().length).toBeGreaterThan(0);
			expect(documentField().getAttribute('aria-invalid')).toBe('true');
			expect(documentField().getAttribute('aria-describedby')).toBe(alert.id);
			expect(documentField().value).toBe(broken);

			expect(postSpy).not.toHaveBeenCalled();
			expect(toastSuccessSpy).not.toHaveBeenCalled();
			expect(getSpy).toHaveBeenCalledTimes(1);
		}
	);

	it.each([
		{ format: 'JSON' as const, invalidNull: 'null' },
		{ format: 'JSON' as const, invalidNull: '{"scope": "global_default", "rule": null}' },
		{ format: 'YAML' as const, invalidNull: 'null\n' }
	])(
		'rejects $format null rule input on import and save without calling the API',
		async ({ format, invalidNull }) => {
			vi.spyOn(api, 'GET').mockResolvedValue(pageResult([storedRule]) as never);
			const postSpy = vi.spyOn(api, 'POST');
			const putSpy = vi.spyOn(api, 'PUT');

			render(LibraryProcessingRules);
			await openRuleAsJson(storedRule.id);
			await chooseFormat(format);
			await typeDocument(invalidNull);

			await fireEvent.click(importButton());
			const importAlert = await screen.findByRole('alert');
			expect((importAlert.textContent ?? '').trim().length).toBeGreaterThan(0);
			expect(documentField().getAttribute('aria-invalid')).toBe('true');
			expect(postSpy).not.toHaveBeenCalled();

			// Clear the Import validation state via Export before exercising Save validation.
			await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
			expect(screen.queryByRole('alert')).toBeNull();
			expect(documentField().getAttribute('aria-invalid')).toBeNull();

			await typeDocument(invalidNull);
			const save = saveButton() as HTMLButtonElement;
			expect(save.disabled).toBe(false);
			await fireEvent.click(save);

			const saveAlert = await screen.findByRole('alert');
			expect((saveAlert.textContent ?? '').trim().length).toBeGreaterThan(0);
			expect(documentField().getAttribute('aria-invalid')).toBe('true');
			expect(screen.getByRole('status').textContent).toBe('');
			expect(postSpy).not.toHaveBeenCalled();
			expect(putSpy).not.toHaveBeenCalled();
		}
	);

	it.each([
		{ format: 'JSON' as const, document: jsonDocument },
		{ format: 'YAML' as const, document: yamlDocument }
	])(
		'imports a valid $format document with the exact payload, then refreshes the bounded list',
		async ({ format, document }) => {
			const getSpy = vi
				.spyOn(api, 'GET')
				.mockResolvedValueOnce(pageResult([]) as never)
				.mockResolvedValueOnce(pageResult([storedRule]) as never);
			const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);
			const toastSuccessSpy = vi.spyOn(toast, 'success');

			render(LibraryProcessingRules);
			await screen.findByText(m.modules_rules_empty());

			await chooseFormat(format);
			await typeDocument(document);
			await fireEvent.click(importButton());

			const select = (await screen.findByLabelText(
				m.modules_rules_select_label()
			)) as HTMLSelectElement;
			expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
			expect(select.value).toBe(storedRule.id);

			expect(postSpy).toHaveBeenCalledTimes(1);
			expect(postSpy).toHaveBeenCalledWith(RULES_PATH, {
				body: ruleInput,
				signal: expect.anything()
			});

			expect(getSpy).toHaveBeenCalledTimes(2);
			expect(getSpy.mock.calls[1]?.[0]).toBe(RULES_PAGE_PATH);
			expect(postSpy.mock.invocationCallOrder[0]).toBeGreaterThan(
				getSpy.mock.invocationCallOrder[0]!
			);
			expect(postSpy.mock.invocationCallOrder[0]).toBeLessThan(getSpy.mock.invocationCallOrder[1]!);

			expect(toastSuccessSpy).toHaveBeenCalledWith(m.modules_rules_import_success());
			await waitFor(() => {
				const status = screen.getByRole('status');
				expect(status.textContent).toBe(m.modules_rules_import_success());
			});
			expect(screen.queryByRole('alert')).toBeNull();
		}
	);

	it('surfaces an API failure on import without refreshing the list', async () => {
		const getSpy = vi.spyOn(api, 'GET').mockResolvedValue(pageResult([]) as never);
		const postSpy = vi.spyOn(api, 'POST').mockRejectedValue(new Error('Rule conflict'));
		const toastErrorSpy = vi.spyOn(toast, 'error');
		const toastSuccessSpy = vi.spyOn(toast, 'success');

		render(LibraryProcessingRules);
		await screen.findByText(m.modules_rules_empty());

		await chooseFormat('JSON');
		await typeDocument(jsonDocument);
		await fireEvent.click(importButton());

		await waitFor(() => expect(toastErrorSpy).toHaveBeenCalled());
		expect(postSpy).toHaveBeenCalledTimes(1);
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, {
			body: ruleInput,
			signal: expect.anything()
		});
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		expect(getSpy).toHaveBeenCalledTimes(1);
		expect(documentField().value).toBe(jsonDocument);
		await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
	});

	it('still creates with POST when a list row happens to be selected', async () => {
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValue(pageResult([storedRule, otherRule]) as never);
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: otherRule } as never);
		const putSpy = vi.spyOn(api, 'PUT');

		render(LibraryProcessingRules);
		const select = await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: storedRule.id } });

		await chooseFormat('JSON');
		await typeDocument(jsonDocument);
		await fireEvent.click(importButton());

		await waitFor(() => expect(postSpy).toHaveBeenCalledTimes(1));
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, {
			body: ruleInput,
			signal: expect.anything()
		});
		await waitFor(() => expect(getSpy).toHaveBeenCalledTimes(2));
		expect(putSpy).not.toHaveBeenCalled();
	});

	it('keeps save disabled until a rule document was opened for editing', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue(pageResult([storedRule]) as never);
		const putSpy = vi.spyOn(api, 'PUT');

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());

		expect((saveButton() as HTMLButtonElement).disabled).toBe(true);
		await openRuleAsJson(storedRule.id);
		expect((saveButton() as HTMLButtonElement).disabled).toBe(false);
		expect(putSpy).not.toHaveBeenCalled();
	});

	it('saves the opened rule by PUT with its id even after another row is selected', async () => {
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValue(pageResult([storedRule, otherRule]) as never);
		const put = deferred<unknown>();
		const putSpy = vi.spyOn(api, 'PUT').mockReturnValue(put.promise as never);
		const postSpy = vi.spyOn(api, 'POST');
		const toastSuccessSpy = vi.spyOn(toast, 'success');

		render(LibraryProcessingRules);
		await openRuleAsJson(storedRule.id);
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();
		await typeDocument(editedDocument);

		// Selecting another row must not retarget the opened document.
		const select = screen.getByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: otherRule.id } });
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();

		const save = saveButton() as HTMLButtonElement;
		await fireEvent.click(save);

		expect(putSpy).toHaveBeenCalledTimes(1);
		expect(putSpy).toHaveBeenCalledWith(RULE_PATH, {
			signal: expect.anything(),
			params: { path: { ruleId: storedRule.id } },
			body: editedInput
		});
		expect(postSpy).not.toHaveBeenCalled();
		// Controlled promise: still pending, so the UI is busy and cannot double-submit.
		expect(save.disabled).toBe(true);
		expect(save.getAttribute('aria-busy')).toBe('true');
		await fireEvent.click(save);
		expect(putSpy).toHaveBeenCalledTimes(1);

		put.resolve({ data: storedRule });
		await waitFor(() =>
			expect(screen.getByRole('status').textContent).toBe(m.modules_rules_save_success())
		);
		expect(toastSuccessSpy).toHaveBeenCalledWith(m.modules_rules_save_success());
		expect(getSpy).toHaveBeenCalledTimes(2);
		const refreshed = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		expect(refreshed.value).toBe(otherRule.id);
		expect(postSpy).not.toHaveBeenCalled();
	});

	it('keeps the document editable and the target after a failed update, then retries the same rule', async () => {
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValue(pageResult([storedRule, otherRule]) as never);
		const put = deferred<unknown>();
		const putSpy = vi
			.spyOn(api, 'PUT')
			.mockReturnValueOnce(put.promise as never)
			.mockResolvedValueOnce({ data: storedRule } as never);
		const toastErrorSpy = vi.spyOn(toast, 'error');
		const toastSuccessSpy = vi.spyOn(toast, 'success');

		render(LibraryProcessingRules);
		await openRuleAsJson(storedRule.id);
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());
		expect(putSpy).toHaveBeenCalledTimes(1);

		put.reject(new Error('Rule gone'));
		await waitFor(() => expect(toastErrorSpy).toHaveBeenCalled());
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		// No refresh after a failed update; text and target survive and stay editable.
		expect(getSpy).toHaveBeenCalledTimes(1);
		expect(documentField().value).toBe(editedDocument);
		await waitFor(() => expect(documentField().disabled).toBe(false));
		await waitFor(() => expect((saveButton() as HTMLButtonElement).disabled).toBe(false));
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();

		await fireEvent.click(saveButton());
		await waitFor(() => expect(putSpy).toHaveBeenCalledTimes(2));
		expect(putSpy).toHaveBeenLastCalledWith(RULE_PATH, {
			signal: expect.anything(),
			params: { path: { ruleId: storedRule.id } },
			body: editedInput
		});
		await waitFor(() =>
			expect(toastSuccessSpy).toHaveBeenCalledWith(m.modules_rules_save_success())
		);
	});

	it('ignores an older load that resolves after a newer one', async () => {
		const older = deferred<unknown>();
		const newer = deferred<unknown>();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockReturnValueOnce(older.promise as never)
			.mockReturnValueOnce(newer.promise as never);
		vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);

		render(LibraryProcessingRules);
		await chooseFormat('JSON');
		await typeDocument(jsonDocument);
		await fireEvent.click(importButton());
		await waitFor(() => expect(getSpy).toHaveBeenCalledTimes(2));

		newer.resolve(pageResult([storedRule]));
		const select = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		expect(select.value).toBe(storedRule.id);

		older.resolve(pageResult([otherRule]));
		await flush();

		const after = screen.getByLabelText(m.modules_rules_select_label()) as HTMLSelectElement;
		expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
		expect(
			screen.queryByRole('option', { name: `${otherRule.scope} · ${otherRule.id}` })
		).toBeNull();
		expect(after.value).toBe(storedRule.id);
	});

	it('ignores an older load failure after a newer load succeeded', async () => {
		const older = deferred<unknown>();
		const newer = deferred<unknown>();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockReturnValueOnce(older.promise as never)
			.mockReturnValueOnce(newer.promise as never);
		vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);

		render(LibraryProcessingRules);
		await chooseFormat('JSON');
		await typeDocument(jsonDocument);
		await fireEvent.click(importButton());
		await waitFor(() => expect(getSpy).toHaveBeenCalledTimes(2));

		newer.resolve(pageResult([storedRule]));
		await screen.findByRole('option', { name: optionLabel });

		older.reject(new Error('stale failure'));
		await flush();

		expect(screen.queryByRole('button', { name: m.modules_rules_retry() })).toBeNull();
		expect(screen.queryByRole('alert')).toBeNull();
		expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
		expect((screen.getByLabelText(m.modules_rules_select_label()) as HTMLSelectElement).value).toBe(
			storedRule.id
		);
	});

	it('keeps loading while only an older load has completed', async () => {
		const older = deferred<unknown>();
		const newer = deferred<unknown>();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockReturnValueOnce(older.promise as never)
			.mockReturnValueOnce(newer.promise as never);
		vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);

		render(LibraryProcessingRules);
		await chooseFormat('JSON');
		await typeDocument(jsonDocument);
		await fireEvent.click(importButton());
		await waitFor(() => expect(getSpy).toHaveBeenCalledTimes(2));

		older.resolve(pageResult([otherRule]));
		await flush();

		expect(screen.getByText(m.loading())).toBeTruthy();
		expect(
			screen.queryByRole('option', { name: `${otherRule.scope} · ${otherRule.id}` })
		).toBeNull();

		newer.resolve(pageResult([storedRule]));
		await screen.findByRole('option', { name: optionLabel });
		expect((screen.getByLabelText(m.modules_rules_select_label()) as HTMLSelectElement).value).toBe(
			storedRule.id
		);
		expect(screen.queryByText(m.loading())).toBeNull();
	});

	it('keeps export disabled until a rule is selected', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue(pageResult([storedRule]) as never);

		render(LibraryProcessingRules);

		const exportButton = (await screen.findByRole('button', {
			name: m.modules_rules_export()
		})) as HTMLButtonElement;
		expect(exportButton.disabled).toBe(true);

		const select = screen.getByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: storedRule.id } });

		expect(exportButton.disabled).toBe(false);
	});

	it('exports the selected rule as YAML and JSON and round-trips through import', async () => {
		vi.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([storedRule]) as never)
			.mockResolvedValueOnce(pageResult([storedRule]) as never);
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);

		render(LibraryProcessingRules);

		const select = await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: storedRule.id } });

		await chooseFormat('JSON');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		const exportedJson = documentField().value;
		expect(() => JSON.parse(exportedJson)).not.toThrow();
		expect(readRuleDocument(exportedJson, 'json')).toEqual(readRuleDocument(jsonDocument, 'json'));
		expect(screen.getByRole('status').textContent).toBe(m.modules_rules_export_success());

		await chooseFormat('YAML');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		const exportedYaml = documentField().value;
		expect(exportedYaml.trimStart().startsWith('{')).toBe(false);
		expect(readRuleDocument(exportedYaml, 'yaml')).toEqual(readRuleDocument(yamlDocument, 'yaml'));

		await fireEvent.click(importButton());
		await waitFor(() => expect(postSpy).toHaveBeenCalledTimes(1));
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, {
			body: ruleInput,
			signal: expect.anything()
		});
	});

	it('pages through multiple bounded pages, preserves selection, reaches later rules for export, and hides load-more on the final page', async () => {
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-page-2') as never)
			.mockResolvedValueOnce(pageResult([secondPageRule], 'cursor-page-3') as never)
			.mockResolvedValueOnce(pageResult([storedRule]) as never);

		render(LibraryProcessingRules);

		const select = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		await fireEvent.change(select, { target: { value: highPrecedenceRule.id } });
		expect(select.value).toBe(highPrecedenceRule.id);

		// Page 2 load preserves page-1 selection and appends page-2 rule in precedence order.
		const loadMore = screen.getByRole('button', { name: m.modules_rules_load_more() });
		await fireEvent.click(loadMore);

		await waitFor(() => {
			expect(
				screen.getByRole('option', {
					name: `${secondPageRule.scope} · ${secondPageRule.id}`
				})
			).toBeTruthy();
		});
		expect(select.value).toBe(highPrecedenceRule.id);
		expect(getSpy).toHaveBeenNthCalledWith(
			2,
			RULES_PAGE_PATH,
			expect.objectContaining({
				params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT, cursor: 'cursor-page-2' } }
			})
		);

		// Final page (has_more=false) appends the global_default rule and removes the load-more button.
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => {
			expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
		});
		expect(screen.queryByRole('button', { name: m.modules_rules_load_more() })).toBeNull();
		expect(select.value).toBe(highPrecedenceRule.id);

		// Select and export a rule reached on a later page.
		await fireEvent.change(select, { target: { value: secondPageRule.id } });
		await chooseFormat('JSON');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		expect(readRuleDocument(documentField().value, 'json')).toEqual({
			scope: 'media_type',
			media_type: 'anime',
			rule: { track: { languages: ['ja'] } }
		});
	});

	it('retains a newly imported lower-precedence rule as selected and exportable even when page 1 does not yet reach it', async () => {
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-page-2') as never)
			// After import, page 1 still holds only highPrecedenceRule while has_more=true.
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-page-2') as never)
			// Loading page 2 reaches storedRule without duplicating the option.
			.mockResolvedValueOnce(pageResult([storedRule]) as never);
		vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());

		await chooseFormat('YAML');
		await typeDocument(yamlDocument);
		await fireEvent.click(importButton());

		const select = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		await waitFor(() => expect(select.value).toBe(storedRule.id));

		// Precedence order is preserved: item_override appears before global_default.
		const optionValues = Array.from(select.options).map((opt) => opt.value);
		expect(optionValues).toEqual(['', highPrecedenceRule.id, storedRule.id]);

		// Export works immediately for the off-page imported rule.
		await chooseFormat('JSON');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		expect(readRuleDocument(documentField().value, 'json')).toEqual(ruleInput);

		// No extra detail GET was needed because POST already returned storedRule.
		expect(getSpy).toHaveBeenCalledTimes(2);

		// Loading the final page deduplicates the pinned rule cleanly while keeping it selected.
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() =>
			expect(screen.queryByRole('button', { name: m.modules_rules_load_more() })).toBeNull()
		);
		expect(Array.from(select.options).map((opt) => opt.value)).toEqual([
			'',
			highPrecedenceRule.id,
			storedRule.id
		]);
		expect(select.value).toBe(storedRule.id);
	});

	it('keeps loaded rules and current selection intact when loading a subsequent page fails, and recovers on retry', async () => {
		vi.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-next') as never)
			.mockRejectedValueOnce(
				new ProblemError({
					type: 'about:blank',
					title: 'Bad Gateway',
					status: 502,
					detail: 'temporary upstream failure'
				})
			)
			.mockResolvedValueOnce(pageResult([storedRule]) as never);

		render(LibraryProcessingRules);

		const select = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		await fireEvent.change(select, { target: { value: highPrecedenceRule.id } });

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		expect(await screen.findByText(/temporary upstream failure/)).toBeTruthy();

		// Existing page-1 options and selection remain usable.
		expect(select.value).toBe(highPrecedenceRule.id);
		expect(
			(screen.getByRole('button', { name: m.modules_rules_export() }) as HTMLButtonElement).disabled
		).toBe(false);

		// Retrying load-more succeeds and clears the error.
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy());
		expect(screen.queryByText(/temporary upstream failure/)).toBeNull();
		expect(select.value).toBe(highPrecedenceRule.id);
	});

	it('retries initial load after an API failure and restores bounded page options', async () => {
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockRejectedValueOnce(
				new ProblemError({
					type: 'about:blank',
					title: 'Service Unavailable',
					status: 503,
					detail: 'database starting'
				})
			)
			.mockResolvedValueOnce(pageResult([storedRule]) as never);

		render(LibraryProcessingRules);

		expect(await screen.findByText(/database starting/)).toBeTruthy();
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));

		const select = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
		expect(select.value).toBe('');
		expect(getSpy).toHaveBeenCalledTimes(2);
	});

	it('reconciles a stale selection when Retry returns a partial page and the detail lookup reports 404', async () => {
		const createdRule = userLanguageFixture(
			'018f0000-0000-7000-8000-000000000004',
			'fr',
			'2026-10-25T00:00:30Z'
		);
		const createdLabel = `${createdRule.scope} · ${createdRule.id}`;
		const createdInput = {
			scope: 'user_language',
			user_language: 'fr',
			rule: { track: { languages: ['fr'] } }
		};
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'import refresh down'))
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockRejectedValueOnce(problemError(404, 'Not Found', 'rule deleted'));
		// A real 201 always carries the created rule.
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: createdRule } as never);
		const toastSuccessSpy = vi.spyOn(toast, 'success');
		const toastErrorSpy = vi.spyOn(toast, 'error');

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());

		await chooseFormat('JSON');
		await typeDocument(JSON.stringify(createdInput));
		await fireEvent.click(importButton());

		// The page refresh failed: Retry is offered and the created rule stays selected.
		expect(await screen.findByText(/import refresh down/)).toBeTruthy();
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, {
			body: createdInput,
			signal: expect.anything()
		});
		expect(screen.getByRole('button', { name: m.modules_rules_retry() })).toBeTruthy();
		expect(ruleSelect().value).toBe(createdRule.id);
		expect(screen.getByRole('option', { name: createdLabel })).toBeTruthy();
		expect(screen.getByRole('status').textContent).toBe(m.modules_rules_import_success());

		// Retry returns a partial page without the selected rule; the detail lookup answers 404.
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
		await waitFor(() =>
			expect(getSpy).toHaveBeenCalledWith(
				RULE_DETAIL_PATH,
				expect.objectContaining({ params: { path: { ruleId: createdRule.id } } })
			)
		);
		await waitFor(() => expect(ruleSelect().value).toBe(''));
		expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id]);
		expect(screen.queryByRole('option', { name: createdLabel })).toBeNull();
		expect(screen.queryByRole('button', { name: m.modules_rules_retry() })).toBeNull();
		expect(screen.queryByRole('alert')).toBeNull();
		expect(
			(screen.getByRole('button', { name: m.modules_rules_export() }) as HTMLButtonElement).disabled
		).toBe(true);
		expect(toastSuccessSpy).toHaveBeenCalledWith(m.modules_rules_import_success());
		expect(toastErrorSpy).not.toHaveBeenCalled();
		expect(getSpy).toHaveBeenCalledTimes(4);
	});

	it('keeps an imported off-page rule selected and exportable when the refresh fails, and retains it after Retry', async () => {
		const createdRule = userLanguageFixture(
			'018f0000-0000-7000-8000-000000000005',
			'fr',
			'2026-10-25T00:00:40Z'
		);
		const createdInput = {
			scope: 'user_language',
			user_language: 'fr',
			rule: { track: { languages: ['fr'] } }
		};
		const createdLabel = `${createdRule.scope} · ${createdRule.id}`;
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'import refresh down'))
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockResolvedValueOnce({ data: createdRule } as never);
		vi.spyOn(api, 'POST').mockResolvedValue({ data: createdRule } as never);

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());

		await chooseFormat('JSON');
		await typeDocument(JSON.stringify(createdInput));
		await fireEvent.click(importButton());
		expect(await screen.findByText(/import refresh down/)).toBeTruthy();

		expect(ruleSelect().value).toBe(createdRule.id);
		expect(screen.getByRole('option', { name: createdLabel })).toBeTruthy();
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		expect(readRuleDocument(documentField().value, 'json')).toEqual(createdInput);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
		await waitFor(() =>
			expect(getSpy).toHaveBeenCalledWith(
				RULE_DETAIL_PATH,
				expect.objectContaining({ params: { path: { ruleId: createdRule.id } } })
			)
		);
		await waitFor(() => expect(screen.queryByText(/import refresh down/)).toBeNull());
		await waitFor(() => expect(ruleSelect().value).toBe(createdRule.id));
		expect(screen.getByRole('option', { name: createdLabel })).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.modules_rules_retry() })).toBeNull();
		expect(getSpy).toHaveBeenCalledTimes(4);
	});

	it('ends a hung list load at the 15s request deadline and Retry gets a fresh, unaborted signal', async () => {
		const { deadlines, timeoutSpy } = mockDeadlines();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockImplementationOnce(hangUntilAborted as never)
			.mockImplementationOnce(pageHonoringSignal([storedRule]) as never);

		render(LibraryProcessingRules);
		expect(await screen.findByText(m.loading())).toBeTruthy();
		expect(timeoutSpy).toHaveBeenNthCalledWith(1, REQUEST_TIMEOUT_MS);

		expireDeadline(deadlines[0]!);

		// The timeout is handled as a current request: loading clears and Retry appears.
		const retry = await screen.findByRole('button', { name: m.modules_rules_retry() });
		expect(screen.queryByText(m.loading())).toBeNull();

		await fireEvent.click(retry);
		await screen.findByRole('option', { name: optionLabel });
		expect(screen.queryByRole('button', { name: m.modules_rules_retry() })).toBeNull();
		expect(getSpy).toHaveBeenCalledTimes(2);
		expect(timeoutSpy).toHaveBeenNthCalledWith(2, REQUEST_TIMEOUT_MS);
		expect(deadlines[1]!.signal.aborted).toBe(false);
		const retrySignal = (getSpy.mock.calls[1]?.[1] as unknown as { signal: AbortSignal }).signal;
		expect(retrySignal.aborted).toBe(false);
	});

	it('treats an import timeout as an unknown outcome: keeps the draft, never re-POSTs, and checks state with a read-only refresh', async () => {
		const { deadlines, timeoutSpy } = mockDeadlines();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([]) as never)
			// The server did commit the import although its response never arrived.
			.mockImplementationOnce(pageHonoringSignal([storedRule]) as never);
		const postSpy = vi.spyOn(api, 'POST').mockImplementation(hangUntilAborted as never);
		const putSpy = vi.spyOn(api, 'PUT');
		const toastErrorSpy = vi.spyOn(toast, 'error');
		const toastSuccessSpy = vi.spyOn(toast, 'success');

		render(LibraryProcessingRules);
		await screen.findByText(m.modules_rules_empty());

		await chooseFormat('JSON');
		await typeDocument(jsonDocument);
		await fireEvent.click(importButton());

		// One deadline for the initial load, one for the write.
		expect(timeoutSpy).toHaveBeenCalledTimes(2);
		expect(timeoutSpy).toHaveBeenLastCalledWith(REQUEST_TIMEOUT_MS);
		const writeDeadline = deadlines[1]!;
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, {
			body: ruleInput,
			signal: writeDeadline.signal
		});
		const busyButton = screen.getByRole('button', { name: m.modules_rules_importing() });
		expect(busyButton.getAttribute('aria-busy')).toBe('true');

		expireDeadline(writeDeadline);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain(m.modules_rules_write_timeout());
		expect(toastErrorSpy).toHaveBeenCalledWith(m.modules_rules_write_timeout());
		await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
		expect(importButton().getAttribute('aria-busy')).toBe('false');
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		expect(screen.getByRole('status').textContent).toBe('');
		expect(documentField().value).toBe(jsonDocument);

		// Nothing repeats the mutation or refreshes on its own.
		await flush();
		expect(postSpy).toHaveBeenCalledTimes(1);
		expect(getSpy).toHaveBeenCalledTimes(1);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
		await screen.findByRole('option', { name: optionLabel });
		expect(getSpy).toHaveBeenCalledTimes(2);
		expect(getSpy.mock.calls[1]?.[0]).toBe(RULES_PAGE_PATH);
		expect(timeoutSpy).toHaveBeenCalledTimes(3);
		expect(timeoutSpy).toHaveBeenLastCalledWith(REQUEST_TIMEOUT_MS);
		expect(deadlines[2]!.signal.aborted).toBe(false);
		expect(postSpy).toHaveBeenCalledTimes(1);
		expect(putSpy).not.toHaveBeenCalled();

		// Reading the state confirms nothing: no success feedback and nothing pinned or selected.
		expect(ruleSelect().value).toBe('');
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		expect(screen.getByRole('status').textContent).toBe('');
		await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
		expect(documentField().value).toBe(jsonDocument);
	});

	it('treats a save timeout as an unknown outcome: keeps the draft and target, never re-PUTs, and checks state with a read-only refresh', async () => {
		const updatedInput = {
			scope: 'media_type',
			media_type: 'movie',
			rule: { track: { languages: ['fr'] } }
		};
		const updatedDocument = JSON.stringify(updatedInput, null, 2);
		const updatedRule = {
			...updatedInput,
			id: storedRule.id,
			created_at: storedRule.created_at,
			updated_at: '2026-10-25T00:12:00Z'
		} as unknown as StoredRule;
		const updatedLabel = `${updatedRule.scope} · ${updatedRule.id}`;

		const { deadlines, timeoutSpy } = mockDeadlines();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([storedRule]) as never)
			// The server did commit the update although its response never arrived.
			.mockImplementationOnce(pageHonoringSignal([updatedRule]) as never);
		const putSpy = vi.spyOn(api, 'PUT').mockImplementation(hangUntilAborted as never);
		const postSpy = vi.spyOn(api, 'POST');
		const toastErrorSpy = vi.spyOn(toast, 'error');
		const toastSuccessSpy = vi.spyOn(toast, 'success');

		render(LibraryProcessingRules);
		await openRuleAsJson(storedRule.id);
		await typeDocument(updatedDocument);
		const save = saveButton();
		await fireEvent.click(save);

		expect(timeoutSpy).toHaveBeenCalledTimes(2);
		expect(timeoutSpy).toHaveBeenLastCalledWith(REQUEST_TIMEOUT_MS);
		const writeDeadline = deadlines[1]!;
		expect(putSpy).toHaveBeenCalledWith(RULE_PATH, {
			signal: writeDeadline.signal,
			params: { path: { ruleId: storedRule.id } },
			body: updatedInput
		});
		expect(save.getAttribute('aria-busy')).toBe('true');

		expireDeadline(writeDeadline);

		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain(m.modules_rules_write_timeout());
		expect(toastErrorSpy).toHaveBeenCalledWith(m.modules_rules_write_timeout());
		await waitFor(() => expect((saveButton() as HTMLButtonElement).disabled).toBe(false));
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		expect(screen.getByRole('status').textContent).toBe('');
		expect(documentField().value).toBe(updatedDocument);
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();

		// Nothing repeats the mutation or refreshes on its own.
		await flush();
		expect(putSpy).toHaveBeenCalledTimes(1);
		expect(getSpy).toHaveBeenCalledTimes(1);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
		await screen.findByRole('option', { name: updatedLabel });
		expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
		expect(getSpy).toHaveBeenCalledTimes(2);
		expect(getSpy.mock.calls[1]?.[0]).toBe(RULES_PAGE_PATH);
		expect(timeoutSpy).toHaveBeenCalledTimes(3);
		expect(timeoutSpy).toHaveBeenLastCalledWith(REQUEST_TIMEOUT_MS);
		expect(deadlines[2]!.signal.aborted).toBe(false);
		expect(putSpy).toHaveBeenCalledTimes(1);
		expect(postSpy).not.toHaveBeenCalled();

		// Reading the state confirms nothing: no success feedback, and the draft stays editable.
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		expect(screen.getByRole('status').textContent).toBe('');
		await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
		expect(documentField().value).toBe(updatedDocument);
	});

	it('retains the authoritative PUT result and user feedback when the subsequent page refresh fails', async () => {
		const updatedInput = {
			scope: 'media_type',
			media_type: 'movie',
			rule: { track: { languages: ['fr'] } }
		};
		const updatedDocument = JSON.stringify(updatedInput, null, 2);
		const updatedRule = {
			...updatedInput,
			id: storedRule.id,
			created_at: storedRule.created_at,
			updated_at: '2026-10-25T00:05:00Z'
		} as unknown as StoredRule;
		const updatedLabel = `${updatedRule.scope} · ${updatedRule.id}`;

		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([storedRule]) as never)
			.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'page refresh unavailable'))
			.mockResolvedValueOnce(pageResult([updatedRule]) as never);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({ data: updatedRule } as never);
		const toastSuccessSpy = vi.spyOn(toast, 'success');
		const toastErrorSpy = vi.spyOn(toast, 'error');

		render(LibraryProcessingRules);
		await openRuleAsJson(storedRule.id);
		await typeDocument(updatedDocument);
		await fireEvent.click(saveButton());

		expect(putSpy).toHaveBeenCalledTimes(1);
		expect(await screen.findByText(/page refresh unavailable/)).toBeTruthy();
		expect(toastSuccessSpy).toHaveBeenCalledWith(m.modules_rules_save_success());
		expect(toastErrorSpy).not.toHaveBeenCalled();
		expect(screen.getByRole('status').textContent).toBe(m.modules_rules_save_success());
		expect(screen.getByText(m.modules_rules_editing({ rule: updatedLabel }))).toBeTruthy();

		const select = ruleSelect();
		expect(select.value).toBe(updatedRule.id);
		expect(screen.getByRole('option', { name: updatedLabel })).toBeTruthy();
		expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();

		await typeDocument('{}');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		expect(readRuleDocument(documentField().value, 'json')).toEqual(updatedInput);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
		await waitFor(() => expect(screen.queryByText(/page refresh unavailable/)).toBeNull());
		expect(getSpy).toHaveBeenCalledTimes(3);
		expect(ruleSelect().value).toBe(updatedRule.id);
	});

	it('reuses authoritative PUT data without a redundant detail GET and retains it when a later detail refresh fails', async () => {
		const initialEditable = itemOverrideFixture('ep-202');
		const updatedGlobal = savedGlobalFixture('2026-10-25T00:06:00Z');
		const getSpy = mockScopeChangeDetailFallback(initialEditable);
		vi.spyOn(api, 'PUT').mockResolvedValue({ data: updatedGlobal } as never);
		const toastSuccessSpy = vi.spyOn(toast, 'success');
		const toastErrorSpy = vi.spyOn(toast, 'error');

		render(LibraryProcessingRules);
		await openRuleAsJson(initialEditable.id);
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());

		const select = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		await waitFor(() => expect(select.value).toBe(updatedGlobal.id));
		expect(getSpy).toHaveBeenCalledTimes(2);
		expect(selectOptionValues(select)).toEqual(['', highPrecedenceRule.id, updatedGlobal.id]);
		expect(toastSuccessSpy).toHaveBeenCalledWith(m.modules_rules_save_success());
		expect(toastErrorSpy).not.toHaveBeenCalled();

		// Failed page refresh + Retry -> page 1 succeeds + detail GET fails 503 -> fallback retains updatedGlobal.
		await fireEvent.click(saveButton());
		expect(await screen.findByText(/page refresh down/)).toBeTruthy();
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
		await waitFor(() =>
			expect(getSpy).toHaveBeenCalledWith(
				RULE_DETAIL_PATH,
				expect.objectContaining({ params: { path: { ruleId: updatedGlobal.id } } })
			)
		);

		const retried = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		await waitFor(() => expect(retried.value).toBe(updatedGlobal.id));
		expect(selectOptionValues(retried)).toEqual(['', highPrecedenceRule.id, updatedGlobal.id]);
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();

		await typeDocument('{}');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		expect(readRuleDocument(documentField().value, 'json')).toEqual(editedInput);
	});

	it('retains a saved off-page rule alongside another pinned selection when refresh fails', async () => {
		const pinnedOther = userLanguageFixture(
			'018f0000-0000-7000-8000-000000000008',
			'de',
			'2026-10-25T00:02:00Z'
		);
		const savedGlobal = savedGlobalFixture('2026-10-25T00:07:00Z');
		mockPinnedAndOffPageSaveFailure(pinnedOther);
		vi.spyOn(api, 'PUT')
			.mockResolvedValueOnce({ data: undefined } as never)
			.mockResolvedValueOnce({ data: savedGlobal } as never);

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy());

		await openRuleAsJson(storedRule.id);
		await fireEvent.change(ruleSelect(), { target: { value: pinnedOther.id } });

		// Save #1 + Retry resets loadedRules to page 1 and pins pinnedOther while storedRule is off-page.
		await fireEvent.click(saveButton());
		expect(await screen.findByText(/initial save refresh down/)).toBeTruthy();
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
		await waitFor(() => expect(screen.queryByText(/initial save refresh down/)).toBeNull());

		// Save #2 returns authoritative savedGlobal, then page refresh fails with 502.
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());
		expect(await screen.findByText(/refresh failed with other pinned/)).toBeTruthy();

		const afterSelect = ruleSelect();
		expect(afterSelect.value).toBe(pinnedOther.id);
		expect(selectOptionValues(afterSelect)).toEqual([
			'',
			highPrecedenceRule.id,
			pinnedOther.id,
			savedGlobal.id
		]);
		expect(documentField().value).toBe(editedDocument);
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();
	});

	it('preserves selection, editor draft, saved off-page rule, and keyset pagination when selection changes during save', async () => {
		const initialEditable = itemOverrideFixture('ep-303');
		const updatedGlobal = savedGlobalFixture('2026-10-25T00:08:00Z');
		const put = deferred<unknown>();
		const getSpy = mockSelectionChangePages(initialEditable, updatedGlobal);
		vi.spyOn(api, 'PUT').mockReturnValue(put.promise as never);

		render(LibraryProcessingRules);
		await openRuleAsJson(initialEditable.id);
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());
		await fireEvent.change(ruleSelect(), { target: { value: highPrecedenceRule.id } });

		put.resolve({ data: updatedGlobal });
		await waitFor(() =>
			expect(screen.getByRole('status').textContent).toBe(m.modules_rules_save_success())
		);

		const refreshed = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		expect(getSpy).toHaveBeenCalledTimes(2);
		expect(refreshed.value).toBe(highPrecedenceRule.id);
		expect(documentField().value).toBe(editedDocument);
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();
		expect(selectOptionValues(refreshed)).toEqual(['', highPrecedenceRule.id, updatedGlobal.id]);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() =>
			expect(selectOptionValues(refreshed)).toEqual([
				'',
				highPrecedenceRule.id,
				secondPageRule.id,
				updatedGlobal.id
			])
		);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() =>
			expect(screen.queryByRole('button', { name: m.modules_rules_load_more() })).toBeNull()
		);
		expect(selectOptionValues(refreshed)).toEqual([
			'',
			highPrecedenceRule.id,
			secondPageRule.id,
			updatedGlobal.id
		]);
		expect(refreshed.value).toBe(highPrecedenceRule.id);
	});

	it('keeps a saved page-2 rule selectable and exportable when refresh returns only page 1', async () => {
		const savedGlobal = savedGlobalFixture('2026-10-25T00:09:00Z');
		vi.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockResolvedValueOnce(pageResult([storedRule]) as never)
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockResolvedValueOnce(pageResult([savedGlobal]) as never);
		vi.spyOn(api, 'PUT').mockResolvedValue({ data: savedGlobal } as never);

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy());

		await openRuleAsJson(storedRule.id);
		await fireEvent.change(ruleSelect(), { target: { value: highPrecedenceRule.id } });
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());

		await waitFor(() =>
			expect(screen.getByRole('status').textContent).toBe(m.modules_rules_save_success())
		);
		await waitFor(() => expect((saveButton() as HTMLButtonElement).disabled).toBe(false));
		const refreshed = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		expect(refreshed.value).toBe(highPrecedenceRule.id);
		expect(selectOptionValues(refreshed)).toEqual(['', highPrecedenceRule.id, savedGlobal.id]);
		expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();

		await fireEvent.change(refreshed, { target: { value: savedGlobal.id } });
		await typeDocument('{}');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		expect(readRuleDocument(documentField().value, 'json')).toEqual(editedInput);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() =>
			expect(screen.queryByRole('button', { name: m.modules_rules_load_more() })).toBeNull()
		);
		expect(selectOptionValues(refreshed)).toEqual(['', highPrecedenceRule.id, savedGlobal.id]);
	});

	it('retains a saved rule that moved behind a stale cursor across Load more until a reload confirms absence', async () => {
		const trailingGlobal = {
			...storedRule,
			id: '018f0000-0000-7000-8000-000000000009',
			created_at: '2026-10-25T00:10:00Z'
		} as unknown as StoredRule;
		const movedInput = {
			scope: 'user_language',
			user_language: 'fr',
			rule: { track: { languages: ['fr'] } }
		};
		const movedRule = userLanguageFixture(storedRule.id, 'fr', storedRule.created_at);
		mockMovedBehindStaleCursorPages(trailingGlobal);
		vi.spyOn(api, 'PUT')
			.mockResolvedValueOnce({ data: savedGlobalFixture('2026-10-25T00:11:00Z') } as never)
			.mockResolvedValueOnce({ data: movedRule } as never);

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy());

		await openRuleAsJson(storedRule.id);
		await fireEvent.change(ruleSelect(), { target: { value: highPrecedenceRule.id } });
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());
		await waitFor(() =>
			expect(screen.getByRole('status').textContent).toBe(m.modules_rules_save_success())
		);
		await waitFor(() => expect((saveButton() as HTMLButtonElement).disabled).toBe(false));

		await typeDocument(JSON.stringify(movedInput, null, 2));
		await fireEvent.click(saveButton());
		expect(await screen.findByText(/refresh down after scope move/)).toBeTruthy();

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() =>
			expect(screen.queryByRole('button', { name: m.modules_rules_load_more() })).toBeNull()
		);
		expect(selectOptionValues()).toEqual([
			'',
			highPrecedenceRule.id,
			movedRule.id,
			secondPageRule.id,
			trailingGlobal.id
		]);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
		await waitFor(() => expect(screen.queryByText(/refresh down after scope move/)).toBeNull());
		expect(selectOptionValues()).toEqual([
			'',
			highPrecedenceRule.id,
			secondPageRule.id,
			trailingGlobal.id
		]);
	});

	it('shows a Load more failure alongside an existing refresh error and keeps the refresh error with Retry', async () => {
		vi.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'refresh failed 502'))
			.mockRejectedValueOnce(problemError(503, 'Service Unavailable', 'load more failed 503'))
			.mockResolvedValueOnce(pageResult([storedRule]) as never);
		vi.spyOn(api, 'PUT').mockResolvedValue({ data: highPrecedenceRule } as never);

		render(LibraryProcessingRules);
		await openRuleAsJson(highPrecedenceRule.id);
		await fireEvent.click(saveButton());
		expect(await screen.findByText(/refresh failed 502/)).toBeTruthy();

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		expect(await screen.findByText(/load more failed 503/)).toBeTruthy();
		expect(screen.getByText(/refresh failed 502/)).toBeTruthy();
		expect(screen.getAllByRole('alert')).toHaveLength(2);
		expect(screen.getByRole('button', { name: m.modules_rules_retry() })).toBeTruthy();

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy());
		expect(screen.queryByText(/load more failed 503/)).toBeNull();
		expect(screen.getByText(/refresh failed 502/)).toBeTruthy();
		expect(screen.getByRole('button', { name: m.modules_rules_retry() })).toBeTruthy();
	});
});
