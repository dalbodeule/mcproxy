import { json } from '@sveltejs/kit';
import { isSessionConfigured } from '#lib/server/session.js';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = () => {
	return json({ status: 'ok', configured: isSessionConfigured() });
};
