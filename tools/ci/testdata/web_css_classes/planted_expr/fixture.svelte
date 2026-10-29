<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<div class="bg-sunken {active ? 'bg-dead-ternary' : 'text-sm'}"></div>
<div class={`bg-sunken ${active ? 'bg-dead-template' : 'text-sm'}`}></div>
<div class={['bg-sunken', {'bg-dead-object': active, 'text-sm': !active}]}></div>
<div class:bg-dead-directive={active} class:bg-sunken={!active}></div>
