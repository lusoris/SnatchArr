// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { ProblemError } from '@sveltesentio/core';
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
	RULE_DETAIL_PATH,
	ruleInput,
	yamlDocument,
	storedRule,
	highPrecedenceRule,
	secondPageRule,
	optionLabel,
	pageResult,
	documentField,
	typeDocument,
	chooseFormat,
	importButton,
	problemError,
	ruleSelect,
	saveButton,
	selectOptionValues,
	userLanguageFixture
} from './library-processing-rules-test-helpers.js';

describe('library processing rules pagination', () => {
	afterEach(() => {
		vi.restoreAllMocks();
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

	it('replaces a loaded rule with the later-page row for the same ID, reordered, and saves the fresh content', async () => {
		const moved = userLanguageFixture(storedRule.id, 'fr', storedRule.created_at);
		const movedLabel = `${moved.scope} · ${moved.id}`;
		const movedInput = {
			scope: 'user_language',
			user_language: 'fr',
			rule: { track: { languages: ['fr'] } }
		};
		vi.spyOn(api, 'GET')
			.mockResolvedValueOnce(pageResult([highPrecedenceRule, storedRule], 'cursor-2') as never)
			.mockResolvedValueOnce(pageResult([moved]) as never)
			.mockResolvedValueOnce(pageResult([highPrecedenceRule, moved]) as never);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValueOnce({ data: moved } as never);

		render(LibraryProcessingRules);
		await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_load_more() }));
		await waitFor(() => expect(screen.getByRole('option', { name: movedLabel })).toBeTruthy());

		expect(screen.queryByRole('option', { name: optionLabel })).toBeNull();
		expect(selectOptionValues()).toEqual(['', highPrecedenceRule.id, storedRule.id]);
		await fireEvent.change(ruleSelect(), { target: { value: storedRule.id } });
		await chooseFormat('JSON');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		expect(readRuleDocument(documentField().value, 'json')).toEqual(movedInput);
		expect(screen.getByText(m.modules_rules_editing({ rule: movedLabel }))).toBeTruthy();

		await fireEvent.click(saveButton());
		await waitFor(() => expect(putSpy).toHaveBeenCalledTimes(1));
		expect(putSpy.mock.calls[0]?.[1]).toMatchObject({ body: movedInput });
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
});
