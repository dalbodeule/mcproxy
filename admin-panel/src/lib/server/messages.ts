import type { Cookies } from '@sveltejs/kit';
import { normalizeLocale, translate } from '#lib/i18n.js';

export function panelMessage(cookies: Cookies, key: string, values: Record<string, string | number> = {}) {
	let text = translate(normalizeLocale(cookies.get('mcproxy_locale')), key);
	for (const [name, value] of Object.entries(values)) text = text.replace(`{${name}}`, String(value));
	return text;
}
