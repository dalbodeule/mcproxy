import { redirect } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { revokeSession, SESSION_COOKIE } from '#lib/server/session.js';

const logout: RequestHandler = async ({ cookies }) => {
	revokeSession(cookies.get(SESSION_COOKIE));
	cookies.delete(SESSION_COOKIE, { path: '/' });
	throw redirect(303, '/login');
};

export const POST = logout;
