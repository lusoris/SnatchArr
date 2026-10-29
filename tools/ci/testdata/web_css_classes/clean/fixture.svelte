<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->
<div class="bg-sunken text-sm hover:bg-sunken"></div>
<div class="bg-sunken {active ? 'text-sm' : 'hover:bg-sunken'}"></div>
<div class={`bg-sunken ${active ? 'text-sm' : 'hover:bg-sunken'}`}></div>
<div class={['bg-sunken', active && 'text-sm', {'hover:bg-sunken': active}]}></div>
<div class:bg-sunken={active} class:text-sm></div>
