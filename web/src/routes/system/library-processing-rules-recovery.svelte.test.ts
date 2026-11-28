// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { readRuleDocument } from '#lib/library-processing-document.js';
import { m } from '#lib/paraglide/messages.js';
import LibraryProcessingRules from './LibraryProcessingRules.svelte';
import {
	type StoredRule,
	RULE_DETAIL_PATH,
	RULE_PATH,
	RULES_PAGE_PATH,
	RULES_PATH,
	documentField,
	editedDocument,
	expireDeadline,
	hangUntilAborted,
	highPrecedenceRule,
	importButton,
	itemOverrideFixture,
	jsonDocument,
	mockDeadlines,
	otherRule,
	openRuleAsJson,
	optionLabel,
	pageResult,
	problemError,
	ruleInput,
	ruleSelect,
	saveButton,
	savedGlobalFixture,
	secondPageRule,
	selectOptionValues,
	storedRule,
	typeDocument,
	userLanguageFixture
} from './library-processing-rules-test-helpers.js';

const saved = savedGlobalFixture('2026-10-25T00:09:00Z');
const committed = userLanguageFixture(saved.id, 'it', saved.created_at);
const committedInput = {
	scope: 'user_language',
	user_language: 'it',
	rule: { track: { languages: ['it'] } }
};
const committedLabel = `${committed.scope} · ${committed.id}`;
const firstPage = pageResult([highPrecedenceRule], 'cursor-2');

afterEach(() => {
	vi.restoreAllMocks();
});

type Detail = { found: StoredRule } | { rejected: unknown } | { empty: true };

function saveDisabled(): boolean {
	return (saveButton() as HTMLButtonElement).disabled;
}

function queueGets(detail: Detail) {
	const getSpy = vi
		.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult([highPrecedenceRule, storedRule]) as never)
		.mockResolvedValueOnce(firstPage as never)
		.mockResolvedValueOnce(firstPage as never);
	if ('rejected' in detail) return getSpy.mockRejectedValueOnce(detail.rejected);
	const data = 'found' in detail ? detail.found : undefined;
	return getSpy.mockResolvedValueOnce({
		data,
		response: new Response(null, { status: 200 })
	} as never);
}

// Save once so the rule is retained off-page, then time out a second PUT to that same ID.
async function timeOutOffPagePut(detail: Detail) {
	const { deadlines } = mockDeadlines();
	const getSpy = queueGets(detail);
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
	await waitFor(() => expect(saveDisabled()).toBe(false));
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
	return { deadlines, getSpy, putSpy, postSpy };
}

async function checkStoredRules(): Promise<void> {
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));
	await screen.findByLabelText(m.modules_rules_select_label());
}

// What every outcome must preserve: the draft, the selection, the timeout warning, no write.
function expectRecoveryKeepsState(spies: Awaited<ReturnType<typeof timeOutOffPagePut>>): void {
	expect(spies.getSpy).toHaveBeenNthCalledWith(4, RULE_DETAIL_PATH, {
		params: { path: { ruleId: saved.id } },
		signal: expect.anything()
	});
	expect(documentField().value).toBe(jsonDocument);
	expect(ruleSelect().value).toBe(highPrecedenceRule.id);
	expect(screen.getByRole('button', { name: m.modules_rules_load_more() })).toBeTruthy();
	expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy();
	expect(screen.getByRole('status').textContent).toBe('');
	expect(spies.putSpy).toHaveBeenCalledTimes(2);
	expect(spies.postSpy).not.toHaveBeenCalled();
	expect(spies.getSpy).toHaveBeenCalledTimes(4);
}

function expectNoConfirmedMissing(): void {
	expect(screen.queryByText(m.modules_rules_write_missing({ id: saved.id }))).toBeNull();
	expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
	expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id, saved.id]);
	expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
	expect(screen.getByText(m.modules_rules_editing({ rule: optionLabel }))).toBeTruthy();
	expect(screen.getByRole('button', { name: m.modules_rules_retry() })).toBeTruthy();
	expect(saveDisabled()).toBe(false);
}

it('found: previews the committed rule read-only and keeps Save enabled', async () => {
	const spies = await timeOutOffPagePut({ found: committed });
	await checkStoredRules();
	expectRecoveryKeepsState(spies);

	const preview = screen.getByRole('textbox', {
		name: `${m.modules_rules_check_state()} · ${committedLabel} (JSON)`
	}) as HTMLTextAreaElement;
	expect(preview.readOnly).toBe(true);
	expect(readRuleDocument(preview.value, 'json')).toEqual(committedInput);
	expect(screen.getByRole('option', { name: committedLabel })).toBeTruthy();
	expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
	expect(screen.queryByText(m.modules_rules_write_missing({ id: saved.id }))).toBeNull();
	expect(saveDisabled()).toBe(false);
	await fireEvent.change(ruleSelect(), { target: { value: saved.id } });
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
	expect(readRuleDocument(documentField().value, 'json')).toEqual(committedInput);
});

it('missing: names the ID, keeps the draft, allows Import and disables Save for that ID', async () => {
	const spies = await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	expectRecoveryKeepsState(spies);

	expect(screen.getByText(m.modules_rules_write_missing({ id: saved.id }))).toBeTruthy();
	expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
	expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id]);
	expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
	expect(screen.getByText(m.modules_rules_editing({ rule: saved.id }))).toBeTruthy();
	expect(screen.queryByRole('button', { name: m.modules_rules_retry() })).toBeNull();
	expect(saveDisabled()).toBe(true);
	expect((importButton() as HTMLButtonElement).disabled).toBe(false);
	await fireEvent.click(saveButton());
	expect(spies.putSpy).toHaveBeenCalledTimes(2);
});

it('empty 200 body: is not a confirmed missing rule and keeps Save enabled', async () => {
	const spies = await timeOutOffPagePut({ empty: true });
	await checkStoredRules();
	expectRecoveryKeepsState(spies);
	expectNoConfirmedMissing();
});

it('transport failure: is not a confirmed missing rule and keeps Save enabled', async () => {
	const spies = await timeOutOffPagePut({ rejected: new TypeError('detail connection lost') });
	await checkStoredRules();
	expectRecoveryKeepsState(spies);
	expectNoConfirmedMissing();
});

it('restores Save when a later check finds the rule again', async () => {
	const spies = await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	expect(saveDisabled()).toBe(true);

	spies.getSpy
		.mockResolvedValueOnce(firstPage as never)
		.mockResolvedValueOnce({ data: committed } as never);
	await checkStoredRules();

	await waitFor(() => expect(saveDisabled()).toBe(false));
	expect(screen.queryByText(m.modules_rules_write_missing({ id: saved.id }))).toBeNull();
});

it('keeps Save disabled for the missing ID across an unrelated Load more', async () => {
	const spies = await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	expect(saveDisabled()).toBe(true);

	spies.getSpy.mockResolvedValueOnce(pageResult([secondPageRule]) as never);
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
	await waitFor(() =>
		expect(screen.queryByRole('button', { name: m.modules_rules_load_more() })).toBeNull()
	);

	expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id, secondPageRule.id]);
	expect(screen.getByText(m.modules_rules_write_missing({ id: saved.id }))).toBeTruthy();
	expect(saveDisabled()).toBe(true);
});

it('missing: Export of another rule moves the editor and drops the notice', async () => {
	await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	expect(screen.getByText(m.modules_rules_write_missing({ id: saved.id }))).toBeTruthy();

	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));

	expect(screen.queryByText(m.modules_rules_write_missing({ id: saved.id }))).toBeNull();
	const highLabel = `${highPrecedenceRule.scope} · ${highPrecedenceRule.id}`;
	expect(screen.getByText(m.modules_rules_editing({ rule: highLabel }))).toBeTruthy();
	expect(saveDisabled()).toBe(false);
});

it('missing: a successful Import keeps the draft until sent, then drops the notice and Save target', async () => {
	const spies = await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	spies.postSpy.mockResolvedValueOnce({ data: otherRule } as never);
	spies.getSpy.mockResolvedValueOnce(firstPage as never);
	expect(documentField().value).toBe(jsonDocument);

	await fireEvent.click(importButton());

	await waitFor(() =>
		expect(screen.getByRole('status').textContent).toBe(m.modules_rules_import_success())
	);
	expect(spies.postSpy).toHaveBeenCalledWith(RULES_PATH, {
		body: ruleInput,
		signal: expect.anything()
	});
	expect(screen.queryByText(m.modules_rules_write_missing({ id: saved.id }))).toBeNull();
	expect(screen.queryByRole('button', { name: m.modules_rules_check_state() })).toBeNull();
	expect(saveDisabled()).toBe(true);
	// Import clears the edited ID, so no extra target read follows it.
	expect(spies.getSpy).toHaveBeenCalledTimes(5);
});

it('missing: a timed-out Import stays an unknown POST with a list-only check and no preview', async () => {
	const spies = await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	spies.postSpy.mockImplementationOnce(hangUntilAborted as never);
	await fireEvent.click(importButton());
	expireDeadline(spies.deadlines[spies.deadlines.length - 1]!);
	await waitFor(() => expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy());

	spies.getSpy.mockResolvedValueOnce(firstPage as never);
	await checkStoredRules();

	expect(spies.getSpy).toHaveBeenNthCalledWith(5, RULES_PAGE_PATH, expect.anything());
	expect(spies.getSpy).toHaveBeenCalledTimes(5);
	expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
	expect(spies.putSpy).toHaveBeenCalledTimes(2);
	expect(spies.postSpy).toHaveBeenCalledTimes(1);
});

it('missing: after a rejected Import, Check stays available and restores Save once the rule is found', async () => {
	const spies = await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	spies.postSpy.mockRejectedValueOnce(problemError(422, 'Unprocessable', 'import rejected'));

	await fireEvent.click(importButton());

	await waitFor(() => expect(screen.getByText(/import rejected/)).toBeTruthy());
	expect(screen.queryByText(m.modules_rules_write_timeout())).toBeNull();
	expect(screen.getByText(m.modules_rules_write_missing({ id: saved.id }))).toBeTruthy();
	expect(saveDisabled()).toBe(true);

	spies.getSpy
		.mockResolvedValueOnce(firstPage as never)
		.mockResolvedValueOnce({ data: committed } as never);
	await checkStoredRules();

	await waitFor(() => expect(saveDisabled()).toBe(false));
	expect(screen.queryByText(m.modules_rules_write_missing({ id: saved.id }))).toBeNull();
	expect(spies.getSpy).toHaveBeenNthCalledWith(6, RULE_DETAIL_PATH, {
		params: { path: { ruleId: saved.id } },
		signal: expect.anything()
	});
});

it('adopts the Load more row for the saved rule so a first-page retry keeps its current scope', async () => {
	const firstRows = [highPrecedenceRule, secondPageRule];
	const movedInput = {
		scope: 'user_language',
		user_language: 'fr',
		rule: { track: { languages: ['fr'] } }
	};
	const movedRule = userLanguageFixture(storedRule.id, 'fr', storedRule.created_at);
	// Another writer moved the same rule to a different scope after the failed refresh.
	const incoming = itemOverrideFixture('ep-9');
	const incomingLabel = `${incoming.scope} · ${incoming.id}`;
	const staleDuplicate = savedGlobalFixture('2026-10-25T00:01:00Z');
	vi.spyOn(api, 'GET')
		.mockResolvedValueOnce(pageResult(firstRows, 'cursor-2') as never)
		.mockResolvedValueOnce(pageResult([storedRule], 'cursor-3') as never)
		.mockResolvedValueOnce(pageResult(firstRows, 'cursor-2') as never)
		.mockRejectedValueOnce(problemError(502, 'Bad Gateway', 'refresh down after scope move'))
		// A repeated ID inside one page: the last row wins, so the earlier copy must never come back.
		.mockResolvedValueOnce(pageResult([staleDuplicate, incoming], 'cursor-4') as never)
		.mockResolvedValueOnce(pageResult(firstRows, 'cursor-2') as never);
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
	await waitFor(() => expect(saveDisabled()).toBe(false));
	await typeDocument(JSON.stringify(movedInput, null, 2));
	await fireEvent.click(saveButton());
	expect(await screen.findByText(/refresh down after scope move/)).toBeTruthy();

	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
	await waitFor(() => expect(screen.getByRole('option', { name: incomingLabel })).toBeTruthy());

	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_retry() }));
	await waitFor(() => expect(screen.queryByText(/refresh down after scope move/)).toBeNull());
	await screen.findByLabelText(m.modules_rules_select_label());
	expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id, secondPageRule.id]);
	expect(ruleSelect().value).toBe(highPrecedenceRule.id);
	expect(screen.getByText(m.modules_rules_editing({ rule: incomingLabel }))).toBeTruthy();
});

type DetailInit = { params?: { path?: { ruleId?: string } } };
type EditorReads = { complete?: boolean; aFails?: boolean; highExists: () => boolean };

// Page and detail answers by ID: A is the timed-out PUT target, B (high) is the edited rule.
function mockEditorReads(getSpy: ReturnType<typeof queueGets>, reads: EditorReads): string[] {
	const detailReads: string[] = [];
	getSpy.mockReset();
	getSpy.mockImplementation(((path: string, init?: DetailInit) => {
		if (path === RULES_PAGE_PATH) {
			const items = reads.highExists() && reads.complete ? [highPrecedenceRule] : [];
			return Promise.resolve(
				pageResult([...items, secondPageRule], reads.complete ? undefined : 'cursor-2')
			);
		}
		const id = init?.params?.path?.ruleId ?? '';
		detailReads.push(id);
		if (id === saved.id) {
			if (reads.aFails) return Promise.reject(new TypeError('detail connection lost'));
			return Promise.resolve({ data: committed });
		}
		if (reads.highExists()) return Promise.resolve({ data: highPrecedenceRule });
		return Promise.reject(problemError(404, 'Not Found', 'gone'));
	}) as never);
	return detailReads;
}

// Opens B (the high rule) as the editor while A's timed-out PUT stays unresolved.
async function exportHighBesideTimeout() {
	await fireEvent.change(ruleSelect(), { target: { value: highPrecedenceRule.id } });
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
	return readRuleDocument(documentField().value, 'json');
}

const highMissing = m.modules_rules_write_missing({ id: highPrecedenceRule.id });

it('confirms the current editor missing beside a timed-out PUT for another ID and restores Save when it returns', async () => {
	const spies = await timeOutOffPagePut({ found: committed });
	const exported = await exportHighBesideTimeout();
	let highExists = false;
	const detailReads = mockEditorReads(spies.getSpy, { highExists: () => highExists });

	await checkStoredRules();

	expect(screen.getByText(highMissing)).toBeTruthy();
	expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy();
	expect(saveDisabled()).toBe(true);
	// The timed-out target is still previewed; one detail read per ID despite two lookups each.
	expect(screen.getByRole('textbox', { name: new RegExp(committedLabel) })).toBeTruthy();
	expect([...detailReads].sort()).toEqual([saved.id, highPrecedenceRule.id].sort());
	await fireEvent.click(saveButton());
	expect(spies.putSpy).toHaveBeenCalledTimes(2);

	// The selection is cleared, yet Check still re-verifies the editor ID off-page.
	highExists = true;
	detailReads.length = 0;
	spies.putSpy.mockResolvedValueOnce({ data: highPrecedenceRule } as never);
	await checkStoredRules();

	await waitFor(() => expect(saveDisabled()).toBe(false));
	expect(screen.queryByText(highMissing)).toBeNull();
	expect([...detailReads].sort()).toEqual([saved.id, highPrecedenceRule.id].sort());
	await fireEvent.click(saveButton());
	expect(spies.putSpy).toHaveBeenNthCalledWith(3, RULE_PATH, {
		params: { path: { ruleId: highPrecedenceRule.id } },
		body: exported,
		signal: expect.anything()
	});
});

it('keeps the confirmed-missing editor when the unrelated timeout lookup then fails', async () => {
	const spies = await timeOutOffPagePut({ found: committed });
	await exportHighBesideTimeout();
	mockEditorReads(spies.getSpy, { aFails: true, highExists: () => false });

	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_check_state() }));

	expect(await screen.findByText(highMissing)).toBeTruthy();
	expect(screen.getByRole('button', { name: m.modules_rules_retry() })).toBeTruthy();
	expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy();
	expect(saveDisabled()).toBe(true);
	// B is gone from every retained copy; the other known rows stay and Retry is available.
	expect(selectOptionValues()).toEqual(['', saved.id]);
	expect(
		(screen.getByRole('button', { name: m.modules_rules_export() }) as HTMLButtonElement).disabled
	).toBe(true);
	await fireEvent.click(saveButton());
	expect(spies.putSpy).toHaveBeenCalledTimes(2);

	// Exporting another entry moves the editor and drops the alert; B is never restorable by export.
	await fireEvent.change(ruleSelect(), { target: { value: saved.id } });
	await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
	expect(screen.queryByText(highMissing)).toBeNull();
	expect(selectOptionValues()).not.toContain(highPrecedenceRule.id);
});

it('confirms the editor missing from a complete list without a detail read, then restores it', async () => {
	const spies = await timeOutOffPagePut({ found: committed });
	await exportHighBesideTimeout();
	let highExists = false;
	const detailReads = mockEditorReads(spies.getSpy, {
		complete: true,
		highExists: () => highExists
	});

	await checkStoredRules();
	expect(screen.getByText(highMissing)).toBeTruthy();
	expect(saveDisabled()).toBe(true);

	highExists = true;
	await checkStoredRules();
	await waitFor(() => expect(saveDisabled()).toBe(false));
	expect(screen.queryByText(highMissing)).toBeNull();
	expect(detailReads).toEqual([]);
});

it('missing: an unknown POST rechecked list-only restores Save when the page has the rule, without a preview', async () => {
	const spies = await timeOutOffPagePut({ rejected: problemError(404, 'Not Found', 'gone') });
	await checkStoredRules();
	spies.postSpy.mockImplementationOnce(hangUntilAborted as never);
	await fireEvent.click(importButton());
	expireDeadline(spies.deadlines[spies.deadlines.length - 1]!);
	await waitFor(() => expect(screen.getByText(m.modules_rules_write_timeout())).toBeTruthy());
	expect(saveDisabled()).toBe(true);

	spies.getSpy.mockResolvedValueOnce(pageResult([committed]) as never);
	await checkStoredRules();

	await waitFor(() => expect(saveDisabled()).toBe(false));
	expect(screen.queryByText(m.modules_rules_write_missing({ id: saved.id }))).toBeNull();
	expect(screen.getAllByRole('textbox')).toEqual([documentField()]);
	expect(spies.getSpy).toHaveBeenCalledTimes(5);
	expect(spies.putSpy).toHaveBeenCalledTimes(2);
});
