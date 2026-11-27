<!--
	SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
	SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { readRuleDocument, writeRuleDocument } from '#lib/library-processing-document.js';
	import { m } from '#lib/paraglide/messages.js';

	type StoredRule = components['schemas']['LibraryProcessingRule'];
	type Format = 'yaml' | 'json';

	const formats: Format[] = ['yaml', 'json'];
	const controller = new AbortController();

	let format = $state<Format>('yaml');
	let text = $state('');
	let rules = $state<StoredRule[]>([]);
	let selectedId = $state('');
	let loading = $state(true);
	let loadError = $state<unknown>(null);
	let submitError = $state<unknown>(null);
	let documentError = $state('');
	let status = $state('');
	let importing = $state(false);

	const selectedRule = $derived(rules.find((rule) => rule.id === selectedId));

	async function loadRules(selectId?: string): Promise<void> {
		loading = true;
		loadError = null;
		try {
			const res = await api.GET('/library-processing/rules', { signal: controller.signal });
			rules = res.data ?? [];
			if (selectId) selectedId = selectId;
			else if (!rules.some((rule) => rule.id === selectedId)) selectedId = '';
		} catch (err) {
			if (!controller.signal.aborted) loadError = err;
		} finally {
			if (!controller.signal.aborted) loading = false;
		}
	}

	async function importRule(): Promise<void> {
		if (importing) return;
		documentError = '';
		status = '';
		submitError = null;

		if (!text.trim()) {
			documentError = m.modules_rules_document_empty();
			return;
		}

		// Parse and validate first so invalid input never reaches the API.
		let input: ReturnType<typeof readRuleDocument>;
		try {
			input = readRuleDocument(text, format);
		} catch (err) {
			documentError = m.modules_rules_invalid({ reason: describeError(err) });
			return;
		}

		importing = true;
		try {
			const res = await api.POST('/library-processing/rules', { body: input });
			status = m.modules_rules_import_success();
			toast.success(status);
			await loadRules(res.data?.id);
		} catch (err) {
			submitError = err;
			toast.error(describeError(err));
		} finally {
			importing = false;
		}
	}

	function exportRule(): void {
		documentError = '';
		status = '';
		submitError = null;
		if (!selectedRule) return;
		try {
			text = writeRuleDocument(selectedRule, format);
			status = m.modules_rules_export_success();
		} catch (err) {
			documentError = describeError(err);
		}
	}

	function ruleLabel(rule: StoredRule): string {
		return `${rule.scope} · ${rule.id}`;
	}

	onMount(() => () => controller.abort());
	onMount(() => {
		void loadRules();
	});
</script>

<section class="card flex w-full flex-col gap-4" aria-labelledby="modules-rules-heading">
	<div class="flex flex-col gap-1">
		<h3 id="modules-rules-heading" class="text-lg font-semibold">
			{m.modules_rules_title()}
		</h3>
		<p class="text-sm text-subtle">{m.modules_rules_desc()}</p>
	</div>

	<fieldset class="flex flex-wrap items-center gap-4" disabled={importing}>
		<legend class="mb-1 text-sm font-medium">{m.modules_rules_format_legend()}</legend>
		{#each formats as option (option)}
			<label class="flex min-h-11 items-center gap-2 text-sm md:min-h-0">
				<input type="radio" name="modules-rules-format" value={option} bind:group={format} />
				{option.toUpperCase()}
			</label>
		{/each}
	</fieldset>

	<div class="flex flex-col gap-2">
		<label for="modules-rules-select" class="text-sm font-medium">
			{m.modules_rules_select_label()}
		</label>
		{#if loading}
			<p class="text-sm text-subtle" aria-live="polite">{m.loading()}</p>
		{:else if loadError}
			<ProblemNote error={loadError} />
			<div>
				<button
					type="button"
					class="btn min-h-11 px-3 py-1.5 text-xs md:min-h-0"
					onclick={() => loadRules()}
				>
					{m.modules_rules_retry()}
				</button>
			</div>
		{:else if rules.length === 0}
			<p class="text-sm text-subtle">{m.modules_rules_empty()}</p>
		{:else}
			<div class="flex flex-wrap items-center gap-2">
				<select id="modules-rules-select" class="input min-h-11 md:min-h-0" bind:value={selectedId}>
					<option value="">{m.modules_rules_select_placeholder()}</option>
					{#each rules as rule (rule.id)}
						<option value={rule.id}>{ruleLabel(rule)}</option>
					{/each}
				</select>
				<button
					type="button"
					class="btn min-h-11 px-3 py-1.5 text-xs md:min-h-0"
					disabled={!selectedRule || importing}
					onclick={exportRule}
				>
					{m.modules_rules_export()}
				</button>
			</div>
		{/if}
	</div>

	<div class="flex flex-col gap-2">
		<label for="modules-rules-document" class="text-sm font-medium">
			{m.modules_rules_document_label()}
		</label>
		<textarea
			id="modules-rules-document"
			class="input min-h-48 w-full font-mono text-sm"
			spellcheck="false"
			rows="12"
			bind:value={text}
			disabled={importing}
			aria-invalid={documentError ? 'true' : undefined}
			aria-describedby={documentError ? 'modules-rules-document-error' : undefined}></textarea>
		{#if documentError}
			<p id="modules-rules-document-error" class="text-sm text-danger" role="alert">
				{documentError}
			</p>
		{/if}
	</div>

	{#if submitError}
		<ProblemNote error={submitError} />
	{/if}

	<div class="flex flex-wrap items-center gap-3">
		<button
			type="button"
			class="btn btn-primary min-h-11 px-3 py-1.5 text-xs md:min-h-0"
			disabled={importing}
			aria-busy={importing}
			onclick={importRule}
		>
			{importing ? m.modules_rules_importing() : m.modules_rules_import()}
		</button>
		<p class="text-sm text-subtle" role="status" aria-live="polite">{status}</p>
	</div>
</section>
