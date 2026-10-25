// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

type FrameKind = 'object' | 'array';
type FrameState = 'KEY_OR_END' | 'COLON' | 'VALUE' | 'COMMA_OR_END' | 'KEY' | 'VALUE_OR_END';

interface Frame {
	kind: FrameKind;
	keys: Set<string>;
	path: string;
	state: FrameState;
	pendingPath?: string;
}

const WHITESPACE_CODES = new Set([0x20, 0x09, 0x0a, 0x0d]);
const PRIMITIVE_TERMINATORS = new Set([',', '}', ']', ' ', '\t', '\n', '\r']);

function skipWhitespace(text: string, start: number): number {
	let i = start;
	const len = text.length;
	while (i < len && WHITESPACE_CODES.has(text.charCodeAt(i))) {
		i++;
	}
	return i;
}

function scanString(text: string, start: number): number {
	let i = start + 1;
	const len = text.length;
	while (i < len) {
		const ch = text[i];
		if (ch === '\\') {
			i += 2;
			continue;
		}
		if (ch === '"') {
			return i;
		}
		i++;
	}
	throw new Error(`Unterminated string starting at index ${start}`);
}

function scanPrimitive(text: string, start: number): number {
	let i = start;
	const len = text.length;
	while (i < len && !PRIMITIVE_TERMINATORS.has(text[i]!)) {
		i++;
	}
	if (i === start) {
		throw new Error(`Unexpected character '${text[start]}' at index ${start}`);
	}
	return i;
}

function completeChildValue(stack: Frame[]): void {
	if (stack.length > 0) {
		const parent = stack[stack.length - 1]!;
		parent.state = 'COMMA_OR_END';
	}
}

function handleColonToken(text: string, i: number, frame: Frame): number {
	if (text[i] !== ':') {
		throw new Error(`Expected ':' at index ${i}, got '${text[i]}'`);
	}
	frame.state = 'VALUE';
	return i + 1;
}

function handleKeyToken(text: string, i: number, frame: Frame, duplicates: string[]): number {
	const ch = text[i];
	if (frame.state === 'KEY_OR_END' && ch === '}') {
		return -1;
	}
	if (ch !== '"') {
		throw new Error(`Expected string key at index ${i}, got '${ch}'`);
	}
	const endQuote = scanString(text, i);
	const raw = text.slice(i, endQuote + 1);
	const decoded = JSON.parse(raw) as string;
	const fullPath = frame.path.length > 0 ? `${frame.path}.${decoded}` : decoded;
	if (frame.keys.has(decoded)) {
		duplicates.push(fullPath);
	} else {
		frame.keys.add(decoded);
	}
	frame.pendingPath = fullPath;
	frame.state = 'COLON';
	return endQuote + 1;
}

function handleValueToken(text: string, i: number, frame: Frame, stack: Frame[]): number {
	const ch = text[i];
	if (frame.state === 'VALUE_OR_END' && ch === ']') {
		return -1;
	}
	const nextPath = frame.kind === 'object' ? (frame.pendingPath ?? '') : frame.path;
	if (ch === '{') {
		stack.push({ kind: 'object', keys: new Set(), path: nextPath, state: 'KEY_OR_END' });
		return i + 1;
	}
	if (ch === '[') {
		stack.push({ kind: 'array', keys: new Set(), path: nextPath, state: 'VALUE_OR_END' });
		return i + 1;
	}
	const nextIdx = ch === '"' ? scanString(text, i) + 1 : scanPrimitive(text, i);
	frame.state = 'COMMA_OR_END';
	return nextIdx;
}

function handleCommaOrEnd(text: string, i: number, frame: Frame, stack: Frame[]): number {
	const ch = text[i];
	const closeChar = frame.kind === 'object' ? '}' : ']';
	if (ch === closeChar) {
		stack.pop();
		completeChildValue(stack);
		return i + 1;
	}
	if (ch === ',') {
		frame.state = frame.kind === 'object' ? 'KEY' : 'VALUE';
		return i + 1;
	}
	throw new Error(`Expected ',' or '${closeChar}' at index ${i}, got '${ch}'`);
}

function handleRootToken(text: string, i: number, stack: Frame[]): number {
	const ch = text[i];
	if (ch === '{') {
		stack.push({ kind: 'object', keys: new Set(), path: '', state: 'KEY_OR_END' });
		return i + 1;
	}
	if (ch === '[') {
		stack.push({ kind: 'array', keys: new Set(), path: '', state: 'VALUE_OR_END' });
		return i + 1;
	}
	throw new Error(`Unexpected character '${ch}' at index ${i}`);
}

function dispatchFrameToken(text: string, i: number, stack: Frame[], duplicates: string[]): number {
	const frame = stack[stack.length - 1]!;
	if (frame.state === 'KEY_OR_END' || frame.state === 'KEY') {
		const next = handleKeyToken(text, i, frame, duplicates);
		if (next === -1) {
			stack.pop();
			completeChildValue(stack);
			return i + 1;
		}
		return next;
	}
	if (frame.state === 'COLON') {
		return handleColonToken(text, i, frame);
	}
	if (frame.state === 'VALUE' || frame.state === 'VALUE_OR_END') {
		const next = handleValueToken(text, i, frame, stack);
		if (next === -1) {
			stack.pop();
			completeChildValue(stack);
			return i + 1;
		}
		return next;
	}
	return handleCommaOrEnd(text, i, frame, stack);
}

export function findDuplicateKeys(text: string): string[] {
	const trimmed = text.trim();
	if (trimmed.length === 0) {
		throw new Error('Empty JSON input');
	}
	if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) {
		JSON.parse(text);
		return [];
	}

	const duplicates: string[] = [];
	const stack: Frame[] = [];
	let i = skipWhitespace(text, 0);
	const len = text.length;
	let rootParsed = false;

	for (let step = 0; step <= len && i < len; step++) {
		i = skipWhitespace(text, i);
		if (i >= len) {
			break;
		}
		if (stack.length === 0) {
			if (rootParsed) {
				throw new Error(`Unexpected trailing content at index ${i}`);
			}
			i = handleRootToken(text, i, stack);
			rootParsed = true;
			continue;
		}
		i = dispatchFrameToken(text, i, stack, duplicates);
	}

	if (stack.length > 0) {
		throw new Error('Unbalanced brackets: unclosed structure at end of input');
	}
	return duplicates;
}
