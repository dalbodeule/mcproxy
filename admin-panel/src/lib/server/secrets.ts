import { readFileSync } from 'node:fs';
import {
	ADMIN_PANEL_PASSWORD,
	ADMIN_PANEL_PASSWORD_FILE,
	ADMIN_PANEL_SESSION_SECRET,
	ADMIN_PANEL_SESSION_SECRET_FILE,
	ADMIN_PANEL_USERNAME,
	ADMIN_PANEL_USERNAME_FILE,
	MCPROXY_ADMIN_TOKEN,
	MCPROXY_ADMIN_TOKEN_FILE,
	MCPROXY_ADMIN_ACTOR_SECRET,
	MCPROXY_ADMIN_ACTOR_SECRET_FILE
} from '$app/env/private';

function secretValue(value: string, file: string) {
	if (value) return value;
	if (!file) return '';
	try {
		return readFileSync(file, 'utf8').trim();
	} catch {
		return '';
	}
}

export const panelUsername = () => secretValue(ADMIN_PANEL_USERNAME, ADMIN_PANEL_USERNAME_FILE);
export const panelPassword = () => secretValue(ADMIN_PANEL_PASSWORD, ADMIN_PANEL_PASSWORD_FILE);
export const panelSessionSecret = () => secretValue(ADMIN_PANEL_SESSION_SECRET, ADMIN_PANEL_SESSION_SECRET_FILE);
export const mcproxyAdminToken = () => secretValue(MCPROXY_ADMIN_TOKEN, MCPROXY_ADMIN_TOKEN_FILE);
export const mcproxyAdminActorSecret = () => secretValue(MCPROXY_ADMIN_ACTOR_SECRET, MCPROXY_ADMIN_ACTOR_SECRET_FILE);
