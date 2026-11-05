<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { createSentioQuery } from '@sveltesentio/query';
	import { api } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { keys } from '#lib/query.js';

	type Preflight = components['schemas']['ForeignSearchPreflight'];
	type Verdict = components['schemas']['ForeignSearchVerdict'];

	const { instanceId }: { instanceId: string } = $props();
	// The route layout remounts per instance, so the initial id is the id.
	const id = untrack(() => instanceId);
	const headingId = $props.id();
	const preflight = createSentioQuery<Preflight | undefined>({
		queryKey: keys.foreignSearchPreflight(id),
		staleTime: 0,
		queryFn: async () =>
			(
				await api.GET('/instances/{instanceId}/foreign-search/preflight', {
					params: { path: { instanceId: id } }
				})
			).data
	});

	const verdictLabels: Record<Verdict['verdict'], () => string> = {
		allowed: m.foreign_search_verdict_allowed,
		original_only: m.foreign_search_verdict_original_only,
		skipped: m.foreign_search_verdict_skipped
	};
	const reasonLabels: Record<Verdict['reason'], () => string> = {
		profile_any: m.foreign_search_reason_profile_any,
		profile_language_match: m.foreign_search_reason_profile_language_match,
		profile_original: m.foreign_search_reason_profile_original,
		profile_language_mismatch: m.foreign_search_reason_profile_language_mismatch,
		profile_language_missing: m.foreign_search_reason_profile_language_missing,
		language_unmapped: m.foreign_search_reason_language_unmapped,
		original_language_match: m.foreign_search_reason_original_language_match,
		original_language_mismatch: m.foreign_search_reason_original_language_mismatch
	};
</script>

{#if preflight.isPending}
	<p aria-live="polite">{m.loading()}</p>
{:else if preflight.isError}
	<ProblemNote error={preflight.error} />
{:else if preflight.data?.applies}
	<section class="card flex min-w-0 flex-col gap-4" aria-labelledby={headingId}>
		<h3 id={headingId} class="text-sm font-semibold">{m.foreign_search_preflight_title()}</h3>
		<p class="text-xs text-subtle">{m.foreign_search_preflight_hint()}</p>
		{#if preflight.data.profiles.length === 0}
			<p class="text-sm text-subtle">{m.foreign_search_preflight_empty()}</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="table w-full">
					<caption class="sr-only">{m.foreign_search_preflight_title()}</caption>
					<thead>
						<tr>
							<th scope="col">{m.foreign_search_profile()}</th>
							<th scope="col">{m.foreign_search_profile_language()}</th>
							{#each preflight.data.languages as language (language)}
								<th scope="col">{language.toUpperCase()}</th>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each preflight.data.profiles as profile (profile.id)}
							<tr>
								<th scope="row">{profile.name}</th>
								<td
									>{profile.profile_language?.name ??
										m.foreign_search_profile_language_missing()}</td
								>
								{#each preflight.data.languages as language (language)}
									{@const result = profile.verdicts.find((v) => v.language === language)}
									<td>
										{#if result}
											<span class="font-medium">{verdictLabels[result.verdict]()}</span>
											<p class="text-sm text-subtle">{reasonLabels[result.reason]()}</p>
										{/if}
									</td>
								{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
{/if}
