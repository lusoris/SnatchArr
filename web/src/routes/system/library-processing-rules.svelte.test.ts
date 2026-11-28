// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { toast } from 'svelte-sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { readRuleDocument } from '#lib/library-processing-document.js';
import { LIBRARY_PROCESSING_RULE_PAGE_LIMIT } from '#lib/library-processing-rule.js';
import { m } from '#lib/paraglide/messages.js';
import LibraryProcessingRules from './LibraryProcessingRules.svelte';
import {
	RULES_PATH,
	RULES_PAGE_PATH,
	ruleInput,
	jsonDocument,
	yamlDocument,
	storedRule,
	optionLabel,
	pageResult,
	documentField,
	typeDocument,
	chooseFormat,
	RULE_PATH,
	otherRule,
	editedInput,
	editedDocument,
	deferred,
	flush,
	saveButton,
	openRuleAsJson,
	importButton
} from './library-processing-rules-test-helpers.js';

describe('library processing rules import and editing', () => {
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
});
