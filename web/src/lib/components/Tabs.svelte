<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script module lang="ts">
	export interface Tab<K extends string = string> {
		id: K;
		label: string;
		disabled?: boolean;
		hidden?: boolean;
	}
</script>

<script lang="ts" generics="T extends string = string">
	import type { Snippet } from 'svelte';

	interface Props {
		tabs: Tab<T>[];
		active: T;
		label?: string;
		onchange?: (id: T) => void;
		children?: Snippet<[T]>;
	}

	let { tabs, active = $bindable(), label, onchange, children }: Props = $props();

	const visibleTabs = $derived(tabs.filter((t) => !t.hidden));

	function selectTab(id: T): void {
		if (id === active) return;
		active = id;
		onchange?.(id);
	}

	function handleKeydown(event: KeyboardEvent): void {
		const enabledTabs = visibleTabs.filter((t) => !t.disabled);
		if (enabledTabs.length === 0) return;

		const currentIndex = enabledTabs.findIndex((t) => t.id === active);
		let targetTab: Tab<T> | undefined;

		if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
			event.preventDefault();
			const nextIdx = currentIndex === -1 ? 0 : (currentIndex + 1) % enabledTabs.length;
			targetTab = enabledTabs[nextIdx];
		} else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
			event.preventDefault();
			const prevIdx =
				currentIndex === -1 ? 0 : (currentIndex - 1 + enabledTabs.length) % enabledTabs.length;
			targetTab = enabledTabs[prevIdx];
		} else if (event.key === 'Home') {
			event.preventDefault();
			targetTab = enabledTabs[0];
		} else if (event.key === 'End') {
			event.preventDefault();
			targetTab = enabledTabs[enabledTabs.length - 1];
		}

		if (targetTab) {
			selectTab(targetTab.id);
			const el = document.getElementById(`tab-${targetTab.id}`);
			el?.focus();
		}
	}
</script>

<div class="tabs-container">
	<div
		role="tablist"
		aria-label={label}
		class="subtab-bar mb-6 flex flex-wrap gap-2 overflow-x-visible border-b border-line md:flex-nowrap md:overflow-x-auto"
	>
		{#each visibleTabs as tab (tab.id)}
			{@const isSelected = tab.id === active}
			<button
				type="button"
				role="tab"
				id={`tab-${tab.id}`}
				aria-selected={isSelected}
				aria-controls={`panel-${tab.id}`}
				tabindex={isSelected && !tab.disabled ? 0 : -1}
				disabled={tab.disabled}
				aria-disabled={tab.disabled ? 'true' : undefined}
				onclick={() => selectTab(tab.id)}
				onkeydown={handleKeydown}
				class="inline-flex min-h-11 items-center rounded-t-lg border-b-[3px] px-3.5 py-2 text-sm font-medium whitespace-nowrap transition-colors md:min-h-0 {isSelected
					? 'border-brand bg-[var(--color-nav-active)] font-semibold text-[var(--color-nav-active-foreground)]'
					: 'border-transparent text-subtle hover:bg-[var(--color-secondary)] hover:text-fg disabled:cursor-not-allowed disabled:opacity-50'}"
			>
				{tab.label}
			</button>
		{/each}
	</div>

	<div
		role="tabpanel"
		id={`panel-${active}`}
		aria-labelledby={`tab-${active}`}
		tabindex="0"
		class="tabpanel"
	>
		{@render children?.(active)}
	</div>
</div>
