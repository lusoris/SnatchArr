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
	type StoredRule,
	RULES_PATH,
	RULES_PAGE_PATH,
	RULE_DETAIL_PATH,
	ruleInput,
	jsonDocument,
	storedRule,
	highPrecedenceRule,
	secondPageRule,
	optionLabel,
	pageResult,
	documentField,
	typeDocument,
	chooseFormat,
	RULE_PATH,
	editedInput,
	editedDocument,
	deferred,
	flush,
	hangUntilAborted,
	saveButton,
	openRuleAsJson,
	REQUEST_TIMEOUT_MS,
	mockDeadlines,
	expireDeadline,
	pageHonoringSignal,
	importButton,
	problemError,
	ruleSelect,
	selectOptionValues,
	itemOverrideFixture,
	savedGlobalFixture,
	userLanguageFixture,
	mockScopeChangeDetailFallback,
	mockPinnedAndOffPageSaveFailure,
	mockSelectionChangePages,
	mockMovedBehindStaleCursorPages
} from './library-processing-rules-test-helpers.js';

describe('library processing rules deadlines and saved state', () => {
	afterEach(() => {
		vi.restoreAllMocks();
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
		expect(screen.getByRole('alert').textContent).toContain(m.modules_rules_write_timeout());
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

		// Even seeing the updated rule does not turn this refresh into a write confirmation.
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		expect(screen.getByRole('status').textContent).toBe('');
		expect(screen.getByRole('alert').textContent).toContain(m.modules_rules_write_timeout());
		expect(documentField().value).toBe(updatedDocument);

		await typeDocument('');
		await fireEvent.click(saveButton());

		expect(screen.getByText(m.modules_rules_document_empty())).toBeTruthy();
		expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy();
		expect(putSpy).toHaveBeenCalledTimes(1);
		expect(postSpy).not.toHaveBeenCalled();
	});

	it('keeps a timed-out save unconfirmed when the edited rule is beyond the refreshed first page', async () => {
		const firstPage = Array.from({ length: LIBRARY_PROCESSING_RULE_PAGE_LIMIT }, (_, index) => {
			const suffix = String(index + 100).padStart(12, '0');
			return {
				...highPrecedenceRule,
				id: `018f0000-0000-7000-8000-${suffix}`,
				item_id: `ep-${suffix}`
			};
		});
		const cursor = 'next-page';
		const checked = userLanguageFixture(storedRule.id, 'it', storedRule.created_at);
		const { deadlines } = mockDeadlines();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult(firstPage, cursor) as never)
			.mockResolvedValueOnce(pageResult([storedRule]) as never)
			// The edited rule still sorts after these item overrides.
			.mockResolvedValueOnce(pageResult(firstPage, cursor) as never)
			.mockResolvedValueOnce({ data: checked } as never);
		const putSpy = vi.spyOn(api, 'PUT').mockImplementation(hangUntilAborted as never);
		const postSpy = vi.spyOn(api, 'POST');
		const toastSuccessSpy = vi.spyOn(toast, 'success');

		render(LibraryProcessingRules);
		await fireEvent.click(await screen.findByRole('button', { name: m.modules_rules_load_more() }));
		await screen.findByRole('option', { name: optionLabel });
		expect(getSpy).toHaveBeenNthCalledWith(2, RULES_PAGE_PATH, {
			params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT, cursor } },
			signal: expect.anything()
		});
		await openRuleAsJson(storedRule.id);
		await typeDocument(editedDocument);
		// Leave the list selection on page one without changing the editor's save target.
		await fireEvent.change(ruleSelect(), { target: { value: firstPage[0]!.id } });
		await fireEvent.click(saveButton());

		const writeDeadline = deadlines[2]!;
		expect(putSpy).toHaveBeenCalledWith(RULE_PATH, {
			params: { path: { ruleId: storedRule.id } },
			body: editedInput,
			signal: writeDeadline.signal
		});
		expireDeadline(writeDeadline);
		await screen.findByRole('alert');
		await flush();
		expect(getSpy).toHaveBeenCalledTimes(2);
		expect(putSpy).toHaveBeenCalledTimes(1);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
		await waitFor(() =>
			expect(selectOptionValues()).toEqual(['', ...firstPage.map(({ id }) => id)])
		);
		expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
		expect(screen.getByRole('button', { name: m.modules_rules_load_more() })).toBeTruthy();
		expect(getSpy).toHaveBeenCalledTimes(4);
		expect(getSpy).toHaveBeenNthCalledWith(4, RULE_DETAIL_PATH, {
			params: { path: { ruleId: storedRule.id } },
			signal: expect.anything()
		});
		expect(getSpy).toHaveBeenNthCalledWith(3, RULES_PAGE_PATH, {
			params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } },
			signal: expect.anything()
		});
		expect(putSpy).toHaveBeenCalledTimes(1);
		expect(postSpy).not.toHaveBeenCalled();
		expect(screen.getByRole('alert').textContent).toContain(m.modules_rules_write_timeout());
		expect(screen.getByRole('status').textContent).toBe('');
		expect(toastSuccessSpy).not.toHaveBeenCalled();
		expect(documentField().value).toBe(editedDocument);
		// There is no saved overlay for this rule; its editor fallback must still refresh.
		expect(
			screen.getByText(m.modules_rules_editing({ rule: `${checked.scope} · ${checked.id}` }))
		).toBeTruthy();
		expect(screen.queryByText(m.modules_rules_editing({ rule: optionLabel }))).toBeNull();
		expect(ruleSelect().value).toBe(firstPage[0]!.id);
		expect((saveButton() as HTMLButtonElement).disabled).toBe(false);
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

	it.each(['found', 'absent', 'empty body', 'transport failure'] as const)(
		'reconciles an unselected, retained off-page PUT target after a timeout: %s',
		async (outcome) => {
			const { deadlines } = mockDeadlines();
			const saved = savedGlobalFixture('2026-10-25T00:09:00Z');
			const committed = userLanguageFixture(saved.id, 'it', saved.created_at);
			const committedInput = {
				scope: 'user_language',
				user_language: 'it',
				rule: { track: { languages: ['it'] } }
			};
			const committedLabel = `${committed.scope} · ${committed.id}`;
			const firstPage = pageResult([highPrecedenceRule], 'cursor-2');
			const getSpy = vi
				.spyOn(api, 'GET')
				.mockResolvedValueOnce(pageResult([highPrecedenceRule, storedRule]) as never)
				.mockResolvedValueOnce(firstPage as never)
				.mockResolvedValueOnce(firstPage as never);
			if (outcome === 'absent') {
				getSpy.mockRejectedValueOnce(problemError(404, 'Not Found', 'rule deleted'));
			} else if (outcome === 'transport failure') {
				getSpy.mockRejectedValueOnce(new TypeError('detail connection lost'));
			} else {
				getSpy.mockResolvedValueOnce({
					data: outcome === 'found' ? committed : undefined,
					response: new Response(null, { status: 200 })
				} as never);
			}
			const putSpy = vi
				.spyOn(api, 'PUT')
				.mockResolvedValueOnce({ data: saved } as never)
				.mockImplementationOnce(hangUntilAborted as never);
			const postSpy = vi.spyOn(api, 'POST');

			render(LibraryProcessingRules);
			await openRuleAsJson(storedRule.id);
			await fireEvent.change(ruleSelect(), { target: { value: highPrecedenceRule.id } });
			await typeDocument(editedDocument);
			await fireEvent.click(saveButton());
			await screen.findByRole('button', { name: m.modules_rules_load_more() });
			await waitFor(() => expect((saveButton() as HTMLButtonElement).disabled).toBe(false));
			expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id, saved.id]);

			await typeDocument(JSON.stringify(committedInput, null, 2));
			await fireEvent.click(saveButton());
			const deadline = deadlines[3]!;
			expect(putSpy).toHaveBeenNthCalledWith(2, RULE_PATH, {
				params: { path: { ruleId: saved.id } },
				body: committedInput,
				signal: deadline.signal
			});
			expireDeadline(deadline);
			await screen.findByText(m.modules_rules_write_timeout());
			expect(getSpy).toHaveBeenCalledTimes(2);
			await typeDocument(jsonDocument);

			await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
			await screen.findByLabelText(m.modules_rules_select_label());
			expect(getSpy).toHaveBeenNthCalledWith(4, RULE_DETAIL_PATH, {
				params: { path: { ruleId: saved.id } },
				signal: expect.anything()
			});
			expect(documentField().value).toBe(jsonDocument);
			expect(ruleSelect().value).toBe(highPrecedenceRule.id);
			expect((saveButton() as HTMLButtonElement).disabled).toBe(false);
			expect(screen.getByRole('button', { name: m.modules_rules_load_more() })).toBeTruthy();
			expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy();
			expect(screen.getByRole('status').textContent).toBe('');

			if (outcome === 'found') {
				const preview = screen.getByRole('textbox', {
					name: `${m.modules_rules_check_state()} · ${committedLabel} (JSON)`
				}) as HTMLTextAreaElement;
				expect(preview.readOnly).toBe(true);
				expect(readRuleDocument(preview.value, 'json')).toEqual(committedInput);
				expect(screen.getByRole('option', { name: committedLabel })).toBeTruthy();
				expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
				await fireEvent.change(ruleSelect(), { target: { value: saved.id } });
				await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
				expect(readRuleDocument(documentField().value, 'json')).toEqual(committedInput);
			} else {
				expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
				if (outcome === 'absent') {
					expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id]);
					expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
					expect(screen.queryByText(m.modules_rules_editing({ rule: optionLabel }))).toBeNull();
					expect(screen.getByText(m.modules_rules_editing({ rule: saved.id }))).toBeTruthy();
					expect(screen.queryByRole('button', { name: m.modules_rules_retry() })).toBeNull();
				} else {
					expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id, saved.id]);
					expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
					expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();
					expect(screen.getByRole('button', { name: m.modules_rules_retry() })).toBeTruthy();
				}
			}
			expect(putSpy).toHaveBeenCalledTimes(2);
			expect(postSpy).not.toHaveBeenCalled();
			expect(getSpy).toHaveBeenCalledTimes(4);
		}
	);

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
	it.each([
		['POST', 'success'],
		['POST', 'failure'],
		['PUT', 'success'],
		['PUT', 'failure']
	] as const)(
		'suppresses toasts and refresh when a late %s settles with %s after unmount',
		async (method, outcome) => {
			const { deadlines } = mockDeadlines();
			const write = deferred<unknown>();
			const getSpy = vi
				.spyOn(api, 'GET')
				.mockResolvedValue(pageResult(method === 'POST' ? [] : [storedRule]) as never);
			const successToast = vi.spyOn(toast, 'success');
			const errorToast = vi.spyOn(toast, 'error');
			const { unmount } = render(LibraryProcessingRules);

			if (method === 'POST') {
				const postSpy = vi.spyOn(api, 'POST').mockReturnValueOnce(write.promise as never);
				await screen.findByText(m.modules_rules_empty());
				await chooseFormat('JSON');
				await typeDocument(jsonDocument);
				await fireEvent.click(importButton());
				expect(postSpy).toHaveBeenCalledExactlyOnceWith(RULES_PATH, {
					body: ruleInput,
					signal: deadlines[1]!.signal
				});
			} else {
				const putSpy = vi.spyOn(api, 'PUT').mockReturnValueOnce(write.promise as never);
				await openRuleAsJson(storedRule.id);
				await typeDocument(editedDocument);
				await fireEvent.click(saveButton());
				expect(putSpy).toHaveBeenCalledExactlyOnceWith(RULE_PATH, {
					params: { path: { ruleId: storedRule.id } },
					body: editedInput,
					signal: deadlines[1]!.signal
				});
			}

			unmount();
			expect(deadlines[1]!.signal.aborted).toBe(false);
			if (outcome === 'success') {
				write.resolve({
					data: method === 'POST' ? storedRule : savedGlobalFixture('2026-10-25T00:13:00Z')
				});
			} else {
				write.reject(problemError(503, 'Service Unavailable', 'late write failure'));
			}
			await flush();

			expect(successToast).not.toHaveBeenCalled();
			expect(errorToast).not.toHaveBeenCalled();
			expect(getSpy).toHaveBeenCalledTimes(1);
		}
	);

	it('keeps the draft when the off-page detail check times out and shows stored languages read-only after Retry', async () => {
		const { deadlines, timeoutSpy } = mockDeadlines();
		const firstPage = pageResult([highPrecedenceRule], 'cursor-2');
		const checkedRule = userLanguageFixture(storedRule.id, 'it', storedRule.created_at);
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(firstPage as never)
			.mockResolvedValueOnce(pageResult([storedRule]) as never)
			.mockResolvedValueOnce(firstPage as never)
			.mockImplementationOnce(hangUntilAborted as never)
			.mockResolvedValueOnce(firstPage as never)
			.mockResolvedValueOnce({ data: checkedRule } as never);
		const putSpy = vi.spyOn(api, 'PUT').mockImplementationOnce(hangUntilAborted as never);
		const postSpy = vi.spyOn(api, 'POST');
		const successToast = vi.spyOn(toast, 'success');
		const checkedLabel = `${m.modules_rules_check_state()} · ${checkedRule.scope} · ${checkedRule.id} (JSON)`;
		const detailParams = { path: { ruleId: storedRule.id } };

		render(LibraryProcessingRules);
		await fireEvent.click(await screen.findByRole('button', { name: m.modules_rules_load_more() }));
		await screen.findByRole('option', { name: optionLabel });
		await openRuleAsJson(storedRule.id);
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());
		expireDeadline(deadlines[2]!);
		expect((await screen.findByRole('alert')).textContent).toContain(
			m.modules_rules_write_timeout()
		);

		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
		await waitFor(() =>
			expect(getSpy).toHaveBeenNthCalledWith(4, RULE_DETAIL_PATH, {
				params: detailParams,
				signal: expect.anything()
			})
		);
		expect(timeoutSpy).toHaveBeenNthCalledWith(5, REQUEST_TIMEOUT_MS);
		expireDeadline(deadlines[4]!);

		const retry = await screen.findByRole('button', { name: m.modules_rules_retry() });
		expect(screen.getAllByRole('alert')).toHaveLength(2);
		expect(screen.queryByText(m.loading())).toBeNull();
		expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
		expect(documentField().value).toBe(editedDocument);
		expect(ruleSelect().value).toBe(storedRule.id);
		expect(getSpy).toHaveBeenCalledTimes(4);

		await fireEvent.click(retry);
		const preview = (await screen.findByRole('textbox', {
			name: checkedLabel
		})) as HTMLTextAreaElement;
		expect(getSpy).toHaveBeenCalledTimes(6);
		expect(getSpy).toHaveBeenNthCalledWith(5, RULES_PAGE_PATH, {
			params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } },
			signal: expect.anything()
		});
		expect(getSpy).toHaveBeenNthCalledWith(6, RULE_DETAIL_PATH, {
			params: detailParams,
			signal: expect.objectContaining({ aborted: false })
		});
		expect(preview.readOnly).toBe(true);
		expect(readRuleDocument(preview.value, 'json').rule.track?.languages).toEqual(['it']);
		expect(documentField().value).toBe(editedDocument);
		expect(ruleSelect().value).toBe(storedRule.id);
		expect(screen.queryByRole('button', { name: m.modules_rules_retry() })).toBeNull();
		expect(screen.getByRole('alert').textContent).toContain(m.modules_rules_write_timeout());
		expect(screen.getByRole('status').textContent).toBe('');
		expect(successToast).not.toHaveBeenCalled();
		expect(putSpy).toHaveBeenCalledTimes(1);
		expect(postSpy).not.toHaveBeenCalled();
	});
	it.each(['on-page', 'off-page'] as const)(
		'previews the dispatched PUT target (%s), not the current selection',
		async (location) => {
			const { deadlines, timeoutSpy } = mockDeadlines();
			const checked = userLanguageFixture(storedRule.id, 'it', storedRule.created_at);
			const getSpy = vi
				.spyOn(api, 'GET')
				.mockResolvedValueOnce(
					pageResult([highPrecedenceRule, secondPageRule, storedRule]) as never
				)
				.mockResolvedValueOnce(
					(location === 'on-page'
						? pageResult([highPrecedenceRule, secondPageRule, checked])
						: pageResult([highPrecedenceRule, secondPageRule], 'cursor-2')) as never
				)
				.mockResolvedValueOnce({ data: checked } as never);
			const putSpy = vi
				.spyOn(api, 'PUT')
				.mockImplementationOnce(hangUntilAborted as never)
				.mockRejectedValueOnce(problemError(503, 'Service Unavailable', 'second save rejected'));
			const successToast = vi.spyOn(toast, 'success');

			render(LibraryProcessingRules);
			await openRuleAsJson(storedRule.id);
			await typeDocument(editedDocument);
			await fireEvent.click(saveButton());
			expect(putSpy).toHaveBeenCalledExactlyOnceWith(RULE_PATH, {
				params: { path: { ruleId: storedRule.id } },
				body: editedInput,
				signal: deadlines[1]!.signal
			});
			await fireEvent.change(ruleSelect(), { target: { value: highPrecedenceRule.id } });
			expireDeadline(deadlines[1]!);
			await screen.findByText(m.modules_rules_write_timeout());
			expect(getSpy).toHaveBeenCalledTimes(1);

			await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
			const previewName = `${m.modules_rules_check_state()} · ${checked.scope} · ${checked.id} (JSON)`;
			const preview = (await screen.findByRole('textbox', {
				name: previewName
			})) as HTMLTextAreaElement;
			expect(preview.readOnly).toBe(true);
			expect(readRuleDocument(preview.value, 'json').rule.track?.languages).toEqual(['it']);
			expect(screen.getAllByRole('textbox')).toHaveLength(2);
			expect(getSpy.mock.calls[1]?.[0]).toBe(RULES_PAGE_PATH);
			expect(getSpy).toHaveBeenCalledTimes(location === 'on-page' ? 2 : 3);
			if (location === 'off-page') {
				expect(getSpy).toHaveBeenNthCalledWith(3, RULE_DETAIL_PATH, {
					params: { path: { ruleId: storedRule.id } },
					signal: expect.objectContaining({ aborted: false })
				});
				expect(timeoutSpy).toHaveBeenNthCalledWith(4, REQUEST_TIMEOUT_MS);
			}
			expect(ruleSelect().value).toBe(highPrecedenceRule.id);
			await fireEvent.change(ruleSelect(), { target: { value: secondPageRule.id } });
			expect(screen.getByRole('textbox', { name: previewName })).toBe(preview);
			expect(documentField().value).toBe(editedDocument);
			expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy();
			expect(screen.getByRole('status').textContent).toBe('');
			expect(successToast).not.toHaveBeenCalled();
			expect(putSpy).toHaveBeenCalledTimes(1);

			await fireEvent.click(saveButton());
			await screen.findByText(/second save rejected/);
			expect(putSpy).toHaveBeenCalledTimes(2);
			expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
			expect(screen.queryByText(m.modules_rules_write_timeout())).toBeNull();
			expect(documentField().value).toBe(editedDocument);
			expect(ruleSelect().value).toBe(secondPageRule.id);
		}
	);

	it('refreshes an unknown POST without previewing the selected existing rule or a matching document', async () => {
		const { deadlines } = mockDeadlines();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule]) as never)
			.mockResolvedValueOnce(pageResult([highPrecedenceRule, storedRule]) as never);
		const postSpy = vi.spyOn(api, 'POST').mockImplementationOnce(hangUntilAborted as never);
		const putSpy = vi.spyOn(api, 'PUT');
		const successToast = vi.spyOn(toast, 'success');

		render(LibraryProcessingRules);
		await openRuleAsJson(highPrecedenceRule.id);
		await typeDocument(jsonDocument);
		await fireEvent.click(importButton());
		expireDeadline(deadlines[1]!);
		await screen.findByText(m.modules_rules_write_timeout());
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
		await screen.findByRole('option', { name: optionLabel });

		expect(getSpy).toHaveBeenCalledTimes(2);
		expect(getSpy).toHaveBeenNthCalledWith(1, RULES_PAGE_PATH, expect.anything());
		expect(getSpy).toHaveBeenNthCalledWith(2, RULES_PAGE_PATH, expect.anything());
		expect(ruleSelect().value).toBe(highPrecedenceRule.id);
		expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
		await fireEvent.change(ruleSelect(), { target: { value: storedRule.id } });
		expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
		expect(documentField().value).toBe(jsonDocument);
		expect(screen.getByRole('alert').textContent).toContain(m.modules_rules_write_timeout());
		expect(screen.getByRole('status').textContent).toBe('');
		expect(successToast).not.toHaveBeenCalled();
		expect(postSpy).toHaveBeenCalledTimes(1);
		expect(putSpy).not.toHaveBeenCalled();
	});

	it('discards an old recovery detail even if it resolves after a new valid save starts', async () => {
		const { deadlines } = mockDeadlines();
		const oldDetail = deferred<unknown>();
		const nextWrite = deferred<unknown>();
		const getSpy = vi
			.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule, storedRule], 'before') as never)
			.mockResolvedValueOnce(pageResult([highPrecedenceRule, secondPageRule], 'stale') as never)
			.mockReturnValueOnce(oldDetail.promise as never); // Deliberately ignores abort.
		const putSpy = vi
			.spyOn(api, 'PUT')
			.mockImplementationOnce(hangUntilAborted as never)
			.mockReturnValueOnce(nextWrite.promise as never);

		render(LibraryProcessingRules);
		await openRuleAsJson(storedRule.id);
		await typeDocument(editedDocument);
		await fireEvent.click(saveButton());
		expireDeadline(deadlines[1]!);
		await screen.findByText(m.modules_rules_write_timeout());
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
		await waitFor(() =>
			expect(getSpy).toHaveBeenNthCalledWith(3, RULE_DETAIL_PATH, {
				params: { path: { ruleId: storedRule.id } },
				signal: expect.objectContaining({ aborted: false })
			})
		);
		expect(screen.getByText(m.loading())).toBeTruthy();

		await fireEvent.click(saveButton());
		expect(putSpy).toHaveBeenCalledTimes(2);
		expect(screen.queryByText(m.loading())).toBeNull();
		expect(screen.queryByText(m.modules_rules_write_timeout())).toBeNull();
		oldDetail.resolve({ data: userLanguageFixture(storedRule.id, 'it', storedRule.created_at) });
		await flush();
		expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id, storedRule.id]);
		expect(ruleSelect().value).toBe(storedRule.id);
		expect(screen.getAllByRole('textbox')).toEqual([documentField()]);

		// No successful-write refresh can mask a stale update or a stuck loading flag.
		nextWrite.reject(problemError(503, 'Service Unavailable', 'new save rejected'));
		await screen.findByText(/new save rejected/);
		expect(getSpy).toHaveBeenCalledTimes(3);
		expect(screen.queryByText(m.loading())).toBeNull();
		expect(
			(screen.getByRole('button', { name: m.modules_rules_load_more() }) as HTMLButtonElement)
				.disabled
		).toBe(false);
		expect(documentField().value).toBe(editedDocument);
	});
	it.each(['POST', 'PUT'] as const)(
		'restarts the list when an explicit %s retry is rejected before any list snapshot',
		async (method) => {
			const { deadlines } = mockDeadlines();
			const getSpy = vi.spyOn(api, 'GET').mockImplementationOnce(hangUntilAborted as never);
			if (method === 'PUT') {
				getSpy.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'import refresh failed'));
				vi.spyOn(api, 'POST').mockResolvedValueOnce({ data: storedRule } as never);
			}
			getSpy
				.mockRejectedValueOnce(problemError(503, 'Service Unavailable', 'list restart failed'))
				.mockImplementationOnce(pageHonoringSignal([storedRule]) as never);
			const writeSpy = vi
				.spyOn(api, method)
				.mockImplementationOnce(hangUntilAborted as never)
				.mockRejectedValueOnce(problemError(409, 'Conflict', 'write retry rejected'));

			render(LibraryProcessingRules);
			await screen.findByText(m.loading());
			await chooseFormat('JSON');
			await typeDocument(jsonDocument);
			if (method === 'PUT') {
				// A successful import can leave an exportable rule without a list snapshot.
				await fireEvent.click(importButton());
				await screen.findByText(/import refresh failed/);
				await openRuleAsJson(storedRule.id);
				await typeDocument(editedDocument);
			}
			const writeButton = method === 'POST' ? importButton : saveButton;
			const draft = method === 'POST' ? jsonDocument : editedDocument;
			const readsBeforeWrite = method === 'POST' ? 1 : 2;
			const deadlineIndex = deadlines.length;
			await fireEvent.click(writeButton());
			expireDeadline(deadlines[deadlineIndex]!);
			await screen.findByRole('button', { name: m.modules_rules_check_state() });
			await flush();
			expect(writeSpy).toHaveBeenCalledTimes(1);
			expect(getSpy).toHaveBeenCalledTimes(readsBeforeWrite);
			expect(documentField().value).toBe(draft);

			await fireEvent.click(writeButton());
			await screen.findByText(/write retry rejected/);
			await screen.findByText(/list restart failed/);
			expect(writeSpy).toHaveBeenCalledTimes(2);
			expect(getSpy).toHaveBeenCalledTimes(readsBeforeWrite + 1);
			expect(getSpy).toHaveBeenLastCalledWith(RULES_PAGE_PATH, {
				params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } },
				signal: expect.objectContaining({ aborted: false })
			});
			expect(screen.queryByRole('button', { name: m.modules_rules_check_state() })).toBeNull();

			await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
			await waitFor(() => expect(screen.queryByText(/list restart failed/)).toBeNull());
			await screen.findByRole('option', { name: optionLabel });
			expect(getSpy).toHaveBeenCalledTimes(readsBeforeWrite + 2);
			expect(writeSpy).toHaveBeenCalledTimes(2);
			expect(documentField().value).toBe(draft);
			expect(ruleSelect().value).toBe(method === 'POST' ? '' : storedRule.id);
		}
	);
	it.each(['rejected', 'timed out', 'rejected after unmount'] as const)(
		'handles an import %s after it interrupts the initial list load',
		async (outcome) => {
			const { deadlines } = mockDeadlines();
			const initialPage = deferred<unknown>();
			const restartedPage = deferred<unknown>();
			const write = deferred<unknown>();
			const getSpy = vi
				.spyOn(api, 'GET')
				.mockReturnValueOnce(initialPage.promise as never) // Deliberately ignores abort.
				.mockReturnValueOnce(restartedPage.promise as never);
			const postSpy = vi.spyOn(api, 'POST').mockReturnValueOnce(write.promise as never);
			const errorToast = vi.spyOn(toast, 'error');
			const { unmount } = render(LibraryProcessingRules);

			await screen.findByText(m.loading());
			await chooseFormat('JSON');
			await typeDocument(jsonDocument);
			await fireEvent.click(importButton());
			expect(postSpy).toHaveBeenCalledExactlyOnceWith(RULES_PATH, {
				body: ruleInput,
				signal: deadlines[1]!.signal
			});
			expect(getSpy).toHaveBeenCalledExactlyOnceWith(RULES_PAGE_PATH, {
				params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } },
				signal: expect.objectContaining({ aborted: true })
			});
			expect(screen.queryByText(m.modules_rules_empty())).toBeNull();

			if (outcome === 'rejected after unmount') unmount();
			if (outcome === 'timed out') {
				const deadline = deadlines[1]!;
				expireDeadline(deadline);
				write.reject(deadline.signal.reason);
			} else {
				write.reject(problemError(409, 'Conflict', 'import rejected'));
			}
			await flush();
			initialPage.resolve(pageResult([]));
			await flush();

			if (outcome === 'rejected') {
				expect(screen.getByText(/import rejected/)).toBeTruthy();
				expect(screen.getByText(m.loading())).toBeTruthy();
				expect(getSpy).toHaveBeenCalledTimes(2);
				expect(getSpy).toHaveBeenNthCalledWith(2, RULES_PAGE_PATH, {
					params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } },
					signal: expect.objectContaining({ aborted: false })
				});
				restartedPage.resolve(pageResult([storedRule]));
				await screen.findByRole('option', { name: optionLabel });
				expect(getSpy).toHaveBeenCalledTimes(2);
				expect(documentField().value).toBe(jsonDocument);
			} else {
				expect(getSpy).toHaveBeenCalledTimes(1);
				if (outcome === 'timed out') {
					expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy();
					expect(screen.queryByText(m.modules_rules_empty())).toBeNull();
				} else {
					expect(errorToast).not.toHaveBeenCalled();
				}
			}
			expect(postSpy).toHaveBeenCalledTimes(1);
		}
	);
});
