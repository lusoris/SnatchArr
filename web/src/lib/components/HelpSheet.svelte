<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import ExternalLink from '@lucide/svelte/icons/external-link';
	import Dialog from '@sveltesentio/ui/dialog/component';
	import { docsUrl } from '#lib/docs.js';
	import { getHelpSummary } from '#lib/help.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		open?: boolean;
		summaryKey: string;
		docsPath: string;
		title?: string;
	}

	let {
		open = $bindable(false),
		summaryKey,
		docsPath,
		title = m.help_sheet_title()
	}: Props = $props();

	const summary = $derived(getHelpSummary(summaryKey));
	const guideUrl = $derived(docsUrl(docsPath));
</script>

<!-- Note: ADR-0029 section 6 specifies a side sheet (desktop) and bottom sheet (mobile), but library Dialog has no sheet placement (dialogContentClass only concatenates classes without merging, so conflicting left-1/2/top-1/2/translate remain). Kept as centered modal until library Dialog supports sheet placement. -->
<Dialog bind:open {title} class="max-w-lg border border-line bg-surface p-6 shadow-xl">
	<div class="flex flex-col gap-4 py-2">
		<p class="text-sm leading-relaxed text-fg">
			{summary}
		</p>

		<div>
			<a
				href={guideUrl}
				target="_blank"
				rel="external noopener"
				class="text-brand inline-flex items-center gap-1.5 text-sm font-medium underline hover:opacity-80"
			>
				<span>{m.help_read_full_guide()}</span>
				<ExternalLink size={14} aria-hidden="true" />
			</a>
		</div>
	</div>
</Dialog>
