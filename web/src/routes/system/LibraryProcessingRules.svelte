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
	// Per-request deadline for every rules API call (page, detail, import, save).
	const REQUEST_TIMEOUT_MS = 15_000;

	let activeLoadController: AbortController | null = null;
	let requestSeq = 0;

	let format = $state<Format>('yaml');
	let text = $state('');
	let loadedRules = $state<StoredRule[]>([]);
	let hasListSnapshot = $state(false);
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
	// Set only when a POST/PUT hit its own deadline: the server may or may not have committed it.
	let writeTimeout = $state.raw<{ message: string; targetId: string } | null>(null);
	let storedPreview = $state<{ id: string; label: string; document: string } | null>(null);

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

	// The deadline is combined only into the signal handed to the transport. isCurrent() keeps
	// checking the supersession/unmount signal alone, so a timeout still reaches the catch and
	// finally blocks as a current request and clears the busy state with an error.
	function withDeadline(signal: AbortSignal): AbortSignal {
		return AbortSignal.any([signal, AbortSignal.timeout(REQUEST_TIMEOUT_MS)]);
	}

	// A write that hit its own deadline has an unknown outcome (the server may have committed it).
	// Native semantics: AbortSignal.timeout() aborts with a TimeoutError reason. A definitive problem
	// response from the server is never treated as unknown.
	function isUnknownWriteOutcome(err: unknown, deadline: AbortSignal): boolean {
		if (!deadline.aborted || problemStatus(err) !== 0) return false;
		const reason: unknown = deadline.reason;
		return (
			typeof reason === 'object' &&
			reason !== null &&
			'name' in reason &&
			reason.name === 'TimeoutError'
		);
	}

	function reportWriteFailure(err: unknown, deadline: AbortSignal, targetId = ''): void {
		if (unmountController.signal.aborted) return;
		if (isUnknownWriteOutcome(err, deadline)) {
			// Nothing was confirmed: only an explicit read checks the stored state.
			const message = m.modules_rules_write_timeout();
			writeTimeout = { message, targetId };
			toast.error(message);
			return;
		}
		submitError = err;
		toast.error(describeError(err));
		// A definitive rejection clears timeout recovery; restore the list if no read has completed.
		if (!hasListSnapshot) {
			void loadRules();
		}
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
		if (!page) throw new Error('Rules page response did not include a body');
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

	async function fetchRule(targetId: string, signal: AbortSignal): Promise<StoredRule | null> {
		try {
			const detail = await api.GET('/library-processing/rules/{ruleId}', {
				params: { path: { ruleId: targetId } },
				signal: withDeadline(signal)
			});
			if (!detail.data) throw new Error('Rule detail response did not include a body');
			return detail.data;
		} catch (err) {
			if (problemStatus(err) === 404) return null;
			throw err;
		}
	}

	async function reconcileSelection(
		items: StoredRule[],
		more: boolean,
		prefer: StoredRule | string | undefined,
		known: StoredRule | undefined,
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
		const resolved = knownTarget ?? (await fetchRule(targetId, signal));
		if (!resolved) {
			return { nextSelectedId: '', nextPinned: null, missingId: targetId };
		}
		return { nextSelectedId: resolved.id, nextPinned: resolved, missingId: '' };
	}

	function hasRule(items: StoredRule[], target: StoredRule | null): boolean {
		return target !== null && items.some((rule) => rule.id === target.id);
	}

	function syncRetainedRules(
		page: ReturnType<typeof parsePageState>,
		missingId: string,
		checked: { id: string; rule: StoredRule | null } | null
	): void {
		function reconcile(cached: StoredRule | null): StoredRule | null {
			if (!cached) return null;
			const { id } = cached;
			if (checked?.id === id) return checked.rule;
			const onPage = page.items.find((rule) => rule.id === id);
			if (onPage) return onPage;
			if (pinnedRule?.id === id) return pinnedRule;
			return page.more && id !== missingId ? cached : null;
		}

		lastSavedRule = reconcile(lastSavedRule);
		savedRule = reconcile(savedRule);
		// Only the list overlay is redundant when the page or selection pin represents it.
		if (savedRule && (hasRule(page.items, savedRule) || savedRule.id === pinnedRule?.id)) {
			savedRule = null;
		}
	}

	async function lookupStoredPreview(
		targetId: string,
		page: ReturnType<typeof parsePageState>,
		freshDetail: StoredRule | null,
		missingId: string,
		signal: AbortSignal
	): Promise<StoredRule | null> {
		const pageRule = page.items.find((rule) => rule.id === targetId);
		if (pageRule) return pageRule;
		if (freshDetail?.id === targetId) return freshDetail;
		if (!page.more || missingId === targetId) return null;
		return fetchRule(targetId, signal);
	}

	async function loadRules(prefer?: StoredRule | string, known?: StoredRule): Promise<void> {
		const recovery = writeTimeout;
		const { seq, signal } = startRequest();
		storedPreview = null;
		loading = true;
		loadingMore = false;
		loadError = null;
		loadMoreError = null;
		try {
			const res = await api.GET('/library-processing/rules/page', {
				params: { query: { limit: LIBRARY_PROCESSING_RULE_PAGE_LIMIT } },
				signal: withDeadline(signal)
			});
			if (!isCurrent(seq, signal)) return;
			const page = parsePageState(res.data);
			const selection = await reconcileSelection(page.items, page.more, prefer, known, signal);
			if (!isCurrent(seq, signal)) return;
			// No inspection and a confirmed absent target are different outcomes.
			let checked: { id: string; rule: StoredRule | null } | null = null;
			// POST has no target ID; only a timed-out PUT can produce a preview.
			if (recovery?.targetId && recovery === writeTimeout) {
				let freshDetail = selection.nextPinned;
				if (freshDetail === prefer || freshDetail === known) freshDetail = null;
				const rule = await lookupStoredPreview(
					recovery.targetId,
					page,
					freshDetail,
					selection.missingId,
					signal
				);
				if (!isCurrent(seq, signal)) return;
				checked = { id: recovery.targetId, rule };
				if (rule) {
					storedPreview = {
						id: rule.id,
						label: `${ruleLabel(rule)} (${format.toUpperCase()})`,
						document: writeRuleDocument(rule, format)
					};
				}
			}
			loadedRules = page.items;
			hasMore = page.more;
			nextCursor = page.cursor;
			pinnedRule = selection.nextPinned;
			selectedId = selection.nextSelectedId;
			syncRetainedRules(page, selection.missingId, checked);
			hasListSnapshot = true;
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

	function clearReachedPinned(combined: StoredRule[]): void {
		if (hasRule(combined, pinnedRule)) pinnedRule = null;
		if (hasRule(combined, savedRule)) savedRule = null;
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
				signal: withDeadline(signal)
			});
			if (!isCurrent(seq, signal)) return;
			const page = parsePageState(res.data);
			const combined = appendRulePage(loadedRules, page.items);
			loadedRules = combined;
			hasMore = page.more;
			nextCursor = page.cursor;
			clearReachedPinned(combined);
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

	function startWrite(): AbortSignal {
		requestSeq += 1;
		activeLoadController?.abort();
		activeLoadController = null;
		loading = false;
		loadingMore = false;
		writeTimeout = null;
		storedPreview = null;
		// Keep the transport deadline independent of navigation: unmount is not a rollback.
		return AbortSignal.timeout(REQUEST_TIMEOUT_MS);
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
	}

	async function importRule(): Promise<void> {
		if (busy) return;
		const input = parseDocument();
		if (!input) return;

		importing = true;
		const deadline = startWrite();
		try {
			const res = await api.POST('/library-processing/rules', {
				body: input,
				signal: deadline
			});
			if (unmountController.signal.aborted) return;
			status = m.modules_rules_import_success();
			toast.success(status);
			// The editor now holds a new document, not the one that was opened for editing.
			editingId = '';
			lastSavedRule = null;
			savedRule = null;
			// Keep the created rule selected and pinned before refreshing so a failed or partial
			// refresh cannot lose it; a successful refresh deduplicates it when the page contains it.
			if (res.data) {
				pinnedRule = res.data;
				selectedId = res.data.id;
			}
			await loadRules(res.data);
		} catch (err) {
			reportWriteFailure(err, deadline);
		} finally {
			if (!unmountController.signal.aborted) importing = false;
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
		const deadline = startWrite();
		try {
			const res = await api.PUT('/library-processing/rules/{ruleId}', {
				params: { path: { ruleId } },
				body: input,
				signal: deadline
			});
			if (unmountController.signal.aborted) return;
			if (res.data) {
				applySavedRule(res.data);
			}
			status = m.modules_rules_save_success();
			toast.success(status);
			await loadRules(undefined, res.data);
		} catch (err) {
			reportWriteFailure(err, deadline, ruleId);
		} finally {
			if (!unmountController.signal.aborted) saving = false;
		}
	}

	// A read-only refresh cannot establish whether a timed-out write committed.
	// Keep the warning until a validated import or save starts.
	async function checkStoredState(): Promise<void> {
		if (busy || loading) return;
		await loadRules();
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
			{#if loadMoreError}
				<ProblemNote error={loadMoreError} />
			{/if}
		{:else if hasListSnapshot && !loadError}
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

	{#if writeTimeout}
		<div class="flex flex-col gap-2" role="alert">
			<p class="text-sm text-danger">{writeTimeout.message}</p>
			<div>
				<button
					type="button"
					class="btn min-h-11 px-3 py-1.5 text-xs md:min-h-0"
					disabled={busy || loading}
					onclick={checkStoredState}
				>
					{m.modules_rules_check_state()}
				</button>
			</div>
		</div>
	{/if}

	{#if storedPreview}
		<div class="flex flex-col gap-2">
			<label for="modules-rules-stored-document" class="text-sm font-medium">
				{m.modules_rules_check_state()} · {storedPreview.label}
			</label>
			<textarea
				id="modules-rules-stored-document"
				class="input w-full font-mono text-sm"
				rows="8"
				spellcheck="false"
				readonly
				value={storedPreview.document}></textarea>
		</div>
	{/if}

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
