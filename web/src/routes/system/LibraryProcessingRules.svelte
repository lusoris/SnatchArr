<!--
	SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
	SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { api, describeError, problemStatus } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { readRuleDocument, writeRuleDocument } from '#lib/library-processing-document.js';
	import {
		appendRulePage,
		compareStoredRules,
		LIBRARY_PROCESSING_RULE_PAGE_LIMIT,
		mergePinnedRule
	} from '#lib/library-processing-rule.js';
	import { m } from '#lib/paraglide/messages.js';

	type StoredRule = components['schemas']['LibraryProcessingRule'];
	type RulePage = components['schemas']['LibraryProcessingRulePage'];
	type Format = 'yaml' | 'json';

	const formats: Format[] = ['yaml', 'json'];
	const unmountController = new AbortController();

	let activeLoadController: AbortController | null = null;
	let requestSeq = 0;

	let format = $state<Format>('yaml');
	let text = $state('');
	let loadedRules = $state<StoredRule[]>([]);
	let pinnedRule = $state<StoredRule | null>(null);
	let nextCursor = $state<string | null>(null);
	let hasMore = $state(false);
	let selectedId = $state('');
	let loading = $state(true);
	let loadingMore = $state(false);
	let loadError = $state<unknown>(null);
	let loadMoreError = $state<unknown>(null);
	let submitError = $state<unknown>(null);
	let documentError = $state('');
	let status = $state('');
	let importing = $state(false);
	let saving = $state(false);
	// The stored rule whose document was opened in the editor; only an explicit save writes to it.
	let editingId = $state('');
	let lastSavedRule = $state<StoredRule | null>(null);
	let savedRule = $state<StoredRule | null>(null);

	type RuleInput = ReturnType<typeof readRuleDocument>;

	const rules = $derived(mergePinnedRule(mergePinnedRule(loadedRules, pinnedRule), savedRule));
	const selectedRule = $derived(rules.find((rule) => rule.id === selectedId));
	const editingRule = $derived(
		rules.find((rule) => rule.id === editingId) ??
			(lastSavedRule?.id === editingId ? lastSavedRule : undefined)
	);
	const busy = $derived(importing || saving);

	function startRequest(): { seq: number; signal: AbortSignal } {
		activeLoadController?.abort();
		const next = new AbortController();
		activeLoadController = next;
		requestSeq += 1;
		return {
			seq: requestSeq,
			signal: AbortSignal.any([unmountController.signal, next.signal])
		};
	}

	function isCurrent(seq: number, signal: AbortSignal): boolean {
		return seq === requestSeq && !signal.aborted && !unmountController.signal.aborted;
	}

	function extractNextCursor(page: RulePage): string | null {
		if (!page.has_more || !page.next_cursor) return null;
		return page.next_cursor;
	}

	function parsePageState(page: RulePage | undefined): {
		items: StoredRule[];
		more: boolean;
		cursor: string | null;
	} {
		if (!page) return { items: [], more: false, cursor: null };
		const cursor = extractNextCursor(page);
		return { items: page.items, more: cursor !== null, cursor };
	}

	function extractPreference(prefer?: StoredRule | string): {
		targetId: string;
		preferred: StoredRule | null;
	} {
		if (typeof prefer === 'string') {
			return { targetId: prefer, preferred: null };
		}
		if (prefer) {
			return { targetId: prefer.id, preferred: prefer };
		}
		return { targetId: selectedId, preferred: null };
	}

	async function fetchRuleOrFallback(
		targetId: string,
		fallback: StoredRule | undefined,
		signal: AbortSignal
	): Promise<StoredRule | null> {
		try {
			const detail = await api.GET('/library-processing/rules/{ruleId}', {
				params: { path: { ruleId: targetId } },
				signal
			});
			return detail.data ?? null;
		} catch (err) {
			if (problemStatus(err) === 404) return null;
			if (fallback?.id === targetId) return fallback;
			throw err;
		}
	}

	async function resolveOffPageSelection(
		targetId: string,
		knownTarget: StoredRule | null,
		previousSelected: StoredRule | undefined,
		signal: AbortSignal
	): Promise<StoredRule | null> {
		if (knownTarget) return knownTarget;
		const fallback = rules.find((rule) => rule.id === targetId) ?? previousSelected;
		return fetchRuleOrFallback(targetId, fallback, signal);
	}

	async function reconcileSelection(
		items: StoredRule[],
		more: boolean,
		prefer: StoredRule | string | undefined,
		known: StoredRule | undefined,
		previousSelected: StoredRule | undefined,
		signal: AbortSignal
	): Promise<{ nextSelectedId: string; nextPinned: StoredRule | null; missingId: string }> {
		const { targetId, preferred } = extractPreference(prefer);
		if (!targetId) {
			return { nextSelectedId: '', nextPinned: null, missingId: '' };
		}
		if (items.some((rule) => rule.id === targetId)) {
			return { nextSelectedId: targetId, nextPinned: null, missingId: '' };
		}
		if (!more) {
			return { nextSelectedId: '', nextPinned: null, missingId: targetId };
		}
		const knownTarget = preferred ?? (known?.id === targetId ? known : null);
		const resolved = await resolveOffPageSelection(targetId, knownTarget, previousSelected, signal);
		if (!resolved) {
			return { nextSelectedId: '', nextPinned: null, missingId: targetId };
		}
		return { nextSelectedId: resolved.id, nextPinned: resolved, missingId: '' };
	}

	function syncSavedRule(items: StoredRule[], more: boolean, missingId = ''): void {
		if (!savedRule) return;
		const id = savedRule.id;
		if (!more || id === missingId || items.some((rule) => rule.id === id)) {
			savedRule = null;
		}
	}

	async function loadRules(prefer?: StoredRule | string, known?: StoredRule): Promise<void> {
		const previousSelected = selectedRule;
		const { seq, signal } = startRequest();
		loading = true;
		loadingMore = false;
		loadError = null;
		loadMoreError = null;
		try {
			const res = await api.GET('/library-processing/rules/page', {
				params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } },
				signal
			});
			const page = parsePageState(res.data);
			const selection = await reconcileSelection(
				page.items,
				page.more,
				prefer,
				known,
				previousSelected,
				signal
			);
			if (!isCurrent(seq, signal)) return;
			loadedRules = page.items;
			hasMore = page.more;
			nextCursor = page.cursor;
			pinnedRule = selection.nextPinned;
			selectedId = selection.nextSelectedId;
			syncSavedRule(page.items, page.more, selection.missingId);
		} catch (err) {
			if (isCurrent(seq, signal)) loadError = err;
		} finally {
			if (isCurrent(seq, signal)) loading = false;
		}
	}

	function nextPageCursor(): string | null {
		if (loading || loadingMore || !hasMore) return null;
		return nextCursor;
	}

	function clearReachedPinned(combined: StoredRule[], more: boolean): void {
		const pinnedId = pinnedRule?.id;
		if (pinnedId && combined.some((rule) => rule.id === pinnedId)) {
			pinnedRule = null;
		}
		syncSavedRule(combined, more);
	}

	async function loadMoreRules(): Promise<void> {
		const cursor = nextPageCursor();
		if (!cursor) return;
		const { seq, signal } = startRequest();
		loadingMore = true;
		loadMoreError = null;
		try {
			const res = await api.GET('/library-processing/rules/page', {
				params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT, cursor } },
				signal
			});
			if (!isCurrent(seq, signal)) return;
			const page = parsePageState(res.data);
			const combined = appendRulePage(loadedRules, page.items);
			loadedRules = combined;
			hasMore = page.more;
			nextCursor = page.cursor;
			clearReachedPinned(combined, page.more);
		} catch (err) {
			if (isCurrent(seq, signal)) loadMoreError = err;
		} finally {
			if (isCurrent(seq, signal)) loadingMore = false;
		}
	}

	// Parse and validate first so invalid input never reaches the API.
	function parseDocument(): RuleInput | null {
		documentError = '';
		status = '';
		submitError = null;

		if (!text.trim()) {
			documentError = m.modules_rules_document_empty();
			return null;
		}

		try {
			return readRuleDocument(text, format);
		} catch (err) {
			documentError = m.modules_rules_invalid({ reason: describeError(err) });
			return null;
		}
	}

	function applySavedRule(saved: StoredRule): void {
		lastSavedRule = saved;
		savedRule = saved;
		if (pinnedRule?.id === saved.id) {
			pinnedRule = saved;
		}
		if (!loadedRules.some((rule) => rule.id === saved.id)) {
			if (!hasMore) {
				loadedRules = [...loadedRules, saved].sort(compareStoredRules);
				savedRule = null;
			}
			return;
		}
		const boundary = loadedRules[loadedRules.length - 1];
		const withoutSaved = loadedRules.filter((rule) => rule.id !== saved.id);
		if (hasMore && boundary && compareStoredRules(saved, boundary) > 0) {
			loadedRules = withoutSaved;
			return;
		}
		loadedRules = [...withoutSaved, saved].sort(compareStoredRules);
		savedRule = null;
	}

	async function importRule(): Promise<void> {
		if (busy) return;
		const input = parseDocument();
		if (!input) return;

		importing = true;
		try {
			const res = await api.POST('/library-processing/rules', { body: input });
			status = m.modules_rules_import_success();
			toast.success(status);
			// The editor now holds a new document, not the one that was opened for editing.
			editingId = '';
			lastSavedRule = null;
			savedRule = null;
			await loadRules(res.data);
		} catch (err) {
			submitError = err;
			toast.error(describeError(err));
		} finally {
			importing = false;
		}
	}

	async function saveRule(): Promise<void> {
		if (busy) return;
		// Bind the target now: later list selection changes must not redirect the write.
		const ruleId = editingId;
		if (!ruleId) return;
		const input = parseDocument();
		if (!input) return;

		saving = true;
		try {
			const res = await api.PUT('/library-processing/rules/{ruleId}', {
				params: { path: { ruleId } },
				body: input
			});
			if (res.data) {
				applySavedRule(res.data);
			}
			status = m.modules_rules_save_success();
			toast.success(status);
			await loadRules(undefined, res.data);
		} catch (err) {
			submitError = err;
			toast.error(describeError(err));
		} finally {
			saving = false;
		}
	}

	function exportRule(): void {
		documentError = '';
		status = '';
		submitError = null;
		if (!selectedRule) return;
		try {
			text = writeRuleDocument(selectedRule, format);
			editingId = selectedRule.id;
			lastSavedRule = selectedRule;
			status = m.modules_rules_export_success();
		} catch (err) {
			documentError = describeError(err);
		}
	}

	function ruleLabel(rule: StoredRule): string {
		return `${rule.scope} · ${rule.id}`;
	}

	onMount(() => () => {
		activeLoadController?.abort();
		unmountController.abort();
	});
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

	<fieldset class="flex flex-wrap items-center gap-4" disabled={busy}>
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
		{:else if rules.length > 0}
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
					disabled={!selectedRule || busy}
					onclick={exportRule}
				>
					{m.modules_rules_export()}
				</button>
				{#if hasMore}
					<button
						type="button"
						class="btn min-h-11 px-3 py-1.5 text-xs md:min-h-0"
						disabled={loadingMore || busy}
						aria-busy={loadingMore}
						onclick={loadMoreRules}
					>
						{loadingMore ? m.modules_rules_loading_more() : m.modules_rules_load_more()}
					</button>
				{/if}
			</div>
			{#if loadMoreError && !loadError}
				<ProblemNote error={loadMoreError} />
			{/if}
		{:else if !loadError}
			<p class="text-sm text-subtle">{m.modules_rules_empty()}</p>
		{/if}
		{#if !loading && loadError}
			<ProblemNote error={loadError} />
			<div>
				<button
					type="button"
					class="btn min-h-11 px-3 py-1.5 text-xs md:min-h-0"
					disabled={busy}
					onclick={() => loadRules()}
				>
					{m.modules_rules_retry()}
				</button>
			</div>
		{/if}
	</div>

	<div class="flex flex-col gap-2">
		<label for="modules-rules-document" class="text-sm font-medium">
			{m.modules_rules_document_label()}
		</label>
		{#if editingId}
			<p id="modules-rules-editing" class="text-sm text-subtle">
				{m.modules_rules_editing({ rule: editingRule ? ruleLabel(editingRule) : editingId })}
			</p>
		{/if}
		<textarea
			id="modules-rules-document"
			class="input min-h-48 w-full font-mono text-sm"
			spellcheck="false"
			rows="12"
			bind:value={text}
			disabled={busy}
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
			disabled={busy}
			aria-busy={importing}
			onclick={importRule}
		>
			{importing ? m.modules_rules_importing() : m.modules_rules_import()}
		</button>
		<button
			type="button"
			class="btn min-h-11 px-3 py-1.5 text-xs md:min-h-0"
			disabled={!editingId || busy}
			aria-busy={saving}
			onclick={saveRule}
		>
			{saving ? m.modules_rules_saving() : m.modules_rules_save()}
		</button>
		<p class="text-sm text-subtle" role="status" aria-live="polite">{status}</p>
	</div>
</section>
