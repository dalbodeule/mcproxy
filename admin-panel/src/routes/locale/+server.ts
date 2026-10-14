import { redirect } from '@sveltejs/kit';
import { normalizeLocale } from '#lib/i18n.js';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ request, cookies, url }) => {
	const form = await request.formData();
	cookies.set('mcproxy_locale', normalizeLocale(String(form.get('locale') ?? '')), {
		path: '/',
		httpOnly: false,
		sameSite: 'strict',
		secure: url.protocol === 'https:',
		maxAge: 60 * 60 * 24 * 365
	});
	const returnTo = String(form.get('returnTo') ?? '/');
	throw redirect(303, returnTo.startsWith('/') && !returnTo.startsWith('//') ? returnTo : '/');
};
