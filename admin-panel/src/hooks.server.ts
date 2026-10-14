import { redirect } from '@sveltejs/kit';
import type { Handle } from '@sveltejs/kit/hooks';
import { readSessionClaims, verifySession, SESSION_COOKIE } from '#lib/server/session.js';
import { getMember } from '#lib/server/membership.js';

export const handle: Handle = async ({ event, resolve }) => {
	const secureHeaders = (response: Response) => {
		// Authenticated pages can contain one-time invitation URLs and temporary
		// passwords. Never allow a browser, reverse proxy, or shared cache to
		// retain those response bodies.
		response.headers.set('cache-control', 'no-store');
		response.headers.set('x-content-type-options', 'nosniff');
		response.headers.set('x-frame-options', 'DENY');
		response.headers.set('content-security-policy', "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'");
		response.headers.set('referrer-policy', 'no-referrer');
		response.headers.set('permissions-policy', 'camera=(), geolocation=(), microphone=()');
		response.headers.set('cross-origin-opener-policy', 'same-origin');
		response.headers.set('cross-origin-resource-policy', 'same-origin');
		response.headers.set('x-permitted-cross-domain-policies', 'none');
		return response;
	};

	// SvelteKit's built-in CSRF check covers form actions. The panel's JSON
	// proxy is a custom endpoint, so protect its state-changing requests too.
	// The authenticated panel API is browser-facing; require an explicit
	// same-origin Origin so a cookie-bearing mutation cannot omit the signal.
	if (event.url.pathname.startsWith('/api/') && ['POST', 'PUT', 'PATCH', 'DELETE'].includes(event.request.method)) {
		const origin = event.request.headers.get('origin');
		if (!origin || origin !== event.url.origin) {
			return secureHeaders(new Response(JSON.stringify({ error: 'cross-origin request forbidden' }), {
				status: 403,
				headers: { 'content-type': 'application/json; charset=utf-8' }
			}));
		}
	}

	if (event.url.pathname === '/login' || event.url.pathname === '/invite' || event.url.pathname === '/locale' || event.url.pathname === '/contrast' || event.url.pathname === '/health' || event.url.pathname === '/ready' || event.url.pathname.startsWith('/_app/')) {
		const response = secureHeaders(await resolve(event));
		if (event.url.protocol === 'https:') response.headers.set('strict-transport-security', 'max-age=31536000; includeSubDomains');
		return response;
	}
	const sessionValue = event.cookies.get(SESSION_COOKIE);
	const claims = readSessionClaims(sessionValue);
	const member = claims?.userId ? getMember(claims.userId) : null;
	if (!verifySession(sessionValue) || !claims?.userId || !member || member.username !== claims.username) {
		if (event.url.pathname.startsWith('/api/')) {
			return secureHeaders(new Response(JSON.stringify({ error: 'admin authentication required' }), {
				status: 401,
				headers: { 'content-type': 'application/json; charset=utf-8' }
			}));
		}
		throw redirect(303, `/login?returnTo=${encodeURIComponent(event.url.pathname)}`);
	}
	if (claims.mustChange && event.url.pathname !== '/account' && event.url.pathname !== '/logout') {
		if (event.url.pathname.startsWith('/api/')) return secureHeaders(new Response(JSON.stringify({ error: 'password change required' }), { status: 403, headers: { 'content-type': 'application/json; charset=utf-8' } }));
		throw redirect(303, '/account');
	}
	const response = secureHeaders(await resolve(event));
	if (event.url.protocol === 'https:') response.headers.set('strict-transport-security', 'max-age=31536000; includeSubDomains');
	return response;
};
