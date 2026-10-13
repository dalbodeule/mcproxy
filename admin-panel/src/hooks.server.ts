import { redirect } from '@sveltejs/kit';
import type { Handle } from '@sveltejs/kit/hooks';
import { verifySession, SESSION_COOKIE } from '#lib/server/session.js';

export const handle: Handle = async ({ event, resolve }) => {
	const secureHeaders = (response: Response) => {
		response.headers.set('x-content-type-options', 'nosniff');
		response.headers.set('x-frame-options', 'DENY');
		response.headers.set('referrer-policy', 'no-referrer');
		response.headers.set('permissions-policy', 'camera=(), geolocation=(), microphone=()');
		response.headers.set('cross-origin-opener-policy', 'same-origin');
		response.headers.set('cross-origin-resource-policy', 'same-origin');
		response.headers.set('x-permitted-cross-domain-policies', 'none');
		return response;
	};

	if (event.url.pathname === '/login' || event.url.pathname.startsWith('/_app/')) {
		const response = secureHeaders(await resolve(event));
		if (event.url.protocol === 'https:') response.headers.set('strict-transport-security', 'max-age=31536000; includeSubDomains');
		return response;
	}
	if (!verifySession(event.cookies.get(SESSION_COOKIE))) {
		if (event.url.pathname.startsWith('/api/')) {
			return secureHeaders(new Response(JSON.stringify({ error: 'admin authentication required' }), {
				status: 401,
				headers: { 'content-type': 'application/json; charset=utf-8' }
			}));
		}
		throw redirect(303, `/login?returnTo=${encodeURIComponent(event.url.pathname)}`);
	}
	const response = secureHeaders(await resolve(event));
	if (event.url.protocol === 'https:') response.headers.set('strict-transport-security', 'max-age=31536000; includeSubDomains');
	return response;
};
