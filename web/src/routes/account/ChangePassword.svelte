<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { ProblemError } from '@sveltesentio/core';
	import { toast } from 'svelte-sonner';
	import { api } from '#lib/api/client.js';
	import Field from '#lib/components/Field.svelte';
	import NewPasswordField from '#lib/components/NewPasswordField.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';

	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let submitting = $state(false);
	let successMessage = $state('');
	let generalError = $state<unknown>(null);
	let fieldErrors = $state<Record<string, string>>({});

	function handleError(err: unknown): void {
		if (err instanceof ProblemError && err.invalidParams && err.invalidParams.length > 0) {
			let matched = false;
			const nextErrors: Record<string, string> = {};
			for (const p of err.invalidParams) {
				if (p.name === 'current_password' || p.name === 'new_password') {
					nextErrors[p.name] = p.reason;
					matched = true;
				}
			}
			fieldErrors = nextErrors;
			if (!matched) generalError = err;
		} else {
			generalError = err;
		}
	}

	async function handleSubmit(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		fieldErrors = {};
		generalError = null;
		successMessage = '';

		if (newPassword !== confirmPassword) {
			fieldErrors = { confirm_password: m.password_mismatch() };
			return;
		}

		submitting = true;
		try {
			await api.POST('/auth/password', {
				body: {
					current_password: currentPassword,
					new_password: newPassword
				}
			});
			successMessage = m.password_changed_sessions_ended();
			toast.success(m.password_changed_sessions_ended());

			currentPassword = '';
			newPassword = '';
			confirmPassword = '';
		} catch (err) {
			handleError(err);
		} finally {
			submitting = false;
		}
	}
</script>

<form class="card flex max-w-xl flex-col gap-4" onsubmit={handleSubmit}>
	<Field id="current-password" label={m.current_password()}>
		<input
			id="current-password"
			class="input"
			type="password"
			autocomplete="current-password"
			required
			bind:value={currentPassword}
		/>
	</Field>
	{#if fieldErrors.current_password}
		<p role="alert" class="text-xs text-danger">{fieldErrors.current_password}</p>
	{/if}

	<NewPasswordField bind:value={newPassword} />
	{#if fieldErrors.new_password}
		<p role="alert" class="text-xs text-danger">{fieldErrors.new_password}</p>
	{/if}

	<Field id="confirm-password" label={m.confirm_new_password()}>
		<input
			id="confirm-password"
			class="input"
			type="password"
			autocomplete="new-password"
			required
			minlength="12"
			bind:value={confirmPassword}
		/>
	</Field>
	{#if fieldErrors.confirm_password}
		<p role="alert" class="text-xs text-danger">{fieldErrors.confirm_password}</p>
	{/if}

	{#if generalError}
		<ProblemNote error={generalError} />
	{/if}

	{#if successMessage}
		<p role="status" class="rounded-md border border-line bg-surface px-3 py-2 text-sm text-fg">
			{successMessage}
		</p>
	{/if}

	<div>
		<button type="submit" class="btn btn-primary" disabled={submitting}>
			{m.change_password_submit()}
		</button>
	</div>
</form>
