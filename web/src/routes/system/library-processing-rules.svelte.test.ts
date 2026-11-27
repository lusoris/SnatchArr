// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { toast } from 'svelte-sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { readRuleDocument } from '#lib/library-processing-document.js';
import { m } from '#lib/paraglide/messages.js';
import LibraryProcessingRules from './LibraryProcessingRules.svelte';

type StoredRule = components['schemas']['LibraryProcessingRule'];

const RULES_PATH = '/library-processing/rules';

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
	id: 'rule-1',
	created_at: '2026-10-25T00:00:00Z',
	updated_at: '2026-10-25T00:00:00Z'
} as unknown as StoredRule;

const optionLabel = `${storedRule.scope} · ${storedRule.id}`;

function documentField(): HTMLTextAreaElement {
	return screen.getByLabelText(m.modules_rules_document_label()) as HTMLTextAreaElement;
}

async function typeDocument(value: string): Promise<void> {
	await fireEvent.input(documentField(), { target: { value } });
}

async function chooseFormat(label: 'YAML' | 'JSON'): Promise<void> {
	await fireEvent.click(screen.getByRole('radio', { name: label }));
}

function importButton(): HTMLElement {
	return screen.getByRole('button', { name: m.modules_rules_import() });
}

describe('library processing rules import/export', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows the empty state and keeps export unavailable when no rules are stored', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue({ data: [] } as never);

		render(LibraryProcessingRules);

		expect(await screen.findByText(m.modules_rules_empty())).toBeTruthy();
		expect(screen.queryByRole('button', { name: m.modules_rules_export() })).toBeNull();
	});

	it('rejects an empty document without calling the API', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue({ data: [] } as never);
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
			// Guard the fixture itself: the real reader must really reject it.
			expect(() => readRuleDocument(broken, key)).toThrow();

			const getSpy = vi.spyOn(api, 'GET').mockResolvedValue({ data: [] } as never);
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
			// Operator input is preserved so it can be fixed.
			expect(documentField().value).toBe(broken);

			expect(postSpy).not.toHaveBeenCalled();
			expect(toastSuccessSpy).not.toHaveBeenCalled();
			// Only the initial load happened; no refresh after a rejected import.
			expect(getSpy).toHaveBeenCalledTimes(1);
		}
	);

	it.each([
		{ format: 'JSON' as const, document: jsonDocument },
		{ format: 'YAML' as const, document: yamlDocument }
	])(
		'imports a valid $format document with the exact payload, then refreshes the list',
		async ({ format, document }) => {
			const getSpy = vi
				.spyOn(api, 'GET')
				.mockResolvedValueOnce({ data: [] } as never)
				.mockResolvedValueOnce({ data: [storedRule] } as never);
			const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);
			const toastSuccessSpy = vi.spyOn(toast, 'success');

			render(LibraryProcessingRules);
			await screen.findByText(m.modules_rules_empty());

			await chooseFormat(format);
			await typeDocument(document);
			await fireEvent.click(importButton());

			// The refreshed list shows the new rule and it is preselected.
			const select = (await screen.findByLabelText(
				m.modules_rules_select_label()
			)) as HTMLSelectElement;
			expect(screen.getByRole('option', { name: optionLabel })).toBeTruthy();
			expect(select.value).toBe(storedRule.id);

			expect(postSpy).toHaveBeenCalledTimes(1);
			expect(postSpy).toHaveBeenCalledWith(RULES_PATH, { body: ruleInput });

			expect(getSpy).toHaveBeenCalledTimes(2);
			expect(getSpy.mock.calls[1]?.[0]).toBe(RULES_PATH);
			// Refresh strictly follows the successful POST.
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
		const getSpy = vi.spyOn(api, 'GET').mockResolvedValue({ data: [] } as never);
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
		// The typed document stays available for retry and the button is usable again.
		expect(documentField().value).toBe(jsonDocument);
		await waitFor(() => expect((importButton() as HTMLButtonElement).disabled).toBe(false));
	});

	it('keeps export disabled until a rule is selected', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue({ data: [storedRule] } as never);

		render(LibraryProcessingRules);

		const exportButton = (await screen.findByRole('button', {
			name: m.modules_rules_export()
		})) as HTMLButtonElement;
		expect(exportButton.disabled).toBe(true);

		const select = screen.getByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: storedRule.id } });

		expect(exportButton.disabled).toBe(false);
	});

	it('exports the selected rule as YAML', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue({ data: [storedRule] } as never);
		const postSpy = vi.spyOn(api, 'POST');

		render(LibraryProcessingRules);

		const select = await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: storedRule.id } });
		await chooseFormat('YAML');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));

		const exported = documentField().value;
		expect(exported.trim().length).toBeGreaterThan(0);
		expect(exported.trimStart().startsWith('{')).toBe(false);
		// The exported YAML is a document the real reader accepts and maps to the rule's writable fields.
		expect(readRuleDocument(exported, 'yaml')).toEqual(readRuleDocument(yamlDocument, 'yaml'));
		expect(screen.getByRole('status').textContent).toBe(m.modules_rules_export_success());
		expect(postSpy).not.toHaveBeenCalled();
	});

	it('exports the selected rule as JSON', async () => {
		vi.spyOn(api, 'GET').mockResolvedValue({ data: [storedRule] } as never);
		const postSpy = vi.spyOn(api, 'POST');

		render(LibraryProcessingRules);

		const select = await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: storedRule.id } });
		await chooseFormat('JSON');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));

		const exported = documentField().value;
		expect(() => JSON.parse(exported)).not.toThrow();
		expect(readRuleDocument(exported, 'json')).toEqual(readRuleDocument(jsonDocument, 'json'));
		expect(screen.getByRole('status').textContent).toBe(m.modules_rules_export_success());
		expect(postSpy).not.toHaveBeenCalled();
	});

	it('round-trips an exported rule back through import with the same payload', async () => {
		vi.spyOn(api, 'GET')
			.mockResolvedValueOnce({ data: [storedRule] } as never)
			.mockResolvedValueOnce({ data: [storedRule] } as never);
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: storedRule } as never);

		render(LibraryProcessingRules);

		const select = await screen.findByLabelText(m.modules_rules_select_label());
		await fireEvent.change(select, { target: { value: storedRule.id } });
		await chooseFormat('YAML');
		await fireEvent.click(screen.getByRole('button', { name: m.modules_rules_export() }));
		await fireEvent.click(importButton());

		await waitFor(() => expect(postSpy).toHaveBeenCalledTimes(1));
		expect(postSpy).toHaveBeenCalledWith(RULES_PATH, { body: ruleInput });
	});
});
