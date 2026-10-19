<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts" module>
	import { setDefaultIconLoader } from '@sveltesentio/ui/icons';
	import { shellIconLoader } from './icons.js';

	// Once per module load, not per icon instance.
	setDefaultIconLoader(shellIconLoader);
</script>

<script lang="ts">
	import Icon from '@sveltesentio/ui/icons/component';

	interface Props {
		name: string;
		size?: number;
		class?: string;
	}

	const { name, size = 18, class: className = '' }: Props = $props();
</script>

<Icon {name} {size} class="shrink-0 {className}" />
