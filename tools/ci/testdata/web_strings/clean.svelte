<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
</script>

<h1>{m.dashboard_title()}</h1>
<p>{m.empty_instances()} · <span>–</span></p>
<img src="/brand/mark.svg" alt="" width="28" height="28" />
<button type="button" aria-label={m.instance_delete()}>✕</button>
<code>const x = "literal in code allowed";</code>
<p>· 50 KiB/s</p>
