<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import BookOpen from '@lucide/svelte/icons/book-open';
	import Search from '@lucide/svelte/icons/search';
	import Server from '@lucide/svelte/icons/server';
	import { m } from '#lib/paraglide/messages.js';

	const DOCS_URL = 'https://lusoris.github.io/SnatchArr/';
</script>

<section class="card flex flex-col gap-6 p-6" aria-labelledby="onboarding-title">
	<div class="flex flex-col gap-1">
		<h2 id="onboarding-title" class="text-xl font-bold">{m.overview_onboarding_title()}</h2>
		<p class="text-sm text-subtle">{m.overview_onboarding_subtitle()}</p>
	</div>

	<div class="grid grid-cols-1 gap-4 md:grid-cols-3">
		<div class="flex flex-col justify-between rounded-lg border border-line bg-surface p-4">
			<div class="flex flex-col gap-2">
				<div class="flex items-center gap-2">
					<span class="bg-brand/10 text-brand flex h-8 w-8 items-center justify-center rounded-md">
						<Server size={18} aria-hidden="true" />
					</span>
					<h3 class="font-semibold text-fg">{m.overview_onboarding_step1_title()}</h3>
				</div>
				<p class="text-xs text-subtle">{m.overview_onboarding_step1_desc()}</p>
			</div>
			<div class="mt-4">
				<a href={resolve('/instances')} class="btn btn-primary w-full justify-center">
					{m.overview_onboarding_step1_action()}
				</a>
			</div>
		</div>

		<div class="flex flex-col justify-between rounded-lg border border-line bg-surface p-4">
			<div class="flex flex-col gap-2">
				<div class="flex items-center gap-2">
					<span class="bg-brand/10 text-brand flex h-8 w-8 items-center justify-center rounded-md">
						<Search size={18} aria-hidden="true" />
					</span>
					<h3 class="font-semibold text-fg">{m.overview_onboarding_step2_title()}</h3>
				</div>
				<p class="text-xs text-subtle">{m.overview_onboarding_step2_desc()}</p>
			</div>
			<div class="mt-4">
				<a href={resolve('/connections/prowlarr')} class="btn w-full justify-center">
					{m.overview_onboarding_step2_action()}
				</a>
			</div>
		</div>

		<div class="flex flex-col justify-between rounded-lg border border-line bg-surface p-4">
			<div class="flex flex-col gap-2">
				<div class="flex items-center gap-2">
					<span class="bg-brand/10 text-brand flex h-8 w-8 items-center justify-center rounded-md">
						<BookOpen size={18} aria-hidden="true" />
					</span>
					<h3 class="font-semibold text-fg">{m.overview_onboarding_step3_title()}</h3>
				</div>
				<p class="text-xs text-subtle">{m.overview_onboarding_step3_desc()}</p>
			</div>
			<div class="mt-4">
				<a
					href={DOCS_URL}
					target="_blank"
					rel="noopener noreferrer"
					class="btn w-full justify-center"
				>
					{m.overview_onboarding_step3_action()}
				</a>
			</div>
		</div>
	</div>
</section>
