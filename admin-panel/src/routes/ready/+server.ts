import { json } from '@sveltejs/kit';
import { membershipStoreReady } from '#lib/server/membership.js';
import { sessionStoreReady } from '#lib/server/session.js';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = () => {
	const ready = sessionStoreReady() && membershipStoreReady();
	return json({ status: ready ? 'ready' : 'not_ready' }, { status: ready ? 200 : 503 });
};
