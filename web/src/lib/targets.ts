// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

/** True for an `a.target-link[href]` or an enabled `button.target-link[type=button]`. */
function isValidTargetLink(el: Element): boolean {
	if (el.tagName === 'A' && el.hasAttribute('href')) {
		return true;
	}
	if (
		el.tagName === 'BUTTON' &&
		el.getAttribute('type') === 'button' &&
		!el.hasAttribute('disabled') &&
		!(el as HTMLButtonElement).disabled
	) {
		return true;
	}
	return false;
}

/**
 * Validates that each `.target` in the given root contains exactly one
 * `a.target-link[href]` or `button.target-link[type=button]:not([disabled])`,
 * has no nested `.target` elements, and has no invalid `.target-link` elements.
 *
 * @param root DOM subtree to validate.
 * @returns Array of human-readable problem descriptions (empty if valid).
 */
export function targetProblems(root: ParentNode): string[] {
	const problems: string[] = [];
	const targets: Element[] = [];

	if (typeof Element !== 'undefined' && root instanceof Element && root.matches('.target')) {
		targets.push(root);
	}
	targets.push(...Array.from(root.querySelectorAll('.target')));

	for (const target of targets) {
		const nested = target.querySelectorAll('.target');
		if (nested.length > 0) {
			problems.push(
				`Nested .target elements are prohibited: found ${nested.length} nested target(s)`
			);
		}

		const allTargetLinks = Array.from(target.querySelectorAll('.target-link')).filter(
			(el) => el.closest('.target') === target
		);

		const validLinks = allTargetLinks.filter(isValidTargetLink);

		if (validLinks.length === 0) {
			problems.push(
				'Target element must contain exactly one a.target-link[href] or button.target-link[type=button]:not([disabled]), found 0'
			);
		} else if (validLinks.length > 1) {
			problems.push(
				`Target element must contain exactly one a.target-link[href] or button.target-link[type=button]:not([disabled]), found ${validLinks.length}`
			);
		}
	}

	return problems;
}
