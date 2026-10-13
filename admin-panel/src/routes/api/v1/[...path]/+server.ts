import type { RequestHandler } from './$types';
import { MCPROXY_ADMIN_TOKEN, MCPROXY_API_URL } from '$app/env/private';

const hopByHop = new Set(['connection', 'host', 'content-length', 'transfer-encoding', 'cookie']);

export const fallback: RequestHandler = async ({ params, request, fetch, url }) => {
	if (!MCPROXY_API_URL || !MCPROXY_ADMIN_TOKEN) {
		return new Response('Admin panel is not configured', { status: 503 });
	}
	const headers = new Headers(request.headers);
	for (const name of hopByHop) headers.delete(name);
	headers.set('Authorization', `Bearer ${MCPROXY_ADMIN_TOKEN}`);

	const response = await fetch(`${MCPROXY_API_URL}/api/v1/${params.path}${url.search}`, {
		method: request.method,
		headers,
		body: request.method === 'GET' || request.method === 'HEAD' ? undefined : await request.arrayBuffer()
	});

	const outgoing = new Headers(response.headers);
	outgoing.delete('set-cookie');
	return new Response(response.body, { status: response.status, headers: outgoing });
};

export const GET = fallback;
export const POST = fallback;
export const PUT = fallback;
export const DELETE = fallback;
export const PATCH = fallback;
