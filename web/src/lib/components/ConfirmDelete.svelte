<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import ConfirmAction from '#lib/components/ConfirmAction.svelte';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		/** Name of the record, shown in the question. */
		name: string;
		disabled?: boolean;
		onconfirm: () => void;
	}

	const { name, disabled = false, onconfirm }: Props = $props();
	const question = $derived(m.confirm_delete({ name }));
</script>

<!-- The delete wording of the shared in-page confirm step. -->
<ConfirmAction
	label={m.action_delete()}
	message={question}
	groupLabel={question}
	confirmLabel={m.confirm_delete_yes()}
	backLabel={m.action_cancel()}
	pending={disabled}
	inline
	{onconfirm}
/>
