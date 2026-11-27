// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2

import { dump, JSON_SCHEMA, loadAll } from 'js-yaml';
import { findDuplicateKeys } from './json-duplicate-keys.js';
import {
	parseRuleInput,
	toRuleInput,
	type LibraryProcessingRule,
	type LibraryProcessingRuleInput
} from './library-processing-rule.js';

export type RuleDocumentFormat = 'json' | 'yaml';

/** Upper bound on accepted document size, in UTF-16 code units. */
export const MAX_RULE_DOCUMENT_CHARS = 262_144;

// Inferred (not annotated) so that options newer than @types/js-yaml, such as
// maxDepth and maxTotalMergeKeys, are still passed through to js-yaml 4.3.2.
const yamlLoadOptions = {
	schema: JSON_SCHEMA,
	json: false,
	maxDepth: 32,
	maxTotalMergeKeys: 0
};

const yamlDumpOptions = {
	schema: JSON_SCHEMA,
	indent: 2,
	lineWidth: -1,
	noRefs: true,
	forceQuotes: true,
	sortKeys: false
};

function decodeJson(text: string): unknown {
	const duplicates = findDuplicateKeys(text);
	if (duplicates.length > 0) {
		throw new Error('Duplicate JSON object keys are not allowed');
	}
	return JSON.parse(text);
}

function decodeYaml(text: string): unknown {
	const documents = loadAll(text, null, yamlLoadOptions);
	if (documents.length !== 1) {
		throw new Error(`Expected exactly one YAML document, found ${documents.length}`);
	}
	return documents[0];
}

/** Parse a bounded JSON or YAML document and validate it with the rule validator. */
export function readRuleDocument(
	text: string,
	format: RuleDocumentFormat
): LibraryProcessingRuleInput {
	if (text.length > MAX_RULE_DOCUMENT_CHARS) {
		throw new Error(`Rule document exceeds ${MAX_RULE_DOCUMENT_CHARS} characters`);
	}
	const value = format === 'json' ? decodeJson(text) : decodeYaml(text);
	return parseRuleInput(value);
}

/** Serialize the editable input form of a stored rule as JSON or YAML. */
export function writeRuleDocument(
	stored: LibraryProcessingRule,
	format: RuleDocumentFormat
): string {
	const input = toRuleInput(stored);
	if (format === 'json') {
		return `${JSON.stringify(input, null, 2)}\n`;
	}
	return dump(input, yamlDumpOptions);
}
