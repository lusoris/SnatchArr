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

function saveButton(): HTMLElement {
	return screen.getByRole('button', { name: m.modules_rules_save() });
}

async function openRuleAsJson(id: string): Promise<void> {
	const select = await screen.findByLabelText(m.modules_rules_select_label());
	await fireEvent.change(select, { target: { value: id } });
	await chooseFormat('JSON');
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
}

function importButton(): HTMLElement {
	return screen.getByRole('button', { name: m.modules_rules_import() });
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
			expect(postSpy).toHaveBeenCalledWith(RULES_PATH, { body: ruleInput });

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
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, { body: ruleInput });
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
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, { body: ruleInput });
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
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, { body: ruleInput });
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

	it('clears an off-page selection when single-rule reconciliation reports 404 after refresh', async () => {
		const otherRule = {
			scope: 'user_language',
			user_language: 'fr',
			rule: { track: { languages: ['fr'] } },
			id: '018f0000-0000-7000-8000-000000000004',
			created_at: '2026-10-25T00:00:30Z',
			updated_at: '2026-10-25T00:00:30Z'
		} as unknown as StoredRule;

		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule], 'cursor-2') as never)
			.mockResolvedValueOnce(pageResult([storedRule]) as never)
			// Refresh after import with missing POST body reconciles prior selectedId via single-rule GET -> 404.
			.mockResolvedValueOnce(pageResult([highPrecedenceRule, otherRule], 'cursor-2') as never)
			.mockRejectedValueOnce(
				new ProblemError({
					type: 'about:blank',
					title: 'Not Found',
					status: 404,
					detail: 'rule deleted'
				})
			);
		vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);

		render(LibraryProcessingRules);
		const select = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy());
		await fireEvent.change(select, { target: { value: storedRule.id } });
		expect(select.value).toBe(storedRule.id);

		await chooseFormat('YAML');
		await typeDocument(
			[
				'scope: user_language',
				'user_language: fr',
				'rule:',
				'  track:',
				'    languages:',
				'      - fr',
				''
			].join('\n')
		);
		await fireEvent.click(importButton());

		await waitFor(() =>
			expect(getSpy).toHaveBeenCalledWith(
				RULE_DETAIL_PATH,
				expect.objectContaining({
					params: { path: { ruleId: storedRule.id } }
				})
			)
		);
		const refreshed = (await screen.findByLabelText(
			m.modules_rules_select_label()
		)) as HTMLSelectElement;
		expect(select.isConnected).toBe(false);
		expect(refreshed.value).toBe('');
		expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
	});
});
