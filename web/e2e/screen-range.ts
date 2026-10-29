// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import AxeBuilder from '@axe-core/playwright';
import { expect, type Locator, type Page } from '@playwright/test';
import { measureContentSpan, measurePhoneFit } from './layout.js';

// Fingerprint supplied by the screen-range brief, fetched 2026-10-28 22:10 CET:
// A = StatCounter Global Stats, screen-resolution-stats/{desktop,mobile,tablet}/worldwide,
// September 2026 table. B = WCAG 2.2 SC 1.4.10. C = project order, independent of share.
export const SCREEN_CLASSES = [
	{ name: 'phone-reflow', width: 320, height: 640, dpr: 2, source: 'B: 320 CSS px reflow' },
	{ name: 'phone-small', width: 360, height: 800, dpr: 3, source: 'A mobile #2: 7.54%' },
	{ name: 'phone', width: 414, height: 896, dpr: 3, source: 'A mobile #1: 13.35%' },
	{ name: 'tablet-portrait', width: 768, height: 1024, dpr: 2, source: 'A tablet #1: 8.09%' },
	{ name: 'tablet-landscape', width: 1280, height: 800, dpr: 2, source: 'A tablet #3: 7.85%' },
	{ name: 'laptop-1366', width: 1366, height: 768, dpr: 1, source: 'A desktop #3: 7.95%' },
	{ name: 'laptop-1440', width: 1440, height: 900, dpr: 2, source: 'C' },
	{
		name: 'laptop-1536',
		width: 1536,
		height: 864,
		dpr: 1,
		source: 'A desktop #2: 10% (1920 at 125% scaling)'
	},
	{ name: 'desktop-1920', width: 1920, height: 1080, dpr: 1, source: 'A desktop #1: 28.07%' },
	{ name: 'qhd-2560', width: 2560, height: 1440, dpr: 1, source: 'A desktop #5: 3.95%' },
	{ name: 'uhd-3840-dpr1', width: 3840, height: 2160, dpr: 1, source: 'C' },
	{
		name: 'uhd-3840-dpr2',
		width: 1920,
		height: 1080,
		dpr: 2,
		source: 'C: 4K panel at 200% scaling'
	},
	{ name: 'ultrawide-3440', width: 3440, height: 1440, dpr: 1, source: 'C' },
	{ name: 'superwide-5120', width: 5120, height: 1440, dpr: 1, source: 'C' }
] as const;

export async function assertNoHorizontalScroll(page: Page): Promise<void> {
	const widths = await page.evaluate(() => ({
		viewport: window.innerWidth,
		document: document.documentElement.scrollWidth,
		body: document.body.scrollWidth
	}));
	expect(widths.document, 'horizontal page scroll (document)').toBeLessThanOrEqual(widths.viewport);
	expect(widths.body, 'horizontal page scroll (body)').toBeLessThanOrEqual(widths.viewport);
}

export async function assertScalesWithViewport(page: Page) {
	const span = await page.evaluate(measureContentSpan);
	const layout = await page.locator('main').evaluate((main) => {
		const style = getComputedStyle(main);
		const box = main.getBoundingClientRect();
		const left = box.left + parseFloat(style.paddingLeft);
		const right = box.right - parseFloat(style.paddingRight);
		const centredMax = Array.from(main.children).some((child) => {
			const rect = child.getBoundingClientRect();
			const maxWidth = parseFloat(getComputedStyle(child).maxWidth);
			return (
				maxWidth >= 1200 &&
				rect.width >= 1200 &&
				Math.abs((rect.left + rect.right) / 2 - (left + right) / 2) <= 2
			);
		});
		return { viewport: window.innerWidth, centredMax };
	});
	// Brief thresholds: >=60% of viewport, or a centred cap >=1200px; <=640px always fails.
	// Rendered measurements are attached by the caller; no baseline numbers assumed here.
	if (layout.viewport >= 1366) {
		expect(span.widest, 'fixed mobile column at wide viewport').toBeGreaterThan(640);
		expect(
			span.widest >= layout.viewport * 0.6 || (span.widest >= 1200 && layout.centredMax),
			`content span ${span.widest}px at ${layout.viewport}px must scale or use a centred cap`
		).toBe(true);
	}
	return { ...span, ...layout };
}

export async function assertReflow(page: Page): Promise<void> {
	await assertNoHorizontalScroll(page);
	const fit = await page.evaluate(measurePhoneFit);
	const width = await page.evaluate(() => window.innerWidth);
	expect(fit.mainWidth, 'reflow requires visible main content').toBeGreaterThan(0);
	expect(fit.mainWidth, 'main exceeds reflow viewport').toBeLessThanOrEqual(width);
	expect(fit.offenders, 'reflow: content needs horizontal scrolling').toEqual([]);
}

export async function applyTextSpacing(page: Page): Promise<void> {
	await page.addStyleTag({
		content: `
		*, *::before, *::after {
			line-height: 1.5 !important;
			letter-spacing: 0.12em !important;
			word-spacing: 0.16em !important;
		}
		p { margin-block-end: 2em !important; }
	`
	});
}

interface TextBox {
	left: number;
	top: number;
	right: number;
	bottom: number;
}
interface TextGeometry {
	label: string;
	boxes: TextBox[];
	// Nearest fixed or sticky ancestor (-1 = page flow): overlays cover scrolled content by design.
	layer: number;
}

const TEXT_TARGETS = 'button, label, h1, h2, h3, h4, h5, h6, [role="heading"]';

// Browser callback: text ranges detect glyph clipping and collisions, rather than
// treating overlapping container boxes (e.g. a nested label and button) as text collisions.
function measureTextGeometry(elements: Element[], selector: string): TextGeometry[] {
	return elements.map((el) => {
		const boxes: TextBox[] = [];
		const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, {
			acceptNode(node) {
				const parent = node.parentElement!;
				const rect = parent.getBoundingClientRect();
				const visible = parent.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true });
				return node.textContent?.trim() &&
					visible &&
					rect.width > 1 &&
					rect.height > 1 &&
					parent.closest(selector) === el
					? NodeFilter.FILTER_ACCEPT
					: NodeFilter.FILTER_REJECT;
			}
		});
		for (let i = 0; i < 200; i++) {
			const node = walker.nextNode();
			if (!node) break;
			const range = document.createRange();
			range.selectNodeContents(node);
			const rects = Array.from(range.getClientRects()).filter((r) => r.width > 1 && r.height > 1);
			boxes.push(
				...rects.map((r) => ({ left: r.left, right: r.right, top: r.top, bottom: r.bottom }))
			);
		}
		const overlays = Array.from(document.querySelectorAll('*')).filter((n) =>
			/^(fixed|sticky)$/.test(getComputedStyle(n).position)
		);
		const overlay = overlays.findLast((n) => n.contains(el));
		return {
			label: `${el.tagName}: ${el.textContent?.trim().slice(0, 100)}`,
			boxes,
			layer: overlay ? overlays.indexOf(overlay) : -1
		};
	});
}

// Browser callback: text inside a scroll container stays reachable by scrolling, so once the
// walk passes a scroller only a clip of the scroller box itself (e.g. a collapsed section)
// limits the text. Finite sentinels, because Infinity does not survive serialisation.
function measureClippingBounds(elements: Element[]): TextBox[] {
	const FAR = 1e9;
	const clipped = (lo: number, hi: number, clipLo: number, clipHi: number) =>
		clipLo > lo + 2 || clipHi < hi - 2;
	return elements.map((el) => {
		const box = el.getBoundingClientRect();
		let left = box.left,
			right = box.right,
			top = box.top,
			bottom = box.bottom;
		let scrollerX: DOMRect | null = null;
		let scrollerY: DOMRect | null = null;
		let ancestor = el.parentElement;
		for (let depth = 0; ancestor && depth < 100; depth++, ancestor = ancestor.parentElement) {
			const style = getComputedStyle(ancestor);
			const rect = ancestor.getBoundingClientRect();
			if (!scrollerX && /^(auto|scroll)$/.test(style.overflowX)) {
				scrollerX = rect;
				left = -FAR;
				right = FAR;
			}
			if (!scrollerY && /^(auto|scroll)$/.test(style.overflowY)) {
				scrollerY = rect;
				top = -FAR;
				bottom = FAR;
			}
			const hideX = /^(hidden|clip)$/.test(style.overflowX);
			if (
				hideX &&
				(!scrollerX || clipped(scrollerX.left, scrollerX.right, rect.left, rect.right))
			) {
				left = Math.max(left, rect.left);
				right = Math.min(right, rect.right);
			}
			const hideY = /^(hidden|clip)$/.test(style.overflowY);
			if (
				hideY &&
				(!scrollerY || clipped(scrollerY.top, scrollerY.bottom, rect.top, rect.bottom))
			) {
				top = Math.max(top, rect.top);
				bottom = Math.min(bottom, rect.bottom);
			}
		}
		return { left, right, top, bottom };
	});
}

// Browser callback: the part of the page each element's scroll and clip ancestors show.
// Text scrolled out of a scroller (an older log line) sits outside it and overlaps nothing.
function measureShownArea(elements: Element[]): TextBox[] {
	return elements.map((el) => {
		const area = { left: -1e9, top: -1e9, right: 1e9, bottom: 1e9 };
		let ancestor = el.parentElement;
		for (let depth = 0; ancestor && depth < 100; depth++, ancestor = ancestor.parentElement) {
			const style = getComputedStyle(ancestor);
			const rect = ancestor.getBoundingClientRect();
			if (style.overflowX !== 'visible') {
				area.left = Math.max(area.left, rect.left);
				area.right = Math.min(area.right, rect.right);
			}
			if (style.overflowY !== 'visible') {
				area.top = Math.max(area.top, rect.top);
				area.bottom = Math.min(area.bottom, rect.bottom);
			}
		}
		return area;
	});
}

function shownBoxes(boxes: TextBox[], area: TextBox): TextBox[] {
	return boxes
		.map((b) => ({
			left: Math.max(b.left, area.left),
			right: Math.min(b.right, area.right),
			top: Math.max(b.top, area.top),
			bottom: Math.min(b.bottom, area.bottom)
		}))
		.filter((b) => b.right - b.left > 2 && b.bottom - b.top > 2);
}

function textCollisions(items: TextGeometry[], areas: TextBox[]): string[] {
	const collisions: string[] = [];
	const shown = items.map((item, i) => shownBoxes(item.boxes, areas[i]));
	for (let i = 0; i < items.length; i++) {
		for (let j = i + 1; j < items.length; j++) {
			if (items[i].layer !== items[j].layer) continue;
			const overlaps = shown[i].some((a) =>
				shown[j].some(
					(b) =>
						Math.min(a.right, b.right) - Math.max(a.left, b.left) > 2 &&
						Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 2
				)
			);
			if (overlaps) collisions.push(`${items[i].label} / ${items[j].label}`);
		}
	}
	return collisions;
}

// The three measurements run as separate calls. A live page (the log stream) can move its
// text between them, so measure again until the text stands still across one round.
async function measureStableText(
	targets: Locator
): Promise<{ items: TextGeometry[]; bounds: TextBox[]; areas: TextBox[] }> {
	let items = await targets.evaluateAll(measureTextGeometry, TEXT_TARGETS);
	for (let attempt = 0; attempt < 5; attempt++) {
		const bounds = await targets.evaluateAll(measureClippingBounds);
		const areas = await targets.evaluateAll(measureShownArea);
		const after = await targets.evaluateAll(measureTextGeometry, TEXT_TARGETS);
		if (JSON.stringify(after) === JSON.stringify(items)) return { items, bounds, areas };
		items = after;
	}
	throw new Error('text kept moving while it was measured');
}

export async function assertTextNotClipped(page: Page): Promise<void> {
	const { items, bounds, areas } = await measureStableText(page.locator(TEXT_TARGETS));
	// A failure names the bounds each text left, so the clipping ancestor can be found.
	const clipped = items
		.map((item, i) => ({ ...item, bounds: bounds[i] }))
		.filter((item) =>
			item.boxes.some(
				(r) =>
					r.left < item.bounds.left - 2 ||
					r.right > item.bounds.right + 2 ||
					r.top < item.bounds.top - 2 ||
					r.bottom > item.bounds.bottom + 2
			)
		);
	expect(items.length, 'text-spacing check requires text targets').toBeGreaterThan(0);
	expect(clipped, 'text clipped after spacing override').toEqual([]);
	expect(textCollisions(items, areas), 'text overlaps after spacing override').toEqual([]);
}

// WCAG 2.4.11 (AA): a focused control must not be entirely hidden. One that fits the
// viewport must be fully visible; a larger one (a tab panel, a scroll region) only in part.
// Fully visible allows one CSS pixel at an edge: focus scrolling lands on whole device pixels,
// so a control at a fractional offset can end a fraction of a pixel past the viewport.
async function focusVisibleRatio(focused: Locator): Promise<number> {
	return focused.evaluate((el) => {
		const r = el.getBoundingClientRect();
		if (r.width > window.innerWidth || r.height > window.innerHeight) return 0;
		// Focus scrolling keeps the html scroll padding clear of a sticky top bar and a fixed
		// bottom bar, so a control taller than the room between them can show only that room.
		const style = getComputedStyle(document.documentElement);
		const padding =
			(Number.parseFloat(style.scrollPaddingTop) || 0) +
			(Number.parseFloat(style.scrollPaddingBottom) || 0);
		const room = Math.min(1, (window.innerHeight - padding) / r.height);
		return room * Math.max(0.5, 1 - 1 / Math.min(r.width, r.height));
	});
}

// WCAG 2.4.11 again: author overlays (a sticky top bar, a fixed bottom bar) must not cover
// the whole control. Probe its centre and four inset corners, clamped to the viewport.
async function focusUnobscured(focused: Locator): Promise<boolean> {
	return focused.evaluate((el) => {
		const r = el.getBoundingClientRect();
		const left = Math.max(r.left, 0) + 2;
		const right = Math.min(r.right, window.innerWidth) - 2;
		const top = Math.max(r.top, 0) + 2;
		const bottom = Math.min(r.bottom, window.innerHeight) - 2;
		if (left > right || top > bottom) return false;
		const points = [
			[(left + right) / 2, (top + bottom) / 2],
			[left, top],
			[right, top],
			[left, bottom],
			[right, bottom]
		];
		return points.some(([x, y]) => {
			const hit = document.elementFromPoint(x, y);
			return hit !== null && (el.contains(hit) || hit.contains(el));
		});
	});
}

export async function assertFocusVisible(page: Page, limit = 12): Promise<void> {
	const count = await page
		.locator('a[href], button, input, select, textarea, [tabindex]')
		.evaluateAll(
			(elements) =>
				elements.filter(
					(el) =>
						(el as HTMLElement).tabIndex >= 0 &&
						!el.matches(':disabled') &&
						!el.closest('[inert]') &&
						el.checkVisibility({ checkVisibilityCSS: true })
				).length
		);
	expect(count, 'focus check requires tabbable controls').toBeGreaterThan(0);
	// Start sequential navigation at the document top, not where an earlier check left it.
	await page.evaluate(() => {
		document.body.tabIndex = -1;
		document.body.focus();
		document.body.removeAttribute('tabindex');
	});
	for (let i = 0; i < Math.min(limit, count); i++) {
		await page.keyboard.press('Tab');
		const focused = page.locator(':focus');
		await expect(focused, `Tab ${i + 1}: focus lost`).toHaveCount(1);
		await expect(focused, `Tab ${i + 1}: focus outside viewport`).toBeInViewport({
			ratio: await focusVisibleRatio(focused)
		});
		expect(await focusUnobscured(focused), `Tab ${i + 1}: focus hidden under an overlay`).toBe(
			true
		);
		const indicator = await focused.evaluate((el) => {
			const style = getComputedStyle(el);
			return {
				control: el !== document.body && el !== document.documentElement,
				outline: style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0,
				shadow: style.boxShadow !== 'none'
			};
		});
		expect(indicator.control, `Tab ${i + 1}: document received focus`).toBe(true);
		expect(indicator.outline || indicator.shadow, `Tab ${i + 1}: no focus indicator`).toBe(true);
	}
}

const NAVIGATION_LINE = /^\s*- navigation(?:\s|:|$)/m;

// A navigation landmark is either on the page or one named menu control away
// (a collapsed drawer between the phone bar and the desktop sidebar is a valid pattern).
async function assertNavigationReachable(page: Page, snapshot: string): Promise<void> {
	if (NAVIGATION_LINE.test(snapshot)) return;
	const toggle = page.locator('button[aria-controls][aria-expanded="false"]:visible').first();
	await expect(toggle, 'navigation landmark or a menu control that reveals one').toHaveCount(1);
	await toggle.click();
	expect(await page.locator('body').ariaSnapshot(), 'menu control reveals navigation').toMatch(
		NAVIGATION_LINE
	);
	await page.keyboard.press('Escape');
	await expect(toggle).toHaveAttribute('aria-expanded', 'false');
	await toggle.blur();
}

export async function assertLandmarksAndNames(page: Page): Promise<string> {
	const snapshot = await page.locator('body').ariaSnapshot();
	expect(snapshot.match(/^\s*- main(?:\s|:|$)/gm) ?? [], 'exactly one main landmark').toHaveLength(
		1
	);
	await assertNavigationReachable(page, snapshot);
	await expect(page.getByRole('heading', { level: 1 }), 'exactly one level-1 heading').toHaveCount(
		1
	);
	const unnamed = snapshot.split('\n').filter((line) => {
		const control = line.match(/^\s*- (button|link|textbox)(?: "((?:\\.|[^"\\])*)")?(?=\s|:|$)/);
		return control && !control[2]?.trim();
	});
	expect(unnamed, 'button/link/textbox missing accessible name').toEqual([]);
	return snapshot;
}

export async function axeScan(page: Page): Promise<void> {
	const results = await new AxeBuilder({ page })
		.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
		.analyze();
	expect(results.violations, 'WCAG 2.2 AA axe violations').toEqual([]);
}
