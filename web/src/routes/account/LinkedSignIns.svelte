<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { createSentioMutation, createSentioQuery } from '@sveltesentio/query';
	import { toast } from 'svelte-sonner';
	import { api, describeError, problemStatus } from '#lib/api/client.js';
	import type { components } from '#lib/api/schema.js';
	import ConfirmAction from '#lib/components/ConfirmAction.svelte';
	import Field from '#lib/components/Field.svelte';
	import ProblemNote from '#lib/components/ProblemNote.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { keys } from '#lib/query.js';
	import { formatWhen } from '#lib/stamina.js';

	type UserIdentity = components['schemas']['UserIdentity'];

	let jfFormOpen = $state(false);
	let jfUsername = $state('');
	let jfPassword = $state('');
	let errorMessage = $state('');

	const sso = createSentioQuery({
		queryKey: keys.ssoStatus,
		queryFn: async () => (await api.GET('/auth/sso')).data
	});

	const identities = createSentioQuery({
		queryKey: keys.identities,
		queryFn: async () => (await api.GET('/auth/identities')).data
	});

	const isEnabled = $derived(Boolean(sso.data?.jellyfin || sso.data?.proxy));
	const jfIdentity = $derived(
		identities.data?.items.find((i: UserIdentity) => i.provider === 'jellyfin')
	);
	const proxyIdentity = $derived(
		identities.data?.items.find((i: UserIdentity) => i.provider === 'proxy')
	);

	function jellyfinErrorMessage(status: number, err: unknown): string {
		switch (status) {
			case 401:
				return m.linked_sign_in_error_401_jellyfin();
			case 403:
				return m.linked_sign_in_error_403_jellyfin();
			case 404:
				return m.linked_sign_in_error_404_jellyfin();
			case 409:
				return m.linked_sign_in_error_409_jellyfin();
			case 429:
				return m.linked_sign_in_error_429();
			default:
				return m.error_generic({ detail: describeError(err) });
		}
	}

	function proxyErrorMessage(status: number, err: unknown): string {
		switch (status) {
			case 401:
				return m.linked_sign_in_error_401_proxy();
			case 403:
				return m.linked_sign_in_error_403_proxy();
			case 404:
				return m.linked_sign_in_error_404_proxy();
			case 409:
				return m.linked_sign_in_error_409_proxy();
			default:
				return m.error_generic({ detail: describeError(err) });
		}
	}

	function unlinkErrorMessage(status: number, err: unknown): string {
		switch (status) {
			case 401:
				return m.linked_sign_in_error_401();
			case 404:
				return m.linked_sign_in_error_404_unlink();
			default:
				return m.error_generic({ detail: describeError(err) });
		}
	}

	const linkJellyfin = createSentioMutation({
		mutationFn: async () => {
			await api.POST('/auth/identities/jellyfin', {
				body: { username: jfUsername, password: jfPassword }
			});
		},
		invalidates: [keys.identities],
		onSuccess: () => {
			toast.success(m.linked_sign_in_link_success());
			jfPassword = '';
			jfUsername = '';
			jfFormOpen = false;
			errorMessage = '';
		},
		onError: (err) => {
			errorMessage = jellyfinErrorMessage(problemStatus(err), err);
			toast.error(errorMessage);
		}
	});

	const linkProxy = createSentioMutation({
		mutationFn: async () => {
			await api.POST('/auth/identities/proxy');
		},
		invalidates: [keys.identities],
		onSuccess: () => {
			toast.success(m.linked_sign_in_link_success());
			errorMessage = '';
		},
		onError: (err) => {
			errorMessage = proxyErrorMessage(problemStatus(err), err);
			toast.error(errorMessage);
		}
	});

	const unlinkIdentity = createSentioMutation({
		mutationFn: async (provider: 'jellyfin' | 'proxy') => {
			if (provider === 'jellyfin') {
				await api.DELETE('/auth/identities/jellyfin');
			} else {
				await api.DELETE('/auth/identities/proxy');
			}
		},
		invalidates: [keys.identities],
		onSuccess: () => {
			toast.success(m.linked_sign_in_unlink_success());
			errorMessage = '';
		},
		onError: (err) => {
			errorMessage = unlinkErrorMessage(problemStatus(err), err);
			toast.error(errorMessage);
		}
	});

	function submitJellyfin(): void {
		errorMessage = '';
		linkJellyfin.mutate();
	}

	function cancelJellyfin(): void {
		jfFormOpen = false;
		jfPassword = '';
		errorMessage = '';
	}
</script>

{#snippet providerHeader(providerLabel: string, identity: UserIdentity | undefined)}
	<div>
		<h3 class="font-medium text-fg">{providerLabel}</h3>
		<p class="text-sm text-subtle">
			{#if identity}
				<span>{m.linked_sign_in_status_linked()}</span>
				<span>·</span>
				<span
					>{m.linked_sign_in_linked_at({
						when: formatWhen(identity.linked_at, getLocale())
					})}</span
				>
			{:else}
				<span>{m.linked_sign_in_status_unlinked()}</span>
			{/if}
		</p>
	</div>
{/snippet}

{#snippet unlinkButton(provider: 'jellyfin' | 'proxy', providerLabel: string)}
	<ConfirmAction
		inline
		label={m.linked_sign_in_unlink()}
		message={m.linked_sign_in_unlink_confirm({
			provider: providerLabel
		})}
		onconfirm={() => unlinkIdentity.mutate(provider)}
		pending={unlinkIdentity.isPending}
		confirmLabel={m.linked_sign_in_unlink_confirm_btn({
			provider: providerLabel
		})}
		backLabel={m.action_cancel()}
	/>
{/snippet}

{#if isEnabled}
	<section aria-labelledby="linked-sign-ins-heading">
		<h2 id="linked-sign-ins-heading" class="mb-4 text-xl font-semibold">
			{m.linked_sign_ins_title()}
		</h2>

		{#if identities.isPending}
			<p aria-live="polite">{m.loading()}</p>
		{:else if identities.isError}
			<ProblemNote error={identities.error} />
		{:else}
			<div class="card flex flex-col divide-y divide-line">
				{#if sso.data?.jellyfin}
					<div class="flex flex-col gap-3 py-4 first:pt-0 last:pb-0">
						<div class="flex flex-wrap items-center justify-between gap-4">
							{@render providerHeader(m.linked_sign_in_provider_jellyfin(), jfIdentity)}
							<div>
								{#if jfIdentity}
									{@render unlinkButton('jellyfin', m.linked_sign_in_provider_jellyfin())}
								{:else if !jfFormOpen}
									<button
										type="button"
										class="btn"
										onclick={() => {
											jfFormOpen = true;
											errorMessage = '';
										}}
									>
										{m.linked_sign_in_link_jellyfin()}
									</button>
								{/if}
							</div>
						</div>

						{#if !jfIdentity && jfFormOpen}
							<form
								class="flex max-w-md flex-col gap-3 rounded-md border border-line bg-surface p-4"
								onsubmit={(e) => {
									e.preventDefault();
									submitJellyfin();
								}}
							>
								<h4 class="text-sm font-semibold">{m.linked_sign_in_jellyfin_form_title()}</h4>
								<Field id="jellyfin-username" label={m.linked_sign_in_username()}>
									<input
										id="jellyfin-username"
										class="input"
										required
										autocomplete="username"
										bind:value={jfUsername}
									/>
								</Field>
								<Field id="jellyfin-password" label={m.linked_sign_in_password()}>
									<input
										id="jellyfin-password"
										type="password"
										class="input"
										required
										autocomplete="current-password"
										bind:value={jfPassword}
									/>
								</Field>
								<div class="flex gap-2">
									<button type="submit" class="btn btn-primary" disabled={linkJellyfin.isPending}>
										{m.linked_sign_in_link_submit()}
									</button>
									<button
										type="button"
										class="btn"
										disabled={linkJellyfin.isPending}
										onclick={cancelJellyfin}
									>
										{m.action_cancel()}
									</button>
								</div>
							</form>
						{/if}
					</div>
				{/if}

				{#if sso.data?.proxy}
					<div class="flex flex-col gap-3 py-4 first:pt-0 last:pb-0">
						<div class="flex flex-wrap items-center justify-between gap-4">
							{@render providerHeader(m.linked_sign_in_provider_proxy(), proxyIdentity)}
							<div>
								{#if proxyIdentity}
									{@render unlinkButton('proxy', m.linked_sign_in_provider_proxy())}
								{:else}
									<button
										type="button"
										class="btn"
										disabled={linkProxy.isPending}
										onclick={() => {
											errorMessage = '';
											linkProxy.mutate();
										}}
									>
										{m.linked_sign_in_link_proxy()}
									</button>
								{/if}
							</div>
						</div>
						{#if !proxyIdentity}
							<p class="text-xs text-subtle">
								{m.linked_sign_in_proxy_help()}
							</p>
						{/if}
					</div>
				{/if}
			</div>

			<div aria-live="polite" class="mt-4">
				{#if errorMessage}
					<p
						role="alert"
						class="rounded-md border border-danger bg-danger/10 px-3 py-2 text-sm text-danger"
					>
						{errorMessage}
					</p>
				{/if}
			</div>
		{/if}
	</section>
{/if}
