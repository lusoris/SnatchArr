<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError } from '#lib/api/client.js';
	import AdminSaveButton from '#lib/components/AdminSaveButton.svelte';
	import Field from '#lib/components/Field.svelte';
	import PageHeader from '#lib/components/PageHeader.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import QueryGuard from '#lib/components/QueryGuard.svelte';
	import { HELP_MAP } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';
	import { keys, queryClient } from '#lib/query.js';
	import { requestsSettingsSchema, type RequestsSettings } from '#lib/requestsSettings.js';
	import { session } from '#lib/session.svelte.js';

	const settingsQuery = createSentioQuery({
		queryKey: keys.requestsSettings,
		queryFn: async () => (await api.GET('/requests/settings')).data
	});

	let draft = $state<RequestsSettings | null>(null);

	$effect(() => {
		if (settingsQuery.data && !draft) {
			draft = { ...settingsQuery.data };
		}
	});

	const saveMutation = createSentioMutation({
		mutationFn: async (payload: RequestsSettings) => {
			const res = await api.PUT('/requests/settings', { body: payload });
			if (res.error) throw res.error;
			return res.data;
		},
		onSuccess: (data) => {
			if (data) {
				draft = { ...data };
				queryClient.setQueryData(keys.requestsSettings, data);
			}
			toast.success(m.requests_settings_saved());
		},
		onError: (err) => {
			toast.error(describeError(err));
		}
	});

	function handleSubmit(e: SubmitEvent): void {
		e.preventDefault();
		if (!draft || !session.isAdmin) return;
		const parsed = requestsSettingsSchema.safeParse(draft);
		if (parsed.success) {
			saveMutation.mutate(parsed.data);
		}
	}
</script>

<PageHeader
	title={m.requests_settings_title()}
	documentTitle
	help={HELP_MAP['/settings/requests']}
/>

<section class="max-w-3xl" aria-labelledby="requests-settings-title">
	<h2 id="requests-settings-title" class="sr-only">{m.requests_settings_title()}</h2>

	<QueryGuard query={settingsQuery}>
		{#if draft}
			<form class="card flex flex-col gap-6" onsubmit={handleSubmit}>
				{#if saveMutation.isError}
					<ProblemNote error={saveMutation.error} />
				{/if}

				{#if !session.isAdmin}
					<p class="text-sm text-subtle" role="status">
						{m.requests_settings_readonly_note()}
					</p>
				{/if}

				<fieldset class="flex flex-col gap-4 border-0 p-0" disabled={!session.isAdmin}>
					<legend class="sr-only">{m.requests_settings_title()}</legend>

					<Field
						id="voting_mode"
						label={m.requests_settings_voting_mode()}
						hint={m.requests_settings_voting_mode_hint()}
					>
						<select
							id="voting_mode"
							data-field="voting_mode"
							class="select"
							aria-describedby="voting_mode-hint"
							bind:value={draft.voting_mode}
							disabled={!session.isAdmin}
						>
							<option value="upvotes_only">
								{m.requests_settings_voting_mode_upvotes_only()}
							</option>
							<option value="net_score">
								{m.requests_settings_voting_mode_net_score()}
							</option>
						</select>
					</Field>

					<div class="flex flex-col gap-1">
						<label for="allow_viewer_votes" class="flex items-center gap-2 text-sm font-medium">
							<input
								id="allow_viewer_votes"
								type="checkbox"
								data-field="allow_viewer_votes"
								class="checkbox"
								aria-describedby="allow_viewer_votes-hint"
								bind:checked={draft.allow_viewer_votes}
								disabled={!session.isAdmin}
							/>
							<span>{m.requests_settings_allow_viewer_votes()}</span>
						</label>
						<p id="allow_viewer_votes-hint" class="text-xs text-subtle">
							{m.requests_settings_allow_viewer_votes_hint()}
						</p>
					</div>

					<Field
						id="visibility"
						label={m.requests_settings_visibility()}
						hint={m.requests_settings_visibility_hint()}
					>
						<select
							id="visibility"
							data-field="visibility"
							class="select"
							aria-describedby="visibility-hint"
							bind:value={draft.visibility}
							disabled={!session.isAdmin}
						>
							<option value="public_attributed">
								{m.requests_settings_visibility_public_attributed()}
							</option>
							<option value="public_anonymous">
								{m.requests_settings_visibility_public_anonymous()}
							</option>
							<option value="private_to_requester_and_admin">
								{m.requests_settings_visibility_private()}
							</option>
						</select>
					</Field>

					<Field
						id="voting_ttl_days"
						label={m.requests_settings_voting_ttl_days()}
						hint={m.requests_settings_voting_ttl_days_hint()}
					>
						<input
							type="number"
							id="voting_ttl_days"
							data-field="voting_ttl_days"
							class="input"
							min="0"
							aria-describedby="voting_ttl_days-hint"
							bind:value={draft.voting_ttl_days}
							disabled={!session.isAdmin}
						/>
					</Field>
				</fieldset>

				<AdminSaveButton disabled={saveMutation.isPending} />
			</form>
		{/if}
	</QueryGuard>
</section>
