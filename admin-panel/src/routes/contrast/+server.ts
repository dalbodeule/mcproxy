import { redirect } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ request, cookies, url }) => {
	const form = await request.formData();
	const value = String(form.get('contrast') ?? '') === 'high' ? 'high' : 'normal';
	cookies.set('mcproxy_contrast', value, {
		path: '/',
		httpOnly: false,
		sameSite: 'strict',
		secure: url.protocol === 'https:',
		maxAge: 60 * 60 * 24 * 365
	});
	const returnTo = String(form.get('returnTo') ?? '/');
	throw redirect(303, returnTo.startsWith('/') && !returnTo.startsWith('//') ? returnTo : '/');
};
