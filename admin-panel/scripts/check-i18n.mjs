import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../src/', import.meta.url);
const files = [];

function walk(directory) {
	for (const name of readdirSync(directory)) {
		const path = join(directory, name);
		if (statSync(path).isDirectory()) walk(path);
		else if (/\.(svelte|ts)$/.test(path)) files.push(path);
	}
}

walk(root.pathname);
const used = new Set();
for (const file of files) {
	const source = readFileSync(file, 'utf8');
	for (const match of source.matchAll(/\bt\(\s*['"]([^'"]+)['"]\s*\)/g)) used.add(match[1]);
	for (const match of source.matchAll(/\b(?:panelMessage|message)\([^,]+,\s*['"]([^'"]+)['"]/g)) used.add(match[1]);
}

const dictionary = readFileSync(new URL('../src/lib/i18n.ts', import.meta.url), 'utf8');
function dictionarySection(name, endMarker) {
	const start = dictionary.indexOf(`const ${name}`);
	const end = dictionary.indexOf(endMarker, start);
	if (start < 0 || end < 0) throw new Error(`Could not parse ${name}`);
	return dictionary.slice(start, end);
}

function localeKeys(section, locale) {
	const start = section.indexOf(`\n\t${locale}: {`);
	if (start < 0) throw new Error(`Could not parse ${locale} dictionary`);
	const closing = section.slice(start + 1).search(/\n\t},?\n/);
	if (closing < 0) throw new Error(`Could not parse ${locale} dictionary`);
	const body = section.slice(start, start + 1 + closing);
	return new Set([...body.matchAll(/['"]([^'"]+)['"]\s*:/g)].map((match) => match[1]));
}

const messages = dictionarySection('messages', 'const actionMessages');
const actionMessages = dictionarySection('actionMessages', 'export function normalizeLocale');
const localeSources = {};
for (const locale of ['ko', 'en', 'ja']) {
	localeSources[locale] = new Set([...localeKeys(messages, locale), ...localeKeys(actionMessages, locale)]);
}
const defined = localeSources.ko;
const missing = [...used].filter((key) => !defined.has(key)).sort();
if (missing.length > 0) {
	console.error(`Missing i18n keys: ${missing.join(', ')}`);
	process.exit(1);
}
for (const locale of ['en', 'ja']) {
	const missingLocale = [...defined].filter((key) => !localeSources[locale].has(key)).sort();
	if (missingLocale.length > 0) {
		console.error(`Missing ${locale} translations: ${missingLocale.join(', ')}`);
		process.exit(1);
	}
}
console.log(`i18n key check passed (${used.size} used keys, ko/en/ja parity confirmed)`);
