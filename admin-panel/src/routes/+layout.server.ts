import type { LayoutServerLoad } from './$types';
import { normalizeLocale } from '#lib/i18n.js';
import { readSessionClaims, SESSION_COOKIE } from '#lib/server/session.js';

export const load: LayoutServerLoad = ({ cookies, request }) => {
	const saved = cookies.get('mcproxy_locale');
	const contrast = cookies.get('mcproxy_contrast') === 'high';
	const claims = readSessionClaims(cookies.get(SESSION_COOKIE));
	if (saved) return { locale: normalizeLocale(saved), contrast, username: claims?.username ?? null, role: claims?.role ?? null };
	const preferred = request.headers.get('accept-language')?.split(',')[0]?.split('-')[0];
	return { locale: normalizeLocale(preferred), contrast, username: claims?.username ?? null, role: claims?.role ?? null };
};
